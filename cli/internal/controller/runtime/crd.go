package runtime

import (
	"context"
	"fmt"
	"log/slog"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/this-is-tobi/ohmlab/cli/api/v1alpha1"
	"github.com/this-is-tobi/ohmlab/cli/internal/controller/metrics"
)

// CheckCRDRevision compares the revision of an installed CRD with the one this
// controller was built with, and exposes the result. A catalog and an image
// that were released apart can disagree for a while; this is how that is seen.
func CheckCRDRevision(ctx context.Context, r client.Reader, m *metrics.Metrics, target, kind, crd string) error {
	var obj apiextensionsv1.CustomResourceDefinition
	if err := r.Get(ctx, types.NamespacedName{Name: crd}, &obj); err != nil {
		return fmt.Errorf("read CRD %s: %w", crd, err)
	}
	got := obj.Annotations[v1alpha1.RevisionAnnotation]
	want := fmt.Sprint(v1alpha1.APIRevision)
	if got == want {
		m.CRDMismatch.WithLabelValues(target, kind).Set(0)
		return nil
	}
	m.CRDMismatch.WithLabelValues(target, kind).Set(1)
	slog.Warn("the installed CRD was generated at another API revision", "crd", crd, "installed", got, "built-with", want)
	return nil
}
