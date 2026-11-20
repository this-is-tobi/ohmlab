package storage

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	ctrlevent "sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	v1alpha1 "github.com/this-is-tobi/ohmlab/cli/api/v1alpha1"
	"github.com/this-is-tobi/ohmlab/cli/internal/controller/config"
	"github.com/this-is-tobi/ohmlab/cli/internal/rustfs"
	"github.com/this-is-tobi/ohmlab/cli/internal/seed"
	"github.com/this-is-tobi/ohmlab/cli/internal/vault"
)

// replicatorKind labels the replicators' metrics.
const replicatorKind = "S3Replicator"

// replicatorUser is the store user whose key copies the store to its replica.
// Its canned policy has the same name.
func replicatorUser(store string) string { return "ohmlab-replicator-" + store }

// replicatorReconciler keeps, for each store with a replica, one read-only
// identity over the buckets that replicate, and its key with the list of those
// buckets in the store's own record. It runs per store, beside the S3Bucket
// reconciler, and shares its clients: one policy over many buckets has no one
// S3Bucket to belong to.
type replicatorReconciler struct {
	*Reconciler
	backoff *backoff
}

func (r *Reconciler) setupReplicators(mgr ctrl.Manager, skipNameValidation bool) error {
	var names []string
	for name, sc := range r.Stores {
		if sc.Replicator != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	rep := &replicatorReconciler{Reconciler: r, backoff: newBackoff()}
	rep.backoff.base, rep.backoff.max = r.backoff.base, r.backoff.max
	// Each store is looked at once at start, so its record exists before any
	// bucket replicates: the copy job reads it.
	initial := make(chan ctrlevent.GenericEvent, len(names))
	for _, name := range names {
		initial <- ctrlevent.GenericEvent{Object: &v1alpha1.S3Bucket{Spec: v1alpha1.S3BucketSpec{Store: name}}}
	}
	return ctrl.NewControllerManagedBy(mgr).
		Named("s3replicator").
		Watches(&v1alpha1.S3Bucket{}, handler.EnqueueRequestsFromMapFunc(rep.storeOf)).
		WatchesRawSource(source.Channel(initial, handler.EnqueueRequestsFromMapFunc(rep.storeOf))).
		WithOptions(controller.Options{MaxConcurrentReconciles: 1, SkipNameValidation: &skipNameValidation}).
		Complete(rep)
}

// storeOf maps any change of an S3Bucket, its status included, to its store's
// replicator.
func (r *replicatorReconciler) storeOf(_ context.Context, o client.Object) []reconcile.Request {
	b, ok := o.(*v1alpha1.S3Bucket)
	if !ok {
		return nil
	}
	if sc, ok := r.Stores[b.Spec.Store]; !ok || sc.Replicator == "" {
		return nil
	}
	return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: b.Spec.Store}}}
}

// Reconcile brings one store's replicator to the buckets that replicate.
func (r *replicatorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	sc, ok := r.Stores[req.Name]
	if !ok || sc.Replicator == "" {
		return ctrl.Result{}, nil
	}
	changes, err := r.sync(ctx, sc)
	r.recordReplicator(sc.Name, changes, err)
	if err != nil {
		return ctrl.Result{RequeueAfter: r.backoff.next(req.NamespacedName)}, nil
	}
	r.backoff.reset(req.NamespacedName)
	return ctrl.Result{RequeueAfter: time.Duration(float64(r.Resync) * (0.9 + 0.2*rand.Float64()))}, nil
}

// sync writes the record first, then makes the store follow it, as for a
// claim. In observe-only mode it plans both and writes neither.
func (r *replicatorReconciler) sync(ctx context.Context, sc StoreConfig) ([]change, error) {
	mount, path, err := seed.SplitPath(sc.Replicator)
	if err != nil {
		return nil, fmt.Errorf("replicator record %q: %w", sc.Replicator, err)
	}
	ca, err := r.readCA()
	if err != nil {
		return nil, err
	}
	store, err := r.storeFor(ctx, sc, ca)
	if err != nil {
		return nil, err
	}
	var list v1alpha1.S3BucketList
	if err := r.List(ctx, &list); err != nil {
		return nil, fmt.Errorf("list the claims: %w", err)
	}
	user := replicatorUser(sc.Name)
	for attempt := 1; ; attempt++ {
		sec, err := r.kv.KVRead(ctx, mount, path)
		if err != nil {
			return nil, err
		}
		entry, _ := sec.Data[config.ReplicatorKey].(map[string]any)
		buckets := replicatedBuckets(list.Items, sc.Name, strings.Fields(str(entry["buckets"])))
		c := claim{owner: "store:" + sc.Name, key: config.ReplicatorKey, fields: map[string]string{
			"buckets": strings.Join(buckets, " "), "ca": ca, "endpoint": sc.Endpoint, "pathStyle": strconv.FormatBool(sc.PathStyle), "region": sc.Region,
		}}
		rp, err := planRecord(sec, c, r.now(), r.RotationGrace, newKeyPair)
		if err != nil {
			return nil, err
		}
		if rp.blocked != "" {
			return nil, errors.New(rp.blocked)
		}
		st, err := observeReplicator(ctx, store, user)
		if err != nil {
			return nil, err
		}
		actions, err := planReplicator(st, user, buckets, rp)
		if err != nil {
			return nil, err
		}
		changes := slices.Clone(rp.changes)
		for _, a := range actions {
			changes = append(changes, change{a.op, a.name})
		}
		if r.ObserveOnly {
			return changes, nil
		}
		if rp.data != nil || rp.metadata != nil {
			_, err := r.writeRecord(ctx, mount, path, sec, rp)
			if errors.Is(err, vault.ErrCASMismatch) && attempt < casAttempts {
				continue
			}
			if err != nil {
				return changes, err
			}
		}
		for _, a := range actions {
			if err := a.do(ctx, store); err != nil {
				if errors.Is(err, rustfs.ErrForbidden) {
					r.drop(sc.Name)
				}
				return changes, err
			}
		}
		return changes, nil
	}
}

// replicatedBuckets lists, sorted, the buckets of the store's S3Buckets that
// replicate: each joins once Ready, and stays while it replicates, so a
// backend outage that turns claims not-Ready never narrows the replica.
func replicatedBuckets(items []v1alpha1.S3Bucket, store string, current []string) []string {
	set := map[string]bool{}
	for _, o := range items {
		if o.Spec.Store != store || !o.Spec.Replicate || !o.DeletionTimestamp.IsZero() {
			continue
		}
		if meta.IsStatusConditionTrue(o.Status.Conditions, v1alpha1.ConditionReady) || slices.Contains(current, o.Spec.Bucket) {
			set[o.Spec.Bucket] = true
		}
	}
	return slices.Sorted(maps.Keys(set))
}

func observeReplicator(ctx context.Context, s Store, user string) (storeState, error) {
	var st storeState
	var err error
	if st.policy, err = s.Policy(ctx, user); err != nil {
		return st, err
	}
	if st.user, err = s.User(ctx, user); err != nil {
		return st, err
	}
	if st.user.Exists {
		if st.keys, err = s.ServiceAccounts(ctx, user); err != nil {
			return st, err
		}
	}
	return st, nil
}

// planReplicator lists the writes that make the replicator match its record:
// its policy over the buckets, put only when there is one, attached only then;
// the user; the record's key, and no other.
func planReplicator(st storeState, user string, buckets []string, rp recordPlan) ([]storeAction, error) {
	var out []storeAction
	add := func(a storeAction) { out = append(out, a) }
	policy := user
	if len(buckets) > 0 {
		doc, err := rustfs.BucketsPolicy(buckets, "readOnly")
		if err != nil {
			return nil, err
		}
		if st.policy == nil || !rustfs.SamePolicy(st.policy, doc) {
			add(storeAction{op: "put-policy", name: policy, do: func(ctx context.Context, s Store) error { return s.PutPolicy(ctx, policy, doc) }})
		}
	}
	if !st.user.Exists {
		add(storeAction{op: "create-user", name: user, do: func(ctx context.Context, s Store) error {
			secret, err := randomString(secretKeyLength, secretKeyAlphabet)
			if err != nil {
				return err
			}
			return s.CreateUser(ctx, user, secret)
		}})
	}
	if len(buckets) > 0 && !slices.Contains(st.user.Policies, policy) {
		add(storeAction{op: "attach-policy", name: policy, do: func(ctx context.Context, s Store) error { return s.AttachPolicy(ctx, policy, user) }})
	}
	for _, p := range st.user.Policies {
		if p != policy || len(buckets) == 0 {
			add(storeAction{op: "detach-policy", name: p, do: func(ctx context.Context, s Store) error { return s.DetachPolicy(ctx, p, user) }})
		}
	}
	if !slices.Contains(st.keys, rp.accessKey) {
		add(storeAction{op: "add-service-account", name: user, do: func(ctx context.Context, s Store) error {
			return s.AddServiceAccount(ctx, user, rp.accessKey, rp.secretKey)
		}})
	}
	for _, k := range st.keys {
		if k != rp.accessKey {
			add(storeAction{op: "remove-service-account", name: user, key: k, stray: true,
				do: func(ctx context.Context, s Store) error { return s.RemoveServiceAccount(ctx, k) }})
		}
	}
	return out, nil
}

// recordReplicator updates the metrics and logs what a pass did, by name: a
// replicator has no object to hold a status or events.
func (r *replicatorReconciler) recordReplicator(store string, changes []change, err error) {
	logger := log.Log.WithValues("target", Target, "kind", replicatorKind, "store", store)
	ready := 1.0
	if err != nil {
		ready = 0
		logger.Info("replicator not reconciled", "error", scrub(err))
	} else {
		r.Metrics.LastSuccess.WithLabelValues(Target, replicatorKind).SetToCurrentTime()
	}
	r.Metrics.ObjectReady.WithLabelValues(Target, replicatorKind, "", store).Set(ready)
	if r.ObserveOnly {
		counts := map[string]int{}
		for _, c := range changes {
			counts[c.action]++
		}
		r.Metrics.SetPlanned(Target, replicatorKind, store, counts)
		if len(changes) > 0 {
			logger.Info("observe-only: replicator changes planned, nothing written", "changes", describe(changes))
		}
		return
	}
	if err == nil && len(changes) > 0 {
		logger.Info("replicator reconciled", "changes", describe(changes))
	}
}
