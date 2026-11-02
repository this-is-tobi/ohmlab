package vaultseed

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/this-is-tobi/ohmlab/utils/api/v1alpha1"
	"github.com/this-is-tobi/ohmlab/utils/internal/controller/config"
	"github.com/this-is-tobi/ohmlab/utils/internal/controller/metrics"
	"github.com/this-is-tobi/ohmlab/utils/internal/controller/runtime"
	"github.com/this-is-tobi/ohmlab/utils/internal/vault"
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
			return CheckCRDRevision(ctx, mgr.GetAPIReader(), m)
		},
	}, nil
}

// CheckCRDRevision compares the revision of the installed CRD with the one this
// controller was built with, and exposes the result. A catalog and an image that
// were released apart can disagree for a while; this is how that is seen.
func CheckCRDRevision(ctx context.Context, r client.Reader, m *metrics.Metrics) error {
	var crd apiextensionsv1.CustomResourceDefinition
	if err := r.Get(ctx, types.NamespacedName{Name: crdName}, &crd); err != nil {
		return fmt.Errorf("read CRD %s: %w", crdName, err)
	}
	got := crd.Annotations[v1alpha1.RevisionAnnotation]
	want := fmt.Sprint(v1alpha1.APIRevision)
	if got == want {
		m.CRDMismatch.WithLabelValues(Target, kind).Set(0)
		return nil
	}
	m.CRDMismatch.WithLabelValues(Target, kind).Set(1)
	slog.Warn("the installed CRD was generated at another API revision", "crd", crdName, "installed", got, "built-with", want)
	return nil
}
