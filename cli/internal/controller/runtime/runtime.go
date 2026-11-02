// Package runtime runs the controller's targets in one process. Every target
// gets a manager of its own: its own cache (limited to the namespaces it may
// read), its own work queue and its own Lease, so a target is never run twice
// by a rolling update and a shared process shares nothing but the probe and
// metrics endpoints.
package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

// Target is one backend the controller configures.
type Target struct {
	// Name is the target's name: it names the Lease, ohmlab-controller-<Name>.
	Name string
	// Namespaces are the only namespaces the target reads objects from.
	Namespaces []string
	// Setup registers the target's controllers.
	Setup func(mgr ctrl.Manager) error
	// Check runs once at start, on every replica, with a reader that does not
	// go through the cache. A failure is logged, never fatal.
	Check func(ctx context.Context, mgr ctrl.Manager) error
}

// Options configure Run.
type Options struct {
	Scheme         *runtime.Scheme
	LeaseNamespace string
	// MetricsAddr and ProbeAddr are the listen addresses of the two endpoints
	// of the process. Empty disables one.
	MetricsAddr string
	ProbeAddr   string
	// Gatherer is what /metrics serves.
	Gatherer prometheus.Gatherer
	// SkipNameValidation allows several processes' controllers of the same name in
	// one test binary.
	SkipNameValidation bool
	// Lease timings; zero uses controller-runtime's defaults (15s, 10s, 2s).
	LeaseDuration, RenewDeadline, RetryPeriod time.Duration
}

type targetState struct {
	name  string
	ready atomic.Bool
}

// Run starts every target and returns when ctx is done or one of them fails.
func Run(ctx context.Context, restCfg *rest.Config, opts Options, targets []Target) error {
	if len(targets) == 0 {
		return errors.New("no target to run")
	}
	seen := map[string]bool{}
	states := make([]*targetState, 0, len(targets))
	managers := make([]ctrl.Manager, 0, len(targets))

	for _, t := range targets {
		if seen[t.Name] {
			return fmt.Errorf("target %q listed twice", t.Name)
		}
		seen[t.Name] = true

		namespaces := map[string]cache.Config{}
		for _, ns := range t.Namespaces {
			namespaces[ns] = cache.Config{}
		}
		mo := ctrl.Options{
			Scheme:                        opts.Scheme,
			Cache:                         cache.Options{DefaultNamespaces: namespaces},
			Metrics:                       metricsserver.Options{BindAddress: "0"},
			HealthProbeBindAddress:        "0",
			LeaderElection:                true,
			LeaderElectionID:              "ohmlab-controller-" + t.Name,
			LeaderElectionNamespace:       opts.LeaseNamespace,
			LeaderElectionReleaseOnCancel: true,
		}
		if opts.LeaseDuration > 0 {
			mo.LeaseDuration = &opts.LeaseDuration
		}
		if opts.RenewDeadline > 0 {
			mo.RenewDeadline = &opts.RenewDeadline
		}
		if opts.RetryPeriod > 0 {
			mo.RetryPeriod = &opts.RetryPeriod
		}
		mgr, err := ctrl.NewManager(restCfg, mo)
		if err != nil {
			return fmt.Errorf("target %s: %w", t.Name, err)
		}
		if err := t.Setup(mgr); err != nil {
			return fmt.Errorf("target %s: %w", t.Name, err)
		}
		st := &targetState{name: t.Name}
		if err := mgr.Add(&observer{mgr: mgr, state: st, check: t.Check, target: t.Name}); err != nil {
			return fmt.Errorf("target %s: %w", t.Name, err)
		}
		states = append(states, st)
		managers = append(managers, mgr)
	}

	g, gctx := errgroup.WithContext(ctx)
	for i, mgr := range managers {
		i, mgr := i, mgr
		g.Go(func() error {
			if err := mgr.Start(gctx); err != nil {
				return fmt.Errorf("target %s: %w", states[i].name, err)
			}
			return nil
		})
	}
	if opts.ProbeAddr != "" {
		g.Go(func() error { return serve(gctx, opts.ProbeAddr, probeMux(states)) })
	}
	if opts.MetricsAddr != "" {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.HandlerFor(opts.Gatherer, promhttp.HandlerOpts{}))
		g.Go(func() error { return serve(gctx, opts.MetricsAddr, mux) })
	}
	return g.Wait()
}

// observer runs on every replica, leader or not: it reports when the target's
// cache has synced and runs the start-up check.
type observer struct {
	mgr    ctrl.Manager
	state  *targetState
	check  func(ctx context.Context, mgr ctrl.Manager) error
	target string
}

func (o *observer) NeedLeaderElection() bool { return false }

func (o *observer) Start(ctx context.Context) error {
	if !o.mgr.GetCache().WaitForCacheSync(ctx) {
		return ctx.Err()
	}
	o.state.ready.Store(true)
	if o.check != nil {
		if err := o.check(ctx, o.mgr); err != nil {
			slog.Warn("start-up check failed", "target", o.target, "error", err.Error())
		}
	}
	<-ctx.Done()
	return nil
}

// probeMux serves /healthz (the process is up) and /readyz (every target's
// cache has synced). Neither looks at a backend: a target whose backend is
// down reports it through its objects' conditions and its metrics, and does not
// make the pod, or the other targets in it, look unhealthy.
func probeMux(states []*targetState) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		for _, s := range states {
			if !s.ready.Load() {
				http.Error(w, "target "+s.name+" has not synced its cache", http.StatusServiceUnavailable)
				return
			}
		}
		w.WriteHeader(http.StatusOK)
	})
	return mux
}

func serve(ctx context.Context, addr string, h http.Handler) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
