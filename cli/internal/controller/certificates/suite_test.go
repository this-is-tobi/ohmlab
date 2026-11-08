package certificates

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/this-is-tobi/ohmlab/cli/internal/controller/config"
	"github.com/this-is-tobi/ohmlab/cli/internal/controller/metrics"
)

// The suite runs a real API server (envtest) with a Certificate CRD holding the
// fields the controller reads. Set KUBEBUILDER_ASSETS to the directory
// `go tool setup-envtest use -p path` prints. CI sets OHMLAB_REQUIRE_ENVTEST so
// a missing API server fails the build instead of skipping the suite.

var (
	restCfg *rest.Config
	scheme  = runtime.NewScheme()
	direct  client.Client // talks to the API server without a cache
)

func TestMain(m *testing.M) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		if os.Getenv("OHMLAB_REQUIRE_ENVTEST") != "" {
			fmt.Fprintln(os.Stderr, "KUBEBUILDER_ASSETS is not set and OHMLAB_REQUIRE_ENVTEST demands an API server")
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "KUBEBUILDER_ASSETS is not set: skipping the envtest suite")
		os.Exit(0)
	}
	_ = clientgoscheme.AddToScheme(scheme)

	env := &envtest.Environment{CRDDirectoryPaths: []string{"testdata/crds"}, ErrorIfCRDPathMissing: true}
	var err error
	if restCfg, err = env.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "start envtest:", err)
		os.Exit(1)
	}
	if direct, err = client.New(restCfg, client.Options{Scheme: scheme}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	_ = env.Stop()
	os.Exit(code)
}

var nsCounter atomic.Int64

func newNamespace(t *testing.T) string {
	t.Helper()
	ns := fmt.Sprintf("c%d-%d", time.Now().UnixNano()%100000, nsCounter.Add(1))
	if err := direct.Create(context.Background(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}); err != nil {
		t.Fatal(err)
	}
	return ns
}

// harness is one manager and one reconciler for the restarts of a namespace.
type harness struct {
	t       *testing.T
	ns      string
	reg     *prometheus.Registry
	metrics *metrics.Metrics
	stop    func()
}

// start runs a manager on ns for the given restarts; entries without a
// namespace get ns. It stops when the test ends, or on h.stop.
func start(t *testing.T, ns string, restarts ...config.Restart) *harness {
	t.Helper()
	for i := range restarts {
		if restarts[i].Namespace == "" {
			restarts[i].Namespace = ns
		}
	}
	mgr, err := ctrl.NewManager(restCfg, ctrl.Options{
		Scheme:                 scheme,
		Cache:                  cache.Options{DefaultNamespaces: map[string]cache.Config{ns: {}}},
		Metrics:                metricsserver.Options{BindAddress: "0"},
		HealthProbeBindAddress: "0",
	})
	if err != nil {
		t.Fatal(err)
	}
	reg := prometheus.NewRegistry()
	h := &harness{t: t, ns: ns, reg: reg, metrics: metrics.New(reg)}
	r := &Reconciler{
		Certificates: mgr.GetCache(),
		Workloads:    mgr.GetAPIReader(),
		Client:       mgr.GetClient(),
		Recorder:     mgr.GetEventRecorder("test"),
		Metrics:      h.metrics,
		Restarts:     index(restarts),
		Resync:       150 * time.Millisecond,
	}
	if err := r.SetupWithManager(mgr, true); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = mgr.Start(ctx); close(done) }()
	h.stop = func() { cancel(); <-done }
	t.Cleanup(cancel)
	return h
}

func restart(cert string, workloads ...config.Workload) config.Restart {
	return config.Restart{Certificate: cert, Workloads: workloads}
}

func deploy(name string) config.Workload { return config.Workload{Kind: "Deployment", Name: name} }
func sts(name string) config.Workload    { return config.Workload{Kind: "StatefulSet", Name: name} }

func podTemplate(name string, ann map[string]string) corev1.PodTemplateSpec {
	return corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": name}, Annotations: ann},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "c", Image: "busybox"}}},
	}
}

func (h *harness) createDeployment(name string, ann map[string]string) {
	h.t.Helper()
	d := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: h.ns},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": name}},
			Template: podTemplate(name, ann),
		},
	}
	if err := direct.Create(context.Background(), d); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) createStatefulSet(name string, onDelete bool) {
	h.t.Helper()
	s := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: h.ns},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": name}},
			Template: podTemplate(name, nil),
		},
	}
	if onDelete {
		s.Spec.UpdateStrategy.Type = appsv1.OnDeleteStatefulSetStrategyType
	}
	if err := direct.Create(context.Background(), s); err != nil {
		h.t.Fatal(err)
	}
}

func newCertificate(ns, name string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(certificateGVK)
	u.SetNamespace(ns)
	u.SetName(name)
	return u
}

func (h *harness) createCertificate(name string) {
	h.t.Helper()
	u := newCertificate(h.ns, name)
	u.Object["spec"] = map[string]any{"secretName": name}
	if err := direct.Create(context.Background(), u); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) deleteCertificate(name string) {
	h.t.Helper()
	if err := direct.Delete(context.Background(), newCertificate(h.ns, name)); err != nil {
		h.t.Fatal(err)
	}
}

// setRevision writes status.revision as cert-manager does after an issuance.
func (h *harness) setRevision(name string, n int64) {
	h.t.Helper()
	u := newCertificate(h.ns, name)
	if err := direct.Get(context.Background(), types.NamespacedName{Namespace: h.ns, Name: name}, u); err != nil {
		h.t.Fatal(err)
	}
	if err := unstructured.SetNestedField(u.Object, n, "status", "revision"); err != nil {
		h.t.Fatal(err)
	}
	if err := direct.Status().Update(context.Background(), u); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) workload(kind, name string) client.Object {
	h.t.Helper()
	var obj client.Object = &appsv1.Deployment{}
	if kind == "StatefulSet" {
		obj = &appsv1.StatefulSet{}
	}
	if err := direct.Get(context.Background(), types.NamespacedName{Namespace: h.ns, Name: name}, obj); err != nil {
		h.t.Fatal(err)
	}
	return obj
}

func templateOf(obj client.Object) corev1.PodTemplateSpec {
	switch o := obj.(type) {
	case *appsv1.Deployment:
		return o.Spec.Template
	case *appsv1.StatefulSet:
		return o.Spec.Template
	}
	return corev1.PodTemplateSpec{}
}

func (h *harness) annotation(kind, name, key string) string {
	return templateOf(h.workload(kind, name)).Annotations[key]
}

func (h *harness) revisionOf(kind, name string) string { return h.annotation(kind, name, Annotation) }

func (h *harness) generation(kind, name string) int64 { return h.workload(kind, name).GetGeneration() }

func (h *harness) restarts(kind, name string) float64 {
	return testutil.ToFloat64(h.metrics.WorkloadRestarts.WithLabelValues(Target, h.ns, kind, name))
}

func (h *harness) ready(cert string) float64 {
	return testutil.ToFloat64(h.metrics.ObjectReady.WithLabelValues(Target, kind, h.ns, cert))
}

// hasSeries reports whether a metric family has a series with label name=value.
func (h *harness) hasSeries(family, name, value string) bool {
	h.t.Helper()
	mfs, err := h.reg.Gather()
	if err != nil {
		h.t.Fatal(err)
	}
	for _, mf := range mfs {
		if mf.GetName() != family {
			continue
		}
		for _, m := range mf.GetMetric() {
			for _, l := range m.GetLabel() {
				if l.GetName() == name && l.GetValue() == value {
					return true
				}
			}
		}
	}
	return false
}

// event reports whether an Event with this reason regards the named object.
func (h *harness) event(reason, regarding string) bool {
	h.t.Helper()
	var list eventsv1.EventList
	if err := direct.List(context.Background(), &list, client.InNamespace(h.ns)); err != nil {
		h.t.Fatal(err)
	}
	for _, e := range list.Items {
		if e.Reason == reason && e.Regarding.Name == regarding {
			return true
		}
	}
	return false
}

// eventually polls until cond holds.
func (h *harness) eventually(what string, cond func() bool) {
	h.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	h.t.Fatalf("timed out waiting for %s", what)
}

func (h *harness) rolledTo(kind, name, want string) {
	h.t.Helper()
	h.eventually(fmt.Sprintf("%s %s to carry revision %q", kind, name, want), func() bool { return h.revisionOf(kind, name) == want })
}

// ownedFields returns the fields a manager owns on a workload, as compact JSON.
func (h *harness) ownedFields(kind, name, manager string) string {
	h.t.Helper()
	for _, f := range h.workload(kind, name).GetManagedFields() {
		if f.Manager == manager && f.FieldsV1 != nil {
			var v any
			if err := json.Unmarshal(f.FieldsV1.Raw, &v); err != nil {
				h.t.Fatal(err)
			}
			b, _ := json.Marshal(v)
			return string(b)
		}
	}
	return ""
}
