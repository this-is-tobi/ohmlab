package storage

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-logr/logr/funcr"
	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/resource"
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

	v1alpha1 "github.com/this-is-tobi/ohmlab/cli/api/v1alpha1"
	"github.com/this-is-tobi/ohmlab/cli/internal/controller/config"
	"github.com/this-is-tobi/ohmlab/cli/internal/controller/metrics"
	"github.com/this-is-tobi/ohmlab/cli/internal/seed/seedtest"
)

// The suite runs a real API server (envtest) against the generated CRD, and
// the reconciler against an in-memory KV and an in-memory store. Set
// KUBEBUILDER_ASSETS to the directory `go tool setup-envtest use -p path`
// prints. CI sets OHMLAB_REQUIRE_ENVTEST so a missing API server fails the
// build instead of skipping the suite.

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
	// Everything the controller logs, at the most verbose level, lands in
	// logBuf: the canary test reads it.
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

const (
	rootPath = "ohmlab/platforms/e2e/core/rustfs"
	record1  = "ohmlab/platforms/e2e/core/app1/s3"
	bucket1  = "ohmlab-app1-pg"
	caOne    = "-----BEGIN CERTIFICATE-----\nCA-ONE\n-----END CERTIFICATE-----\n"
)

// rootAccess and rootSecret are the root keys of the store, made for each run.
var rootAccess, rootSecret = mustKeyPair()

func mustKeyPair() (string, string) {
	ak, sk, err := newKeyPair()
	if err != nil {
		panic(err)
	}
	return ak, sk
}

var nsCounter atomic.Int64

// harness is a manager and a reconciler in a namespace of their own, with a
// KV and a store that outlive a restart.
type harness struct {
	t       *testing.T
	ns      string
	kv      *seedtest.MemKV
	store   *fakeStore
	caFile  string
	dials   atomic.Int32
	reg     *prometheus.Registry
	metrics *metrics.Metrics
	rec     *Reconciler
	mgr     ctrl.Manager
	cancel  context.CancelFunc
	done    chan struct{}
}

type option func(*Reconciler)

func observeOnly(r *Reconciler) { r.ObserveOnly = true }
func allowDelete(r *Reconciler) { r.AllowDelete = true }

func grace(d time.Duration) option { return func(r *Reconciler) { r.RotationGrace = d } }

func start(t *testing.T, opts ...option) *harness {
	t.Helper()
	h := prepare(t, opts...)
	h.run()
	return h
}

// prepare sets a harness up without starting its manager.
func prepare(t *testing.T, opts ...option) *harness {
	t.Helper()
	ns := fmt.Sprintf("t%d-%d", time.Now().UnixNano()%100000, nsCounter.Add(1))
	if err := direct.Create(context.Background(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}); err != nil {
		t.Fatal(err)
	}
	reg := prometheus.NewRegistry()
	h := &harness{t: t, ns: ns, kv: seedtest.New(), store: newFakeStore(), reg: reg, metrics: metrics.New(reg)}
	h.caFile = filepath.Join(t.TempDir(), "ca.crt")
	h.writeCA(caOne)
	h.kv.Put(rootPath, map[string]any{"admin": map[string]any{"accessKey": rootAccess, "secretKey": rootSecret}}, nil)
	h.build(opts...)
	return h
}

func (h *harness) build(opts ...option) {
	h.t.Helper()
	mgr, err := ctrl.NewManager(restCfg, ctrl.Options{
		Scheme:                 scheme,
		Cache:                  cache.Options{DefaultNamespaces: map[string]cache.Config{h.ns: {}}},
		Metrics:                metricsserver.Options{BindAddress: "0"},
		HealthProbeBindAddress: "0",
	})
	if err != nil {
		h.t.Fatal(err)
	}
	h.mgr = mgr
	h.rec = &Reconciler{
		Client: mgr.GetClient(),
		KV:     h.kv,
		Stores: map[string]StoreConfig{"backups": {
			Name: "backups", Endpoint: "https://rustfs-svc.e2e-store.svc:9000", Region: "us-east-1", PathStyle: true,
			Capacity: 50 << 30, Root: config.SecretRef{Path: rootPath, Key: "admin"}, CAFile: h.caFile, Dial: h.dial,
		}},
		CAFile:        h.caFile,
		Recorder:      mgr.GetEventRecorder("test"),
		Metrics:       h.metrics,
		RotationGrace: 26 * time.Hour,
		Resync:        150 * time.Millisecond,
		BackoffBase:   40 * time.Millisecond,
		BackoffMax:    200 * time.Millisecond,
	}
	for _, o := range opts {
		o(h.rec)
	}
	if err := h.rec.SetupWithManager(mgr, true); err != nil {
		h.t.Fatal(err)
	}
}

// dial hands out the fake store when given the root keys stored in the KV.
func (h *harness) dial(accessKey, secretKey string) (Store, error) {
	h.dials.Add(1)
	if accessKey != rootAccess || secretKey != rootSecret {
		return nil, fmt.Errorf("dial with keys other than the root keys")
	}
	return h.store, nil
}

func (h *harness) run() {
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel, h.done = cancel, make(chan struct{})
	go func() {
		_ = h.mgr.Start(ctx)
		close(h.done)
	}()
	h.t.Cleanup(h.stop)
}

// stop stops the manager and waits for it, so a restart never runs two.
func (h *harness) stop() {
	if h.cancel == nil {
		return
	}
	h.cancel()
	<-h.done
	h.cancel = nil
}

func (h *harness) restart(opts ...option) {
	h.t.Helper()
	h.stop()
	h.build(opts...)
	h.run()
}

func (h *harness) writeCA(pem string) {
	h.t.Helper()
	if err := os.WriteFile(h.caFile, []byte(pem), 0o600); err != nil {
		h.t.Fatal(err)
	}
}

// claim creates an S3Bucket for app1's pg claim, edited by mutate.
func (h *harness) claim(name string, mutate ...func(*v1alpha1.S3Bucket)) *v1alpha1.S3Bucket {
	h.t.Helper()
	obj := &v1alpha1.S3Bucket{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: h.ns},
		Spec: v1alpha1.S3BucketSpec{
			Store:       "backups",
			Bucket:      bucket1,
			Credentials: v1alpha1.S3CredentialsRef{Path: record1, Key: "pg"},
			Quota:       resource.MustParse("64Mi"),
		},
	}
	for _, m := range mutate {
		m(obj)
	}
	if err := direct.Create(context.Background(), obj); err != nil {
		h.t.Fatal(err)
	}
	return obj
}

func (h *harness) get(name string) *v1alpha1.S3Bucket {
	h.t.Helper()
	var obj v1alpha1.S3Bucket
	if err := direct.Get(context.Background(), types.NamespacedName{Namespace: h.ns, Name: name}, &obj); err != nil {
		h.t.Fatal(err)
	}
	return &obj
}

// entry is one claim's entry of a record, as strings.
func (h *harness) entry(path, key string) map[string]string {
	data, _, _, _ := h.kv.Get(path)
	e, _ := data[key].(map[string]any)
	out := map[string]string{}
	for k, v := range e {
		out[k] = fmt.Sprint(v)
	}
	return out
}

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

func cond(obj *v1alpha1.S3Bucket, t string) *metav1.Condition {
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
