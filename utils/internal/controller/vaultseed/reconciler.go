// Package vaultseed reconciles VaultSeed objects: each one is a KV v2 path that
// must hold what the object declares.
package vaultseed

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"sort"
	"strings"
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

	v1alpha1 "github.com/this-is-tobi/ohmlab/utils/api/v1alpha1"
	"github.com/this-is-tobi/ohmlab/utils/internal/controller/metrics"
	"github.com/this-is-tobi/ohmlab/utils/internal/seed"
	"github.com/this-is-tobi/ohmlab/utils/internal/vault"
)

const (
	// Target is the name of the target this package implements.
	Target = "vault"
	kind   = "VaultSeed"
)

// Reconciler reconciles VaultSeeds.
type Reconciler struct {
	client.Client
	KV       KV
	Recorder recorder.EventRecorder
	Metrics  *metrics.Metrics

	// ObserveOnly plans everything and writes nothing to Vault.
	ObserveOnly bool
	// AllowDelete lets deletionPolicy: Delete remove paths.
	AllowDelete bool
	// Resync is how often an object is reconciled again; each interval is
	// stretched or shrunk by up to 10% so objects do not line up.
	Resync time.Duration
	// BackoffBase and BackoffMax bound the retry delay of an object that is
	// waiting or failing. They default to 5s and 5m.
	BackoffBase, BackoffMax time.Duration

	backoff *backoff
	kv      KV
}

// SetupWithManager registers the controller. The queue is fed by changes to an
// object's spec (its generation), and by the resync the reconciler asks for;
// the status the controller writes does not wake it up again.
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
	r.kv = &instrumented{inner: r.KV, m: r.Metrics, target: Target}
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.VaultSeed{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Named("vaultseed").
		WithOptions(controller.Options{MaxConcurrentReconciles: 1, SkipNameValidation: &skipNameValidation}).
		Complete(r)
}

func owner(s *v1alpha1.VaultSeed) string { return s.Namespace + "/" + s.Name }

// outcome is what one reconcile decided, before it is written to the status.
type outcome struct {
	ready      metav1.ConditionStatus
	reason     string
	message    string
	stalled    bool // nothing changes until the spec does: no requeue
	reconcile  bool // working towards Ready: retry with backoff
	drifted    bool
	success    bool // completed without a backend error
	version    int64
	managed    []string
	keepStatus bool // leave kvVersion and managedKeys as they are
	changes    []seed.Change
	written    seed.Result
}

// Reconcile brings the path of one VaultSeed to what it declares.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var obj v1alpha1.VaultSeed
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
	// A stalled object is looked at again with the resync, not with a backoff:
	// some stalls end without a change to this spec (the other object of a
	// cycle was fixed, a hand-filled value arrived), and a stall that stays is
	// re-confirmed from the cache and the path without writing anything.
	r.backoff.reset(req.NamespacedName)
	return ctrl.Result{RequeueAfter: r.jittered()}, nil
}

func (r *Reconciler) jittered() time.Duration {
	return time.Duration(float64(r.Resync) * (0.9 + 0.2*rand.Float64()))
}

// decide runs the checks and the engine and returns what to report.
func (r *Reconciler) decide(ctx context.Context, obj *v1alpha1.VaultSeed) outcome {
	data, err := decodeData(obj.Spec.Data.Raw)
	if err != nil {
		return stalled(v1alpha1.ReasonInvalidSpec, "spec.data: "+err.Error())
	}
	var rotate *seed.Rotate
	if obj.Spec.Rotate != nil {
		rotate = &seed.Rotate{Token: obj.Spec.Rotate.Token, Keys: obj.Spec.Rotate.Keys}
	}
	if errs := seed.Validate(obj.Spec.Path, data, rotate); len(errs) > 0 {
		return stalled(v1alpha1.ReasonInvalidSpec, joinErrors(errs))
	}

	if o := r.syncFinalizer(ctx, obj); o != nil {
		return *o
	}

	if cycle, err := r.refCycle(ctx, obj, data); err != nil {
		return backendDown(err)
	} else if cycle != nil {
		return stalled(v1alpha1.ReasonRefCycle, "references form a cycle: "+seed.DescribeCycle(cycle))
	}

	res, err := seed.Run(ctx, r.kv, obj.Spec.Path, seed.Spec{
		Owner:   owner(obj),
		Data:    data,
		Rotate:  rotate,
		Prune:   obj.Spec.Prune,
		Managed: obj.Status.ManagedKeys,
	}, seed.Options{ObserveOnly: r.ObserveOnly})
	if err != nil {
		o := failed(err)
		if res.DataWritten {
			// The data landed and the marker after it did not: say what was
			// written, or the retry that redoes the marker leaves no trace.
			o.written, o.changes = res, res.Plan.Changes
			o.version, o.managed, o.keepStatus = res.Version, res.Plan.Managed, false
		}
		return o
	}

	plan := res.Plan
	switch plan.Blocked {
	case seed.ReasonOwnedByAnother:
		return outcome{ready: metav1.ConditionFalse, reason: v1alpha1.ReasonOwnedByAnother, message: plan.Message, keepStatus: true, success: true}
	case seed.ReasonRefPending:
		return outcome{ready: metav1.ConditionFalse, reason: v1alpha1.ReasonRefPending, message: plan.Message, reconcile: true, keepStatus: true}
	case seed.ReasonInvalidSpec:
		return stalled(v1alpha1.ReasonInvalidSpec, plan.Message)
	case seed.ReasonInvalidRotation:
		return stalled(v1alpha1.ReasonInvalidRotation, plan.Message)
	}

	if r.ObserveOnly {
		if plan.Empty() && len(plan.Changes) == 0 {
			return outcome{ready: metav1.ConditionTrue, reason: v1alpha1.ReasonReconciled, message: "the path holds what is declared", version: res.Version, keepStatus: true, success: true}
		}
		return outcome{ready: metav1.ConditionFalse, reason: v1alpha1.ReasonObserveOnly, drifted: true, version: res.Version, keepStatus: true, success: true, changes: plan.Changes,
			message: fmt.Sprintf("observe-only: %s planned, nothing written", describe(plan.Changes))}
	}
	return outcome{ready: metav1.ConditionTrue, reason: v1alpha1.ReasonReconciled, message: "the path holds what is declared",
		version: res.Version, managed: plan.Managed, success: true, written: res, changes: plan.Changes}
}

// failed reports an error of the engine: a bad path is the spec's, a refusal
// is the policy's, anything else is the backend's.
func failed(err error) outcome {
	switch {
	case errors.Is(err, seed.ErrInvalidPath):
		return stalled(v1alpha1.ReasonInvalidSpec, err.Error())
	case errors.Is(err, vault.ErrForbidden):
		return outcome{ready: metav1.ConditionFalse, reason: v1alpha1.ReasonForbidden, reconcile: true, keepStatus: true,
			message: "Vault refused the operation: " + scrub(err)}
	}
	return backendDown(err)
}

func stalled(reason, msg string) outcome {
	return outcome{ready: metav1.ConditionFalse, reason: reason, message: msg, stalled: true, keepStatus: true}
}

func backendDown(err error) outcome {
	return outcome{ready: metav1.ConditionFalse, reason: v1alpha1.ReasonBackendUnavailable, reconcile: true, keepStatus: true, message: "Vault: " + scrub(err)}
}

// refCycle lists the seeds the controller sees and looks for a cycle through
// this one.
func (r *Reconciler) refCycle(ctx context.Context, obj *v1alpha1.VaultSeed, data map[string]any) ([]string, error) {
	var list v1alpha1.VaultSeedList
	if err := r.List(ctx, &list); err != nil {
		return nil, err
	}
	graph := map[string][]string{obj.Spec.Path: seed.Sources(data)}
	for i := range list.Items {
		other := &list.Items[i]
		if other.Namespace == obj.Namespace && other.Name == obj.Name || other.Spec.Path == obj.Spec.Path {
			continue
		}
		if d, err := decodeData(other.Spec.Data.Raw); err == nil {
			graph[other.Spec.Path] = seed.Sources(d)
		}
	}
	return seed.FindCycle(graph, obj.Spec.Path), nil
}

// syncFinalizer keeps the finalizer on exactly the objects that need it:
// deletionPolicy Delete, with deletion allowed, and writes enabled.
func (r *Reconciler) syncFinalizer(ctx context.Context, obj *v1alpha1.VaultSeed) *outcome {
	wantsDelete := obj.Spec.DeletionPolicy == v1alpha1.DeletionPolicyDelete
	if wantsDelete && !r.AllowDelete && !r.ObserveOnly {
		o := stalled(v1alpha1.ReasonForbidden, "deletionPolicy: Delete needs targets.vault.allowDelete in the chart; use Retain or enable it")
		return &o
	}
	want := wantsDelete && r.AllowDelete && !r.ObserveOnly
	has := controllerutil.ContainsFinalizer(obj, v1alpha1.Finalizer)
	if want == has {
		return nil
	}
	before := obj.DeepCopy()
	if want {
		controllerutil.AddFinalizer(obj, v1alpha1.Finalizer)
	} else {
		controllerutil.RemoveFinalizer(obj, v1alpha1.Finalizer)
	}
	if err := r.Patch(ctx, obj, client.MergeFrom(before)); err != nil {
		o := backendDown(fmt.Errorf("update finalizer: %w", err))
		return &o
	}
	return nil
}

// finalize removes the path when the object is deleted with deletionPolicy
// Delete, and only if the path carries this object's owner marker.
func (r *Reconciler) finalize(ctx context.Context, obj *v1alpha1.VaultSeed) (ctrl.Result, error) {
	key := client.ObjectKeyFromObject(obj)
	if !controllerutil.ContainsFinalizer(obj, v1alpha1.Finalizer) {
		return ctrl.Result{}, nil
	}
	release := func() (ctrl.Result, error) {
		before := obj.DeepCopy()
		controllerutil.RemoveFinalizer(obj, v1alpha1.Finalizer)
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

	mount, path, err := seed.SplitPath(obj.Spec.Path)
	if err != nil {
		return release()
	}
	sec, err := r.kv.KVRead(ctx, mount, path)
	if err != nil {
		return r.retryDelete(ctx, obj, err)
	}
	if sec.Version == 0 || sec.Metadata[seed.OwnerKey] != owner(obj) {
		// Never written, or not ours: nothing of ours to remove.
		r.Recorder.Eventf(obj, nil, "Normal", "Retained", "Delete", "the path does not carry this object's owner marker and was left in place")
		return release()
	}
	if err := r.kv.KVDestroy(ctx, mount, path); err != nil {
		return r.retryDelete(ctx, obj, err)
	}
	r.Recorder.Eventf(obj, nil, "Normal", "Deleted", "Delete", "removed the path from Vault")
	return release()
}

func (r *Reconciler) retryDelete(ctx context.Context, obj *v1alpha1.VaultSeed, err error) (ctrl.Result, error) {
	reason := v1alpha1.ReasonBackendUnavailable
	if errors.Is(err, vault.ErrForbidden) {
		reason = v1alpha1.ReasonForbidden
	}
	o := outcome{ready: metav1.ConditionFalse, reason: reason, message: "could not remove the path: " + scrub(err), reconcile: true, keepStatus: true}
	if werr := r.writeStatus(ctx, obj, o); werr != nil {
		return ctrl.Result{}, werr
	}
	r.Recorder.Eventf(obj, nil, "Warning", reason, "Delete", "could not remove the path: %s", scrub(err))
	return ctrl.Result{RequeueAfter: r.backoff.next(client.ObjectKeyFromObject(obj))}, nil
}

// writeStatus updates the status only when something in it changed, so a
// resync that finds nothing to do writes nothing.
func (r *Reconciler) writeStatus(ctx context.Context, obj *v1alpha1.VaultSeed, o outcome) error {
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

	if !o.keepStatus {
		st.KVVersion = o.version
		st.ManagedKeys = o.managed
	} else if o.version != 0 {
		st.KVVersion = o.version
	}
	if apiEqual(before.Status, *st) {
		return nil
	}
	return r.Status().Patch(ctx, obj, client.MergeFrom(before))
}

func apiEqual(a, b v1alpha1.VaultSeedStatus) bool {
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	return bytes.Equal(ja, jb)
}

// record updates metrics and emits events for what happened.
func (r *Reconciler) record(obj *v1alpha1.VaultSeed, o outcome, previous string) {
	// Healthy: Ready, or only held back by observe-only mode (the drift is
	// reported by ohmlab_planned_changes, not by an alert on every object).
	counts := actionCounts(o.changes)
	ready := 0.0
	if o.ready == metav1.ConditionTrue || o.drifted {
		ready = 1
	}
	r.Metrics.ObjectReady.WithLabelValues(Target, kind, obj.Namespace, obj.Name).Set(ready)
	if o.success {
		r.Metrics.LastSuccess.WithLabelValues(Target, kind).SetToCurrentTime()
	}
	if r.ObserveOnly {
		r.Metrics.SetPlanned(Target, kind, obj.Namespace+"/"+obj.Name, counts)
	}

	log.Log.WithValues("target", Target, "kind", kind, "namespace", obj.Namespace, "name", obj.Name).
		V(1).Info("reconciled", "ready", string(o.ready), "reason", o.reason)

	w := o.written
	if w.DataWritten || w.MetadataWritten {
		if n := counts["adopt"]; n > 0 {
			r.Metrics.Adopted.WithLabelValues(Target, kind).Add(float64(n))
			r.Recorder.Eventf(obj, nil, "Normal", "Adopted", "Reconcile", "took ownership of the path")
		}
		if n := counts["rotate"]; n > 0 {
			r.Metrics.Rotations.WithLabelValues(Target).Add(float64(n))
			r.Recorder.Eventf(obj, nil, "Normal", "Rotated", "Reconcile", "regenerated %d key(s)", n)
		}
		if w.DataWritten {
			r.Recorder.Eventf(obj, nil, "Normal", "Written", "Reconcile", "wrote KV version %d: %s", w.Version, describe(o.changes))
		}
	}
	// A failure is an event once, when the object enters it: the Ready reason
	// before this reconcile says whether it was already there.
	if o.ready == metav1.ConditionFalse && !o.drifted && o.reason != previous {
		r.Recorder.Eventf(obj, nil, "Warning", o.reason, "Reconcile", "%s", o.message)
	}
}

// --- helpers ----------------------------------------------------------------

func decodeData(raw []byte) (map[string]any, error) {
	if len(raw) == 0 {
		return nil, errors.New("is empty")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var data map[string]any
	if err := dec.Decode(&data); err != nil {
		return nil, errors.New("is not an object")
	}
	return data, nil
}

func joinErrors(errs []error) string {
	msgs := make([]string, 0, len(errs))
	for i, e := range errs {
		if i == 3 {
			msgs = append(msgs, fmt.Sprintf("and %d more", len(errs)-3))
			break
		}
		msgs = append(msgs, e.Error())
	}
	return strings.Join(msgs, "; ")
}

func actionCounts(changes []seed.Change) map[string]int {
	counts := map[string]int{}
	for _, c := range changes {
		counts[string(c.Action)]++
	}
	return counts
}

// namedKeys is how many keys describe names per action before it cuts.
const namedKeys = 5

// describe sums changes by action and names the keys behind them, so the field
// a change touches is found without reading the path. A change carries key
// names only, never a value.
func describe(changes []seed.Change) string {
	keys := map[seed.Action][]string{}
	counts := map[seed.Action]int{}
	for _, c := range changes {
		counts[c.Action]++
		if c.Key != "" {
			keys[c.Action] = append(keys[c.Action], c.Key)
		}
	}
	actions := make([]string, 0, len(counts))
	for a := range counts {
		actions = append(actions, string(a))
	}
	sort.Strings(actions)
	parts := make([]string, 0, len(actions))
	for _, a := range actions {
		part := fmt.Sprintf("%d %s", counts[seed.Action(a)], a)
		if k := keys[seed.Action(a)]; len(k) > 0 {
			sort.Strings(k)
			if len(k) > namedKeys {
				k = append(k[:namedKeys:namedKeys], fmt.Sprintf("and %d more", len(k)-namedKeys))
			}
			part += " (" + strings.Join(k, ", ") + ")"
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, ", ")
}

// scrub bounds an error from the backend before it reaches a condition or an
// event. The client never puts a request body or a header in an error, and this
// keeps the text short on top of that.
func scrub(err error) string {
	s := err.Error()
	if len(s) > 300 {
		s = s[:300]
	}
	return s
}
