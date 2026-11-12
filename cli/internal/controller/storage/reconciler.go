// Package storage reconciles S3Bucket objects: each one is a bucket in a
// store, an identity confined to it, and the claim's key in a Vault record.
// Vault is the source of truth: the record is written first, and the store
// is made to match it, so a store rebuilt empty gets every key back.
package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/recorder"

	v1alpha1 "github.com/this-is-tobi/ohmlab/cli/api/v1alpha1"
	"github.com/this-is-tobi/ohmlab/cli/internal/controller/config"
	"github.com/this-is-tobi/ohmlab/cli/internal/controller/metrics"
	"github.com/this-is-tobi/ohmlab/cli/internal/rustfs"
	"github.com/this-is-tobi/ohmlab/cli/internal/seed"
	"github.com/this-is-tobi/ohmlab/cli/internal/vault"
)

const (
	// Target is the name of the target this package implements.
	Target = "storage"
	// PathOwner marks the records this target writes.
	PathOwner = "ohmlab-controller-storage"
	kind      = "S3Bucket"
	crdName   = "s3buckets.ohmlab.fr"
	// casAttempts bounds the record writes of one reconcile that lose a race.
	casAttempts = 3
)

// StoreConfig is one store of the target's configuration.
type StoreConfig struct {
	Name, Endpoint, Region, BucketPrefix string
	PathStyle                            bool
	Capacity                             int64
	// Root is where the store's root keys live in Vault.
	Root   config.SecretRef
	CAFile string
	// Dial builds a client from the root keys.
	Dial func(accessKey, secretKey string) (Store, error)
}

// Reconciler reconciles S3Buckets.
type Reconciler struct {
	client.Client
	KV     KV
	Stores map[string]StoreConfig
	// CAFile is the stores' CA, written into every record.
	CAFile   string
	Recorder recorder.EventRecorder
	Metrics  *metrics.Metrics

	// ObserveOnly plans every store and Vault change and writes neither.
	ObserveOnly bool
	// AllowDelete lets deletionPolicy: Delete remove a claim's identity.
	AllowDelete bool
	// RotationGrace is how long a rotated key keeps working.
	RotationGrace time.Duration
	// Resync is how often an object is reconciled again, stretched or shrunk
	// by up to 10% so objects do not line up.
	Resync time.Duration
	// BackoffBase and BackoffMax bound the retries; 5s and 5m by default.
	BackoffBase, BackoffMax time.Duration
	// Now is the clock; time.Now by default.
	Now func() time.Time

	backoff *backoff
	kv      KV
	mu      sync.Mutex
	clients map[string]*storeClient
}

// storeClient is a store's client and the CA it was built with.
type storeClient struct {
	Store
	ca string
}

// SetupWithManager registers the controller. The queue is fed by changes to
// an object's spec and by the resync the reconciler asks for.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager, skipNameValidation bool) error {
	if r.Resync <= 0 {
		r.Resync = 10 * time.Minute
	}
	r.backoff = newBackoff()
	if r.BackoffBase > 0 {
		r.backoff.base = r.BackoffBase
	}
	if r.BackoffMax > 0 {
		r.backoff.max = r.BackoffMax
	}
	r.kv = &instrumentedKV{inner: r.KV, m: r.Metrics}
	r.clients = map[string]*storeClient{}
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &v1alpha1.S3Bucket{}, bucketIndex, func(o client.Object) []string {
		return []string{storeBucket(o.(*v1alpha1.S3Bucket))}
	}); err != nil {
		return err
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.S3Bucket{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Named("s3bucket").
		WithOptions(controller.Options{MaxConcurrentReconciles: 1, SkipNameValidation: &skipNameValidation}).
		Complete(r)
}

func owner(o *v1alpha1.S3Bucket) string { return o.Namespace + "/" + o.Name }

// bucketIndex finds the S3Buckets naming one bucket of one store.
const bucketIndex = "ohmlab.fr/store-bucket"

func storeBucket(o *v1alpha1.S3Bucket) string { return o.Spec.Store + "/" + o.Spec.Bucket }

// holder is the oldest S3Bucket naming obj's store and bucket through another
// record key, when it is older than obj. The naming rule leaves the record's
// env and scope out, and its parts may hold dashes, so two records can imply
// one bucket; claims on one record key are told apart by its claim marker.
func (r *Reconciler) holder(ctx context.Context, obj *v1alpha1.S3Bucket) (*v1alpha1.S3Bucket, error) {
	var list v1alpha1.S3BucketList
	if err := r.List(ctx, &list, client.MatchingFields{bucketIndex: storeBucket(obj)}); err != nil {
		return nil, fmt.Errorf("list the claims on bucket %s: %w", obj.Spec.Bucket, err)
	}
	var h *v1alpha1.S3Bucket
	for i := range list.Items {
		o := &list.Items[i]
		if o.Spec.Credentials != obj.Spec.Credentials && older(o, obj) && (h == nil || older(o, h)) {
			h = o
		}
	}
	return h, nil
}

// older orders claims by creation, then by namespace and name.
func older(a, b *v1alpha1.S3Bucket) bool {
	if !a.CreationTimestamp.Equal(&b.CreationTimestamp) {
		return a.CreationTimestamp.Before(&b.CreationTimestamp)
	}
	return owner(a) < owner(b)
}

func (r *Reconciler) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// outcome is what one reconcile decided, before it reaches the status.
type outcome struct {
	ready     metav1.ConditionStatus
	reason    string
	message   string
	stalled   bool // nothing changes until the spec does: no backoff
	reconcile bool // working towards Ready: retry with backoff
	drifted   bool
	success   bool  // completed without a backend error
	version   int64 // the record's version; 0 leaves the status as it is
	quota     int64
	changes   []change
	events    []event
	adopted   bool
	rotated   bool
}

type event struct{ reason, note string }

// Reconcile brings one S3Bucket's bucket, identity and record to what it
// declares.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var obj v1alpha1.S3Bucket
	if err := r.Get(ctx, req.NamespacedName, &obj); err != nil {
		if apierrors.IsNotFound(err) {
			r.backoff.reset(req.NamespacedName)
			r.Metrics.Forget(Target, kind, req.Namespace, req.Name)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	if !obj.DeletionTimestamp.IsZero() {
		return r.finalize(ctx, &obj)
	}

	previous := ""
	if c := meta.FindStatusCondition(obj.Status.Conditions, v1alpha1.ConditionReady); c != nil {
		previous = c.Reason
	}
	out := r.decide(ctx, &obj)
	if err := r.writeStatus(ctx, &obj, out); err != nil {
		return ctrl.Result{}, err
	}
	r.record(&obj, out, previous)

	if out.reconcile {
		return ctrl.Result{RequeueAfter: r.backoff.next(req.NamespacedName)}, nil
	}
	// A stalled or refused object is looked at again with the resync: the
	// other object of a conflict may go away without this spec changing.
	r.backoff.reset(req.NamespacedName)
	return ctrl.Result{RequeueAfter: time.Duration(float64(r.Resync) * (0.9 + 0.2*rand.Float64()))}, nil
}

// decide checks the spec, plans the record and the store, and in live mode
// writes the record, then makes the store follow it.
func (r *Reconciler) decide(ctx context.Context, obj *v1alpha1.S3Bucket) outcome {
	sc, mount, path, err := r.check(obj)
	if err != nil {
		return stalled(v1alpha1.ReasonInvalidSpec, err.Error())
	}
	h, err := r.holder(ctx, obj)
	if err != nil {
		return backendDown(err)
	}
	if h != nil {
		return outcome{ready: metav1.ConditionFalse, reason: v1alpha1.ReasonOwnedByAnother, success: true,
			message: fmt.Sprintf("bucket %s of store %s is claimed by %s, through another record", obj.Spec.Bucket, sc.Name, owner(h))}
	}
	if o := r.syncFinalizer(ctx, obj); o != nil {
		return *o
	}
	ca, err := r.readCA()
	if err != nil {
		return backendDown(err)
	}
	store, err := r.storeFor(ctx, sc, ca)
	if err != nil {
		return r.failed(sc.Name, err)
	}
	w, err := wantOf(obj)
	if err != nil {
		return stalled(v1alpha1.ReasonInvalidSpec, err.Error())
	}
	c := claim{owner: owner(obj), key: obj.Spec.Credentials.Key, fields: map[string]string{
		"bucket": obj.Spec.Bucket, "ca": ca, "endpoint": sc.Endpoint, "pathStyle": strconv.FormatBool(sc.PathStyle), "region": sc.Region,
	}}
	if obj.Spec.Rotate != nil {
		c.token = obj.Spec.Rotate.Token
	}

	for attempt := 1; ; attempt++ {
		sec, err := r.kv.KVRead(ctx, mount, path)
		if err != nil {
			return r.failed(sc.Name, err)
		}
		rp, err := planRecord(sec, c, r.now(), r.RotationGrace, newKeyPair)
		if err != nil {
			return backendDown(err)
		}
		if rp.blocked != "" {
			return outcome{ready: metav1.ConditionFalse, reason: v1alpha1.ReasonOwnedByAnother, message: rp.blocked, success: true}
		}
		st, err := observeStore(ctx, store, w)
		if err != nil {
			return r.failed(sc.Name, err)
		}
		actions := planStore(st, w, rp)
		changes := slices.Clone(rp.changes)
		for _, a := range actions {
			changes = append(changes, change{a.op, a.name})
		}

		if r.ObserveOnly {
			if len(changes) == 0 {
				return outcome{ready: metav1.ConditionTrue, reason: v1alpha1.ReasonReconciled, message: readyMessage, version: sec.Version, quota: int64(w.quota), success: true}
			}
			return outcome{ready: metav1.ConditionFalse, reason: v1alpha1.ReasonObserveOnly, drifted: true, version: sec.Version, success: true, changes: changes,
				message: fmt.Sprintf("observe-only: %s planned, nothing written", describe(changes))}
		}

		out := outcome{version: sec.Version, changes: changes}
		if rp.data != nil || rp.metadata != nil {
			v, err := r.writeRecord(ctx, mount, path, sec, rp)
			if errors.Is(err, vault.ErrCASMismatch) && attempt < casAttempts {
				continue
			}
			if err != nil {
				return r.failed(sc.Name, err)
			}
			out.version = v
			out.recordWritten(rp, v)
		}
		for _, a := range actions {
			if err := a.do(ctx, store); err != nil {
				o := r.failed(sc.Name, err)
				o.version, o.events, o.adopted, o.rotated = out.version, out.events, out.adopted, out.rotated
				return o
			}
			out.storeDone(a, sc.Name)
		}
		out.ready, out.reason, out.message, out.quota, out.success = metav1.ConditionTrue, v1alpha1.ReasonReconciled, readyMessage, int64(w.quota), true
		return out
	}
}

const readyMessage = "the bucket, its identity and the record hold what is declared"

// recordWritten notes what a record write did, for the events.
func (o *outcome) recordWritten(p recordPlan, version int64) {
	var fields []change
	for _, c := range p.changes {
		switch c.action {
		case "add-key", "update-field":
			fields = append(fields, c)
		case "adopt":
			o.adopted = true
			o.events = append(o.events, event{"Adopted", "took ownership of the record's key " + c.name})
		case "rotate":
			o.rotated = true
			o.events = append(o.events, event{"Rotated", fmt.Sprintf("made a new key for %s; the old one works until its grace ends", c.name)})
		case "retire-key":
			o.events = append(o.events, event{"Retired", "the grace of the key replaced by the last rotation ended: it goes"})
		}
	}
	if p.data != nil {
		o.events = append(o.events, event{"Written", fmt.Sprintf("wrote record version %d: %s", version, describe(fields))})
	}
}

// storeDone notes what a store action did, for the events.
func (o *outcome) storeDone(a storeAction, store string) {
	switch {
	case a.op == "create-bucket":
		o.events = append(o.events, event{"Created", fmt.Sprintf("created bucket %s in store %s", a.name, store)})
	case a.op == "detach-policy":
		o.events = append(o.events, event{"Detached", "detached policy " + a.name + ": the claim names another"})
	case a.op == "remove-service-account" && a.stray:
		// Four characters tell keys apart in an investigation; the whole key
		// stays out of events.
		o.events = append(o.events, event{"Removed", fmt.Sprintf("removed a service account of %s that the record does not name: %s…", a.name, a.key[:min(4, len(a.key))])})
	}
}

// check refuses what no reconcile can fix: an unknown store, a bucket other
// than the record implies, a quota of nothing.
func (r *Reconciler) check(obj *v1alpha1.S3Bucket) (StoreConfig, string, string, error) {
	sc, ok := r.Stores[obj.Spec.Store]
	if !ok {
		return StoreConfig{}, "", "", fmt.Errorf("store %q is not configured", obj.Spec.Store)
	}
	mount, path, err := seed.SplitPath(obj.Spec.Credentials.Path)
	parts := strings.Split(path, "/")
	if err != nil || len(parts) != 5 || parts[0] != "platforms" || parts[4] != "s3" {
		return StoreConfig{}, "", "", fmt.Errorf("credentials.path %q: want <mount>/platforms/<env>/<scope>/<app>/s3", obj.Spec.Credentials.Path)
	}
	if want := sc.BucketPrefix + mount + "-" + parts[3] + "-" + obj.Spec.Credentials.Key; obj.Spec.Bucket != want {
		return StoreConfig{}, "", "", fmt.Errorf("bucket must be %q: the store's bucketPrefix, then <mount>-<app>-<claim> of the record", want)
	}
	if obj.Spec.Quota.Sign() <= 0 {
		return StoreConfig{}, "", "", fmt.Errorf("quota must be above zero")
	}
	return sc, mount, path, nil
}

// readCA reads the stores' CA at each reconcile, so a rotation reaches the
// records and the clients.
func (r *Reconciler) readCA() (string, error) {
	if r.CAFile == "" {
		return "", nil
	}
	b, err := os.ReadFile(r.CAFile)
	if err != nil {
		return "", fmt.Errorf("read the stores' CA: %w", err)
	}
	return string(b), nil
}

// storeFor returns a store's client, built from the root keys in Vault at
// the first reconcile that needs it, and again once the store refused it or
// the CA changed.
func (r *Reconciler) storeFor(ctx context.Context, sc StoreConfig, ca string) (Store, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c := r.clients[sc.Name]; c != nil && c.ca == ca {
		return c.Store, nil
	}
	mount, path, err := seed.SplitPath(sc.Root.Path)
	if err != nil {
		return nil, fmt.Errorf("store %s: root keys: %w", sc.Name, err)
	}
	sec, err := r.kv.KVRead(ctx, mount, path)
	if err != nil {
		return nil, fmt.Errorf("store %s: read the root keys: %w", sc.Name, err)
	}
	root, _ := sec.Data[sc.Root.Key].(map[string]any)
	ak, sk := str(root["accessKey"]), str(root["secretKey"])
	if ak == "" || sk == "" {
		return nil, fmt.Errorf("store %s: %s, key %s, holds no accessKey and secretKey", sc.Name, sc.Root.Path, sc.Root.Key)
	}
	s, err := sc.Dial(ak, sk)
	if err != nil {
		return nil, fmt.Errorf("store %s: %w", sc.Name, err)
	}
	c := &storeClient{Store: &instrumentedStore{inner: s, m: r.Metrics}, ca: ca}
	r.clients[sc.Name] = c
	return c.Store, nil
}

func (r *Reconciler) drop(store string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.clients, store)
}

// writeRecord writes the markers before the data on a path that exists: a
// rotation interrupted between the two is then seen as pending and resumes,
// where the other order would rotate twice and drop the key the app uses. A
// new path keeps no custom_metadata before its first version: data first.
func (r *Reconciler) writeRecord(ctx context.Context, mount, path string, sec *vault.KVSecret, p recordPlan) (int64, error) {
	// custom_metadata is written whole: the plan carries what was there.
	markersFirst := p.metadata != nil && sec.Version > 0
	if markersFirst {
		if err := r.kv.KVSetCustomMetadata(ctx, mount, path, p.metadata); err != nil {
			return sec.Version, err
		}
	}
	version := sec.Version
	if p.data != nil {
		v, err := r.kv.KVWriteCAS(ctx, mount, path, p.data, sec.Version)
		if err != nil {
			return 0, err
		}
		version = v
	}
	if p.metadata != nil && !markersFirst {
		if err := r.kv.KVSetCustomMetadata(ctx, mount, path, p.metadata); err != nil {
			return version, err
		}
	}
	return version, nil
}

// failed reports a backend error. A store that refused its root keys gets a
// new client next time, so keys rotated in Vault are picked up.
func (r *Reconciler) failed(store string, err error) outcome {
	switch {
	case errors.Is(err, rustfs.ErrForbidden):
		r.drop(store)
		return outcome{ready: metav1.ConditionFalse, reason: v1alpha1.ReasonForbidden, reconcile: true, message: "the store refused the operation: " + scrub(err)}
	case errors.Is(err, vault.ErrForbidden):
		return outcome{ready: metav1.ConditionFalse, reason: v1alpha1.ReasonForbidden, reconcile: true, message: "Vault refused the operation: " + scrub(err)}
	}
	return backendDown(err)
}

func stalled(reason, msg string) outcome {
	return outcome{ready: metav1.ConditionFalse, reason: reason, message: msg, stalled: true}
}

func backendDown(err error) outcome {
	return outcome{ready: metav1.ConditionFalse, reason: v1alpha1.ReasonBackendUnavailable, reconcile: true, message: scrub(err)}
}

// syncFinalizer keeps the finalizer on exactly the objects that need it:
// deletionPolicy Delete, with deletion allowed, and writes enabled.
func (r *Reconciler) syncFinalizer(ctx context.Context, obj *v1alpha1.S3Bucket) *outcome {
	wantsDelete := obj.Spec.DeletionPolicy == v1alpha1.DeletionPolicyDelete
	if wantsDelete && !r.AllowDelete && !r.ObserveOnly {
		o := stalled(v1alpha1.ReasonForbidden, "deletionPolicy: Delete needs targets.storage.allowDelete in the chart; use Retain or enable it")
		return &o
	}
	want := wantsDelete && r.AllowDelete && !r.ObserveOnly
	has := controllerutil.ContainsFinalizer(obj, v1alpha1.S3BucketFinalizer)
	if want == has {
		return nil
	}
	before := obj.DeepCopy()
	if want {
		controllerutil.AddFinalizer(obj, v1alpha1.S3BucketFinalizer)
	} else {
		controllerutil.RemoveFinalizer(obj, v1alpha1.S3BucketFinalizer)
	}
	if err := r.Patch(ctx, obj, client.MergeFrom(before)); err != nil {
		o := backendDown(fmt.Errorf("update the finalizer: %w", err))
		return &o
	}
	return nil
}

// finalize removes what a Delete claim owns, in phase 0's order: its service
// accounts, its user and its policy, the bucket when it holds no object
// version, then the claim's key and markers in the record.
func (r *Reconciler) finalize(ctx context.Context, obj *v1alpha1.S3Bucket) (ctrl.Result, error) {
	key := client.ObjectKeyFromObject(obj)
	if !controllerutil.ContainsFinalizer(obj, v1alpha1.S3BucketFinalizer) {
		return ctrl.Result{}, nil
	}
	release := func() (ctrl.Result, error) {
		before := obj.DeepCopy()
		controllerutil.RemoveFinalizer(obj, v1alpha1.S3BucketFinalizer)
		if err := r.Patch(ctx, obj, client.MergeFrom(before)); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		r.backoff.reset(key)
		r.Metrics.Forget(Target, kind, obj.Namespace, obj.Name)
		return ctrl.Result{}, nil
	}
	if obj.Spec.DeletionPolicy != v1alpha1.DeletionPolicyDelete || !r.AllowDelete || r.ObserveOnly {
		return release()
	}
	sc, mount, path, err := r.check(obj)
	if err != nil {
		r.Recorder.Eventf(obj, nil, "Normal", "Retained", "Delete", "nothing removed: %s", err.Error())
		return release()
	}
	h, err := r.holder(ctx, obj)
	if err != nil {
		return r.retryDelete(ctx, obj, sc.Name, err)
	}
	if h != nil {
		r.Recorder.Eventf(obj, nil, "Normal", "Retained", "Delete", "bucket %s is claimed by %s: nothing removed", obj.Spec.Bucket, owner(h))
		return release()
	}
	sec, err := r.kv.KVRead(ctx, mount, path)
	if err != nil {
		return r.retryDelete(ctx, obj, sc.Name, err)
	}
	if sec.Metadata[ownerKey] != PathOwner || sec.Metadata[claimPrefix+obj.Spec.Credentials.Key] != owner(obj) {
		r.Recorder.Eventf(obj, nil, "Normal", "Retained", "Delete", "the record's key %s is not this object's: nothing removed", obj.Spec.Credentials.Key)
		return release()
	}
	ca, err := r.readCA()
	if err != nil {
		return r.retryDelete(ctx, obj, sc.Name, err)
	}
	store, err := r.storeFor(ctx, sc, ca)
	if err != nil {
		return r.retryDelete(ctx, obj, sc.Name, err)
	}
	w, err := wantOf(obj)
	if err != nil {
		return release()
	}
	keys, err := store.ServiceAccounts(ctx, w.user)
	if err != nil {
		return r.retryDelete(ctx, obj, sc.Name, err)
	}
	for _, k := range keys {
		if err := store.RemoveServiceAccount(ctx, k); err != nil {
			return r.retryDelete(ctx, obj, sc.Name, err)
		}
	}
	if err := store.RemoveUser(ctx, w.user); err != nil {
		return r.retryDelete(ctx, obj, sc.Name, err)
	}
	if err := store.RemovePolicy(ctx, w.policy); err != nil {
		return r.retryDelete(ctx, obj, sc.Name, err)
	}
	empty, err := store.BucketEmpty(ctx, w.bucket)
	if err != nil {
		return r.retryDelete(ctx, obj, sc.Name, err)
	}
	bucketNote := "kept bucket " + w.bucket + ": it holds data"
	if empty {
		if err := store.RemoveBucket(ctx, w.bucket); err != nil {
			return r.retryDelete(ctx, obj, sc.Name, err)
		}
		bucketNote = "removed bucket " + w.bucket
	}
	data, md := withoutClaim(sec, obj.Spec.Credentials.Key)
	if _, had := sec.Data[obj.Spec.Credentials.Key]; had {
		if _, err := r.kv.KVWriteCAS(ctx, mount, path, data, sec.Version); err != nil {
			return r.retryDelete(ctx, obj, sc.Name, err)
		}
	}
	if err := r.kv.KVSetCustomMetadata(ctx, mount, path, md); err != nil {
		return r.retryDelete(ctx, obj, sc.Name, err)
	}
	r.Recorder.Eventf(obj, nil, "Normal", "Deleted", "Delete", "removed the identity and the record's key %s; %s", obj.Spec.Credentials.Key, bucketNote)
	return release()
}

func (r *Reconciler) retryDelete(ctx context.Context, obj *v1alpha1.S3Bucket, store string, err error) (ctrl.Result, error) {
	o := r.failed(store, err)
	o.message = "could not remove the claim: " + o.message
	if werr := r.writeStatus(ctx, obj, o); werr != nil {
		return ctrl.Result{}, werr
	}
	r.Recorder.Eventf(obj, nil, "Warning", o.reason, "Delete", "%s", o.message)
	return ctrl.Result{RequeueAfter: r.backoff.next(client.ObjectKeyFromObject(obj))}, nil
}

// writeStatus updates the status only when something in it changed, so a
// resync that finds nothing to do writes nothing.
func (r *Reconciler) writeStatus(ctx context.Context, obj *v1alpha1.S3Bucket, o outcome) error {
	before := obj.DeepCopy()
	st := &obj.Status
	st.ObservedGeneration = obj.Generation
	set := func(t string, status metav1.ConditionStatus, reason, msg string) {
		meta.SetStatusCondition(&st.Conditions, metav1.Condition{Type: t, Status: status, Reason: reason, Message: msg, ObservedGeneration: obj.Generation})
	}
	set(v1alpha1.ConditionReady, o.ready, o.reason, o.message)
	toggle := func(t string, on bool, reason, msg string) {
		if on {
			set(t, metav1.ConditionTrue, reason, msg)
		} else {
			meta.RemoveStatusCondition(&st.Conditions, t)
		}
	}
	toggle(v1alpha1.ConditionStalled, o.stalled, o.reason, o.message)
	toggle(v1alpha1.ConditionReconciling, o.reconcile, o.reason, o.message)
	toggle(v1alpha1.ConditionDrifted, o.drifted, v1alpha1.ReasonDrifted, o.message)
	if o.version != 0 {
		st.KVVersion = o.version
	}
	if o.ready == metav1.ConditionTrue {
		st.QuotaBytes = o.quota
	}
	ja, _ := json.Marshal(before.Status)
	jb, _ := json.Marshal(*st)
	if bytes.Equal(ja, jb) {
		return nil
	}
	return r.Status().Patch(ctx, obj, client.MergeFrom(before))
}

// record updates metrics and emits events for what happened.
func (r *Reconciler) record(obj *v1alpha1.S3Bucket, o outcome, previous string) {
	ready := 0.0
	if o.ready == metav1.ConditionTrue || o.drifted {
		ready = 1
	}
	r.Metrics.ObjectReady.WithLabelValues(Target, kind, obj.Namespace, obj.Name).Set(ready)
	if o.success {
		r.Metrics.LastSuccess.WithLabelValues(Target, kind).SetToCurrentTime()
	}
	if r.ObserveOnly {
		counts := map[string]int{}
		for _, c := range o.changes {
			counts[c.action]++
		}
		r.Metrics.SetPlanned(Target, kind, obj.Namespace+"/"+obj.Name, counts)
	}
	if o.ready == metav1.ConditionTrue {
		r.Metrics.SetBucketQuota(Target, kind, obj.Namespace, obj.Name, obj.Spec.Store, obj.Spec.Bucket, float64(o.quota))
	}
	if o.adopted {
		r.Metrics.Adopted.WithLabelValues(Target, kind).Inc()
	}
	if o.rotated {
		r.Metrics.Rotations.WithLabelValues(Target).Inc()
	}
	log.Log.WithValues("target", Target, "kind", kind, "namespace", obj.Namespace, "name", obj.Name).
		V(1).Info("reconciled", "ready", string(o.ready), "reason", o.reason)
	for _, e := range o.events {
		r.Recorder.Eventf(obj, nil, "Normal", e.reason, "Reconcile", "%s", e.note)
	}
	// A failure is an event once, when the object enters it.
	if o.ready == metav1.ConditionFalse && !o.drifted && o.reason != previous {
		r.Recorder.Eventf(obj, nil, "Warning", o.reason, "Reconcile", "%s", o.message)
	}
}

// namedItems is how many names describe lists per action before it cuts.
const namedItems = 5

// describe sums changes by action and names what they touch: fields, claims,
// buckets, policies or users. Never a value.
func describe(changes []change) string {
	counts, names := map[string]int{}, map[string][]string{}
	for _, c := range changes {
		counts[c.action]++
		if c.name != "" && !slices.Contains(names[c.action], c.name) {
			names[c.action] = append(names[c.action], c.name)
		}
	}
	actions := make([]string, 0, len(counts))
	for a := range counts {
		actions = append(actions, a)
	}
	sort.Strings(actions)
	parts := make([]string, 0, len(actions))
	for _, a := range actions {
		part := fmt.Sprintf("%d %s", counts[a], a)
		if n := names[a]; len(n) > 0 {
			sort.Strings(n)
			if len(n) > namedItems {
				n = append(n[:namedItems:namedItems], fmt.Sprintf("and %d more", len(n)-namedItems))
			}
			part += " (" + strings.Join(n, ", ") + ")"
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, ", ")
}

// scrub bounds an error before it reaches a condition or an event. The
// clients never put a request body or a header in an error.
func scrub(err error) string {
	s := err.Error()
	if len(s) > 300 {
		s = s[:300]
	}
	return s
}
