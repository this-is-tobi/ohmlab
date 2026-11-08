// Package certificates restarts the workloads that read their certificate only
// when they start: when a listed cert-manager Certificate is issued again, each
// of its workloads is rolled by changing one pod-template annotation.
package certificates

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strconv"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/recorder"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/this-is-tobi/ohmlab/cli/internal/controller/config"
	"github.com/this-is-tobi/ohmlab/cli/internal/controller/metrics"
)

const (
	// Target is the name of the target this package implements.
	Target = "certificates"
	// Annotation is the pod-template annotation that holds the revision of the
	// certificate a workload's pods were started with.
	Annotation = "ohmlab.fr/certificate-revision"
	// FieldOwner is the server-side apply manager of the annotation, and of
	// nothing else.
	FieldOwner = "ohmlab-controller-certificates"
	kind       = "Certificate"
)

var certificateGVK = schema.GroupVersionKind{Group: "cert-manager.io", Version: "v1", Kind: "Certificate"}

// Reconciler rolls the workloads of the listed Certificates.
type Reconciler struct {
	// Certificates reads from the manager's cache: the client would read
	// unstructured objects live.
	Certificates client.Reader
	// Workloads reads by name, past the cache, so the controller needs no list
	// or watch on them and its RBAC can name each one.
	Workloads client.Reader
	Client    client.Client
	Recorder  recorder.EventRecorder
	Metrics   *metrics.Metrics
	// Restarts maps each listed Certificate to its workloads.
	Restarts map[types.NamespacedName][]config.Workload
	// Resync is how often a Certificate is looked at again; each interval is
	// stretched or shrunk by up to 10%. It also puts back an annotation that
	// someone removed.
	Resync time.Duration
}

func index(restarts []config.Restart) map[types.NamespacedName][]config.Workload {
	out := make(map[types.NamespacedName][]config.Workload, len(restarts))
	for _, r := range restarts {
		out[types.NamespacedName{Namespace: r.Namespace, Name: r.Certificate}] = r.Workloads
	}
	return out
}

// SetupWithManager registers the controller. Any change to a listed
// Certificate wakes it up: the revision lives in the status. Every listed
// Certificate is also queued once at start, so one that does not exist (a typo,
// or not created yet) is reported instead of never looked at.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager, skipNameValidation bool) error {
	if r.Resync <= 0 {
		r.Resync = 10 * time.Minute
	}
	cert := &unstructured.Unstructured{}
	cert.SetGroupVersionKind(certificateGVK)
	listed := predicate.NewPredicateFuncs(func(o client.Object) bool {
		_, ok := r.Restarts[client.ObjectKeyFromObject(o)]
		return ok
	})
	return ctrl.NewControllerManagedBy(mgr).
		For(cert, builder.WithPredicates(listed)).
		WatchesRawSource(source.Func(func(_ context.Context, q workqueue.TypedRateLimitingInterface[reconcile.Request]) error {
			for key := range r.Restarts {
				q.Add(reconcile.Request{NamespacedName: key})
			}
			return nil
		})).
		Named("certificate-restarts").
		WithOptions(controller.Options{MaxConcurrentReconciles: 1, SkipNameValidation: &skipNameValidation}).
		Complete(r)
}

// Reconcile brings every workload of one Certificate to its current revision.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	workloads, ok := r.Restarts[req.NamespacedName]
	if !ok {
		return ctrl.Result{}, nil
	}
	ready := r.Metrics.ObjectReady.WithLabelValues(Target, kind, req.Namespace, req.Name)
	cert := &unstructured.Unstructured{}
	cert.SetGroupVersionKind(certificateGVK)
	if err := r.Certificates.Get(ctx, req.NamespacedName, cert); err != nil {
		ready.Set(0)
		if apierrors.IsNotFound(err) {
			return ctrl.Result{RequeueAfter: r.jittered()}, nil
		}
		return ctrl.Result{}, err
	}
	revision, found, err := unstructured.NestedInt64(cert.Object, "status", "revision")
	if err != nil || !found {
		// Not issued yet: the status update that sets the revision wakes this up.
		ready.Set(0)
		return ctrl.Result{RequeueAfter: r.jittered()}, nil
	}
	want := strconv.FormatInt(revision, 10)

	all := true
	var errs []error
	for _, w := range workloads {
		carries, err := r.roll(ctx, cert, w, want)
		if err != nil {
			errs = append(errs, err)
		}
		all = all && carries
	}
	if len(errs) > 0 {
		ready.Set(0)
		return ctrl.Result{}, errors.Join(errs...)
	}
	if all {
		ready.Set(1)
	} else {
		ready.Set(0)
	}
	r.Metrics.LastSuccess.WithLabelValues(Target, kind).SetToCurrentTime()
	return ctrl.Result{RequeueAfter: r.jittered()}, nil
}

// roll brings one workload's annotation to want and reports whether the
// workload carries it. A missing workload is reported, not retried: it is a
// configuration problem the next resync looks at again.
func (r *Reconciler) roll(ctx context.Context, cert *unstructured.Unstructured, w config.Workload, want string) (bool, error) {
	key := types.NamespacedName{Namespace: cert.GetNamespace(), Name: w.Name}
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: w.Kind})
	err := r.Workloads.Get(ctx, key, obj)
	r.count("get", err)
	if apierrors.IsNotFound(err) {
		r.Recorder.Eventf(cert, nil, "Warning", "WorkloadNotFound", "Restart", "%s %s does not exist", w.Kind, w.Name)
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("get %s %s: %w", w.Kind, key, err)
	}
	current, _, _ := unstructured.NestedString(obj.Object, "spec", "template", "metadata", "annotations", Annotation)
	if current == want {
		return true, nil
	}

	patch := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1",
		"kind":       w.Kind,
		"metadata":   map[string]any{"name": w.Name, "namespace": key.Namespace},
		"spec": map[string]any{"template": map[string]any{"metadata": map[string]any{
			"annotations": map[string]any{Annotation: want},
		}}},
	}}
	// Forced: the key is the controller's by name. A hand edit takes the field
	// over, and an unforced apply would then conflict until the edit is undone,
	// leaving the workload on its old certificate.
	err = r.Client.Apply(ctx, client.ApplyConfigurationFromUnstructured(patch), client.FieldOwner(FieldOwner), client.ForceOwnership)
	r.count("apply", err)
	if err != nil {
		return false, fmt.Errorf("apply %s %s: %w", w.Kind, key, err)
	}
	r.Metrics.WorkloadRestarts.WithLabelValues(Target, key.Namespace, w.Kind, w.Name).Inc()
	r.Recorder.Eventf(cert, nil, "Normal", "Restarted", "Restart", "rolled %s %s for revision %s", w.Kind, w.Name, want)
	log.FromContext(ctx).Info("rolled a workload onto its renewed certificate",
		"kind", w.Kind, "namespace", key.Namespace, "name", w.Name, "from", current, "to", want)
	if strategy, _, _ := unstructured.NestedString(obj.Object, "spec", "updateStrategy", "type"); strategy == "OnDelete" {
		r.Recorder.Eventf(cert, nil, "Warning", "OnDelete", "Restart",
			"%s %s replaces its pods only when they are deleted: delete them to load revision %s", w.Kind, w.Name, want)
	}
	return true, nil
}

func (r *Reconciler) count(op string, err error) {
	result := "ok"
	switch {
	case err == nil:
	case apierrors.IsNotFound(err):
		result = "notfound"
	case apierrors.IsForbidden(err):
		result = "forbidden"
	case apierrors.IsConflict(err):
		result = "conflict"
	default:
		result = "error"
	}
	r.Metrics.BackendRequests.WithLabelValues(Target, op, result).Inc()
}

func (r *Reconciler) jittered() time.Duration {
	return time.Duration(float64(r.Resync) * (0.9 + 0.2*rand.Float64()))
}
