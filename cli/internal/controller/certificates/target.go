package certificates

import (
	"time"

	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/this-is-tobi/ohmlab/cli/internal/controller/config"
	"github.com/this-is-tobi/ohmlab/cli/internal/controller/metrics"
	"github.com/this-is-tobi/ohmlab/cli/internal/controller/runtime"
)

// NewTarget builds the certificates target from its configuration. It watches
// only the namespaces its restarts name.
func NewTarget(cfg *config.Certificates, resync time.Duration, m *metrics.Metrics) (runtime.Target, error) {
	restarts := index(cfg.Restarts)
	return runtime.Target{
		Name:       Target,
		Namespaces: cfg.Namespaces(),
		Setup: func(mgr ctrl.Manager) error {
			r := &Reconciler{
				Certificates: mgr.GetCache(),
				Workloads:    mgr.GetAPIReader(),
				Client:       mgr.GetClient(),
				Recorder:     mgr.GetEventRecorder("ohmlab-controller-" + Target),
				Metrics:      m,
				Restarts:     restarts,
				Resync:       resync,
			}
			return r.SetupWithManager(mgr, false)
		},
	}, nil
}
