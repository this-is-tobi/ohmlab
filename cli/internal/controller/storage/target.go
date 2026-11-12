package storage

import (
	"context"
	"fmt"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/this-is-tobi/ohmlab/cli/internal/controller/config"
	"github.com/this-is-tobi/ohmlab/cli/internal/controller/metrics"
	"github.com/this-is-tobi/ohmlab/cli/internal/controller/runtime"
	"github.com/this-is-tobi/ohmlab/cli/internal/rustfs"
	"github.com/this-is-tobi/ohmlab/cli/internal/vault"
)

// NewTarget builds the storage target from its configuration. It logs in to
// Vault with its own role, and reads each store's root keys there itself.
func NewTarget(cfg *config.Storage, resync time.Duration, m *metrics.Metrics) (runtime.Target, error) {
	grace, err := cfg.Grace()
	if err != nil {
		return runtime.Target{}, err
	}
	kv, err := vault.NewClient(vault.ClientConfig{
		Address:       cfg.Backend.Address,
		CACertPath:    cfg.Backend.CAFile,
		SkipTLSVerify: cfg.Backend.SkipTLSVerify,
	})
	if err != nil {
		return runtime.Target{}, fmt.Errorf("vault client: %w", err)
	}
	kv.UseKubernetesLogin(cfg.Backend.Auth.Mount, cfg.Backend.Auth.Role, cfg.Backend.Auth.TokenFile)

	stores := map[string]StoreConfig{}
	for _, s := range cfg.Stores {
		capacity, err := s.CapacityBytes()
		if err != nil {
			return runtime.Target{}, fmt.Errorf("store %s: %w", s.Name, err)
		}
		sc := StoreConfig{
			Name: s.Name, Endpoint: s.Endpoint, Region: s.Region, BucketPrefix: s.BucketPrefix, PathStyle: s.PathStyle,
			Capacity: capacity, Root: s.RootCredentials, CAFile: cfg.CAFile,
		}
		sc.Dial = func(accessKey, secretKey string) (Store, error) {
			return rustfs.New(rustfs.Config{Endpoint: sc.Endpoint, CAFile: sc.CAFile, Region: sc.Region, AccessKey: accessKey, SecretKey: secretKey})
		}
		stores[s.Name] = sc
		m.StoreCapacity.WithLabelValues(s.Name).Set(float64(capacity))
	}

	return runtime.Target{
		Name:       Target,
		Namespaces: cfg.WatchNamespaces,
		Setup: func(mgr ctrl.Manager) error {
			r := &Reconciler{
				Client:        mgr.GetClient(),
				KV:            kv,
				Stores:        stores,
				CAFile:        cfg.CAFile,
				Recorder:      mgr.GetEventRecorder("ohmlab-controller-" + Target),
				Metrics:       m,
				ObserveOnly:   cfg.ObserveOnly,
				AllowDelete:   cfg.AllowDelete,
				RotationGrace: grace,
				Resync:        resync,
			}
			return r.SetupWithManager(mgr, false)
		},
		Check: func(ctx context.Context, mgr ctrl.Manager) error {
			return runtime.CheckCRDRevision(ctx, mgr.GetAPIReader(), m, Target, kind, crdName)
		},
	}, nil
}
