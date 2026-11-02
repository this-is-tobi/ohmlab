package runtime

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	v1alpha1 "github.com/this-is-tobi/ohmlab/utils/api/v1alpha1"
)

var (
	restCfg *rest.Config
	scheme  = runtime.NewScheme()
	direct  client.Client
	nsSeq   atomic.Int64
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
	_ = v1alpha1.AddToScheme(scheme)
	env := &envtest.Environment{CRDDirectoryPaths: []string{"../../../../argo-cd/apps/ohmlab-controller/files/crds"}, ErrorIfCRDPathMissing: true}
	var err error
	if restCfg, err = env.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "start envtest:", err)
		os.Exit(1)
	}
	direct, _ = client.New(restCfg, client.Options{Scheme: scheme})
	code := m.Run()
	_ = env.Stop()
	os.Exit(code)
}

func leaseNamespace(t *testing.T) string {
	t.Helper()
	ns := fmt.Sprintf("lease-%d-%d", time.Now().UnixNano()%100000, nsSeq.Add(1))
	if err := direct.Create(context.Background(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}); err != nil {
		t.Fatal(err)
	}
	return ns
}

func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().String()
}

// active counts the replicas whose leader-only work is running, and the most
// there ever were at once.
type active struct{ now, max atomic.Int32 }

type leaderWork struct{ a *active }

func (w leaderWork) NeedLeaderElection() bool { return true }
func (w leaderWork) Start(ctx context.Context) error {
	n := w.a.now.Add(1)
	for {
		m := w.a.max.Load()
		if n <= m || w.a.max.CompareAndSwap(m, n) {
			break
		}
	}
	<-ctx.Done()
	w.a.now.Add(-1)
	return nil
}

func target(name string, a *active) Target {
	return Target{Name: name, Namespaces: []string{"default"}, Setup: func(mgr ctrl.Manager) error {
		return mgr.Add(manager.Runnable(leaderWork{a: a}))
	}}
}

func opts(ns string) Options {
	return Options{
		Scheme: scheme, LeaseNamespace: ns, SkipNameValidation: true,
		LeaseDuration: 2 * time.Second, RenewDeadline: time.Second, RetryPeriod: 200 * time.Millisecond,
		Gatherer: prometheus.NewRegistry(),
	}
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestOnlyOneReplicaRunsATargetAndAnotherTakesOver(t *testing.T) {
	ns := leaseNamespace(t)
	var a active

	ctxA, cancelA := context.WithCancel(context.Background())
	ctxB, cancelB := context.WithCancel(context.Background())
	defer cancelB()
	errA, errB := make(chan error, 1), make(chan error, 1)
	go func() { errA <- Run(ctxA, restCfg, opts(ns), []Target{target("vault", &a)}) }()
	go func() { errB <- Run(ctxB, restCfg, opts(ns), []Target{target("vault", &a)}) }()

	eventually(t, "one replica to lead", func() bool { return a.now.Load() == 1 })
	time.Sleep(1500 * time.Millisecond) // long enough for the standby to try and fail
	if a.now.Load() != 1 || a.max.Load() != 1 {
		t.Fatalf("running now %d, at most %d: a target ran twice", a.now.Load(), a.max.Load())
	}

	var lease coordinationv1.Lease
	if err := direct.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: "ohmlab-controller-vault"}, &lease); err != nil {
		t.Fatalf("the Lease has the target's name: %v", err)
	}

	// Stop whoever leads: the other must take over, and never alongside.
	cancelA()
	<-errA
	eventually(t, "the standby to take over", func() bool { return a.now.Load() == 1 })
	if a.max.Load() != 1 {
		t.Errorf("the two replicas overlapped (max %d)", a.max.Load())
	}
	cancelB()
	if err := <-errB; err != nil {
		t.Errorf("Run: %v", err)
	}
}

func TestEachTargetKeepsItsOwnLease(t *testing.T) {
	ns := leaseNamespace(t)
	var a, b active
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errc := make(chan error, 1)
	go func() { errc <- Run(ctx, restCfg, opts(ns), []Target{target("one", &a), target("two", &b)}) }()

	eventually(t, "both targets to lead", func() bool { return a.now.Load() == 1 && b.now.Load() == 1 })
	for _, name := range []string{"ohmlab-controller-one", "ohmlab-controller-two"} {
		var lease coordinationv1.Lease
		if err := direct.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: name}, &lease); err != nil {
			t.Errorf("lease %s: %v", name, err)
		}
	}
	cancel()
	if err := <-errc; err != nil {
		t.Errorf("Run: %v", err)
	}
}

func TestProbesAndMetricsAreOneEndpointEach(t *testing.T) {
	ns := leaseNamespace(t)
	reg := prometheus.NewRegistry()
	c := prometheus.NewCounter(prometheus.CounterOpts{Name: "ohmlab_probe_test_total", Help: "x"})
	reg.MustRegister(c)
	c.Inc()

	o := opts(ns)
	o.Gatherer, o.ProbeAddr, o.MetricsAddr = reg, freeAddr(t), freeAddr(t)
	var a, b active
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errc := make(chan error, 1)
	go func() { errc <- Run(ctx, restCfg, o, []Target{target("one", &a), target("two", &b)}) }()

	get := func(addr, path string) (int, string) {
		resp, err := http.Get("http://" + addr + path)
		if err != nil {
			return 0, ""
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body)
	}
	eventually(t, "readyz", func() bool { code, _ := get(o.ProbeAddr, "/readyz"); return code == 200 })
	if code, _ := get(o.ProbeAddr, "/healthz"); code != 200 {
		t.Errorf("healthz = %d", code)
	}
	if code, body := get(o.MetricsAddr, "/metrics"); code != 200 || !strings.Contains(body, "ohmlab_probe_test_total 1") {
		t.Errorf("metrics = %d %q", code, body)
	}
	cancel()
	<-errc
}

func TestRunRefusesNothingToDoAndDuplicates(t *testing.T) {
	var a active
	if err := Run(context.Background(), restCfg, opts("default"), nil); err == nil {
		t.Error("no target must be an error")
	}
	if err := Run(context.Background(), restCfg, opts("default"), []Target{target("x", &a), target("x", &a)}); err == nil {
		t.Error("a duplicate target must be an error")
	}
}
