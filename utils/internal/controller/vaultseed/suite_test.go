package vaultseed

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-logr/logr/funcr"
	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	v1alpha1 "github.com/this-is-tobi/ohmlab/utils/api/v1alpha1"
	"github.com/this-is-tobi/ohmlab/utils/internal/controller/metrics"
	"github.com/this-is-tobi/ohmlab/utils/internal/seed/seedtest"
)

// The suite runs a real API server (envtest) against the generated CRD and the
// reconciler against an in-memory KV. Set KUBEBUILDER_ASSETS to the directory
// `go tool setup-envtest use -p path` prints. CI sets OHMLAB_REQUIRE_ENVTEST so
// a missing API server fails the build instead of skipping the suite.

var (
	restCfg *rest.Config
	scheme  = runtime.NewScheme()
	direct  client.Client // talks to the API server without a cache
	logBuf  = &syncBuffer{}
)

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func TestMain(m *testing.M) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		if os.Getenv("OHMLAB_REQUIRE_ENVTEST") != "" {
			fmt.Fprintln(os.Stderr, "KUBEBUILDER_ASSETS is not set and OHMLAB_REQUIRE_ENVTEST demands an API server")
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "KUBEBUILDER_ASSETS is not set: skipping the envtest suite")
		os.Exit(0)
	}
	// Everything the controller logs, at the most verbose level, lands in logBuf:
	// the canary test reads it.
	ctrl.SetLogger(funcr.New(func(prefix, args string) { fmt.Fprintln(logBuf, prefix, args) }, funcr.Options{Verbosity: 10}))

	_ = clientgoscheme.AddToScheme(scheme)
	_ = apiextensionsv1.AddToScheme(scheme)
	_ = v1alpha1.AddToScheme(scheme)

	env := &envtest.Environment{CRDDirectoryPaths: []string{"../../../../argo-cd/apps/ohmlab-controller/files/crds"}, ErrorIfCRDPathMissing: true}
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

// harness is one manager and one reconciler working in a namespace of their own.
type harness struct {
	t       *testing.T
	ns      string
	kv      *seedtest.MemKV
	rec     *Reconciler
	reg     *prometheus.Registry
	metrics *metrics.Metrics
	mgr     ctrl.Manager
	cancel  context.CancelFunc
}

type option func(*Reconciler)

func observeOnly(r *Reconciler) { r.ObserveOnly = true }
func allowDelete(r *Reconciler) { r.AllowDelete = true }

func start(t *testing.T, opts ...option) *harness {
	t.Helper()
	h := prepare(t, opts...)
	h.run()
	return h
}

// prepare sets a harness up without starting its manager, for a test whose
// objects must all exist before the controller first looks.
func prepare(t *testing.T, opts ...option) *harness {
	t.Helper()
	ns := fmt.Sprintf("t%d-%d", time.Now().UnixNano()%100000, nsCounter.Add(1))
	if err := direct.Create(context.Background(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}); err != nil {
		t.Fatal(err)
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
	h := &harness{t: t, ns: ns, kv: seedtest.New(), reg: reg, metrics: metrics.New(reg), mgr: mgr}
	h.rec = &Reconciler{
		Client:      mgr.GetClient(),
		KV:          h.kv,
		Recorder:    mgr.GetEventRecorder("test"),
		Metrics:     h.metrics,
		Resync:      150 * time.Millisecond,
		BackoffBase: 40 * time.Millisecond,
		BackoffMax:  200 * time.Millisecond,
	}
	for _, o := range opts {
		o(h.rec)
	}
	if err := h.rec.SetupWithManager(mgr, true); err != nil {
		t.Fatal(err)
	}
	return h
}

// run starts the manager; it stops when the test ends.
func (h *harness) run() {
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	go func() { _ = h.mgr.Start(ctx) }()
	h.t.Cleanup(cancel)
}

func (h *harness) path(name string) string { return "homelab/platforms/production/core/" + name }

func (h *harness) create(name, path string, data string, mutate ...func(*v1alpha1.VaultSeed)) *v1alpha1.VaultSeed {
	h.t.Helper()
	obj := &v1alpha1.VaultSeed{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: h.ns},
		Spec:       v1alpha1.VaultSeedSpec{Path: path, Data: apiextensionsv1.JSON{Raw: []byte(data)}},
	}
	for _, m := range mutate {
		m(obj)
	}
	if err := direct.Create(context.Background(), obj); err != nil {
		h.t.Fatal(err)
	}
	return obj
}

func (h *harness) get(name string) *v1alpha1.VaultSeed {
	h.t.Helper()
	var obj v1alpha1.VaultSeed
	if err := direct.Get(context.Background(), types.NamespacedName{Namespace: h.ns, Name: name}, &obj); err != nil {
		h.t.Fatal(err)
	}
	return &obj
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

func cond(obj *v1alpha1.VaultSeed, t string) *metav1.Condition {
	for i := range obj.Status.Conditions {
		if obj.Status.Conditions[i].Type == t {
			return &obj.Status.Conditions[i]
		}
	}
	return nil
}

func (h *harness) ready(name string) {
	h.t.Helper()
	h.eventually(name+" to be Ready", func() bool {
		c := cond(h.get(name), v1alpha1.ConditionReady)
		return c != nil && c.Status == metav1.ConditionTrue
	})
}

func (h *harness) reason(name, condition, reason string) {
	h.t.Helper()
	h.eventually(fmt.Sprintf("%s %s=%s", name, condition, reason), func() bool {
		c := cond(h.get(name), condition)
		return c != nil && c.Reason == reason
	})
}
