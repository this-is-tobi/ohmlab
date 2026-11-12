package vaultseed

import (
	"context"
	"fmt"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/this-is-tobi/ohmlab/cli/internal/controller/config"
	"github.com/this-is-tobi/ohmlab/cli/internal/controller/metrics"
	"github.com/this-is-tobi/ohmlab/cli/internal/controller/runtime"
	"github.com/this-is-tobi/ohmlab/cli/internal/vault"
)

const crdName = "vaultseeds.ohmlab.fr"

// NewTarget builds the vault target from its configuration. Only this target's
// section reaches it, and the Vault client it creates lives only in it.
func NewTarget(cfg *config.Vault, resync time.Duration, m *metrics.Metrics) (runtime.Target, error) {
	client, err := vault.NewClient(vault.ClientConfig{
		Address:       cfg.Backend.Address,
		CACertPath:    cfg.Backend.CAFile,
		SkipTLSVerify: cfg.Backend.SkipTLSVerify,
	})
	if err != nil {
		return runtime.Target{}, fmt.Errorf("vault client: %w", err)
	}
	client.UseKubernetesLogin(cfg.Backend.Auth.Mount, cfg.Backend.Auth.Role, cfg.Backend.Auth.TokenFile)

	return runtime.Target{
		Name:       Target,
		Namespaces: cfg.WatchNamespaces,
		Setup: func(mgr ctrl.Manager) error {
			r := &Reconciler{
				Client:      mgr.GetClient(),
				KV:          client,
				Recorder:    mgr.GetEventRecorder("ohmlab-controller-" + Target),
				Metrics:     m,
				ObserveOnly: cfg.ObserveOnly,
				AllowDelete: cfg.AllowDelete,
				Resync:      resync,
			}
			return r.SetupWithManager(mgr, false)
		},
		Check: func(ctx context.Context, mgr ctrl.Manager) error {
			return runtime.CheckCRDRevision(ctx, mgr.GetAPIReader(), m, Target, kind, crdName)
		},
	}, nil
}
