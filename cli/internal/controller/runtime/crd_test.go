package runtime

import (
	"context"
	"fmt"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/types"

	v1alpha1 "github.com/this-is-tobi/ohmlab/cli/api/v1alpha1"
	"github.com/this-is-tobi/ohmlab/cli/internal/controller/metrics"
)

var kinds = []struct{ target, kind, crd string }{
	{"vault", "VaultSeed", "vaultseeds.ohmlab.fr"},
	{"storage", "S3Bucket", "s3buckets.ohmlab.fr"},
}

func TestACRDAtAnotherRevisionRaisesTheMismatchGauge(t *testing.T) {
	for _, k := range kinds {
		t.Run(k.kind, func(t *testing.T) {
			ctx := context.Background()
			m := metrics.New(prometheus.NewRegistry())
			gauge := func() float64 { return testutil.ToFloat64(m.CRDMismatch.WithLabelValues(k.target, k.kind)) }

			if err := CheckCRDRevision(ctx, direct, m, k.target, k.kind, k.crd); err != nil || gauge() != 0 {
				t.Fatalf("the generated CRD matches the build: err=%v gauge=%v", err, gauge())
			}

			var crd apiextensionsv1.CustomResourceDefinition
			if err := direct.Get(ctx, types.NamespacedName{Name: k.crd}, &crd); err != nil {
				t.Fatal(err)
			}
			original := crd.Annotations[v1alpha1.RevisionAnnotation]
			t.Cleanup(func() {
				var c apiextensionsv1.CustomResourceDefinition
				_ = direct.Get(ctx, types.NamespacedName{Name: k.crd}, &c)
				c.Annotations[v1alpha1.RevisionAnnotation] = original
				_ = direct.Update(ctx, &c)
			})
			crd.Annotations[v1alpha1.RevisionAnnotation] = "99"
			if err := direct.Update(ctx, &crd); err != nil {
				t.Fatal(err)
			}
			if err := CheckCRDRevision(ctx, direct, m, k.target, k.kind, k.crd); err != nil || gauge() != 1 {
				t.Errorf("a CRD at another revision: err=%v gauge=%v", err, gauge())
			}
		})
	}
}

func TestAMissingCRDIsAnError(t *testing.T) {
	m := metrics.New(prometheus.NewRegistry())
	if err := CheckCRDRevision(context.Background(), direct, m, "storage", "S3Bucket", "nothing.ohmlab.fr"); err == nil {
		t.Error("want an error for a CRD that is not installed")
	}
}

func TestTheGeneratedCRDsCarryTheBuildRevision(t *testing.T) {
	for _, k := range kinds {
		var crd apiextensionsv1.CustomResourceDefinition
		if err := direct.Get(context.Background(), types.NamespacedName{Name: k.crd}, &crd); err != nil {
			t.Fatal(err)
		}
		if got, want := crd.Annotations[v1alpha1.RevisionAnnotation], fmt.Sprint(v1alpha1.APIRevision); got != want {
			t.Errorf("%s: annotation %q, APIRevision %q: bump both together", k.crd, got, want)
		}
	}
}
