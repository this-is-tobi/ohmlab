package certificates

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// settle waits for a few resyncs, so a write the controller should not make
// would have happened.
func settle() { time.Sleep(10 * 150 * time.Millisecond) }

func TestFirstSightRollsTheWorkload(t *testing.T) {
	ns := newNamespace(t)
	h := start(t, ns, restart("app-tls", deploy("app")))
	h.createDeployment("app", map[string]string{"checksum/config": "x"})
	h.createCertificate("app-tls")
	h.setRevision("app-tls", 1)

	h.rolledTo("Deployment", "app", "1")
	if got := h.annotation("Deployment", "app", "checksum/config"); got != "x" {
		t.Errorf("checksum/config = %q, the controller must leave other annotations alone", got)
	}
	h.eventually("the restart to be counted", func() bool { return h.restarts("Deployment", "app") == 1 })
	h.eventually("a Restarted event on the Certificate", func() bool { return h.event("Restarted", "app-tls") })
	h.eventually("the Certificate to be ready", func() bool { return h.ready("app-tls") == 1 })
}

func TestARenewalRollsEveryListedWorkload(t *testing.T) {
	ns := newNamespace(t)
	h := start(t, ns, restart("app-tls", deploy("app"), sts("db")))
	h.createDeployment("app", nil)
	h.createStatefulSet("db", false)
	h.createCertificate("app-tls")
	h.setRevision("app-tls", 1)
	h.rolledTo("Deployment", "app", "1")
	h.rolledTo("StatefulSet", "db", "1")

	h.setRevision("app-tls", 2)
	h.rolledTo("Deployment", "app", "2")
	h.rolledTo("StatefulSet", "db", "2")
	h.eventually("two restarts each", func() bool {
		return h.restarts("Deployment", "app") == 2 && h.restarts("StatefulSet", "db") == 2
	})
}

func TestAResyncWithNothingToDoWritesNothing(t *testing.T) {
	ns := newNamespace(t)
	h := start(t, ns, restart("app-tls", deploy("app")))
	h.createDeployment("app", nil)
	h.createCertificate("app-tls")
	h.setRevision("app-tls", 1)
	h.rolledTo("Deployment", "app", "1")
	gen := h.generation("Deployment", "app")

	settle()
	if got := h.generation("Deployment", "app"); got != gen {
		t.Errorf("generation %d -> %d: a resync rolled the workload again", gen, got)
	}
	if got := h.restarts("Deployment", "app"); got != 1 {
		t.Errorf("restarts = %v, want 1", got)
	}
}

func TestUnlistedObjectsAreNeverTouched(t *testing.T) {
	ns := newNamespace(t)
	h := start(t, ns, restart("app-tls", deploy("app")))
	h.createDeployment("app", nil)
	h.createDeployment("bystander", nil)
	h.createCertificate("app-tls")
	h.createCertificate("other-tls")
	h.setRevision("app-tls", 1)
	h.setRevision("other-tls", 1)
	h.rolledTo("Deployment", "app", "1")

	h.setRevision("other-tls", 2)
	settle()
	if got := h.revisionOf("Deployment", "bystander"); got != "" {
		t.Errorf("bystander carries %q", got)
	}
	if got := h.generation("Deployment", "bystander"); got != 1 {
		t.Errorf("bystander generation = %d", got)
	}
	if h.hasSeries("ohmlab_workload_restarts_total", "name", "bystander") {
		t.Error("bystander has a restart series")
	}
	if got := h.revisionOf("Deployment", "app"); got != "1" {
		t.Errorf("an unlisted Certificate's renewal changed app to %q", got)
	}
}

func TestTheAnnotationIsPutBackWhenSomeoneChangesIt(t *testing.T) {
	ns := newNamespace(t)
	h := start(t, ns, restart("app-tls", deploy("app")))
	h.createDeployment("app", nil)
	h.createCertificate("app-tls")
	h.setRevision("app-tls", 1)
	h.rolledTo("Deployment", "app", "1")

	patch := []byte(`{"spec":{"template":{"metadata":{"annotations":{"` + Annotation + `":"x"}}}}}`)
	obj := h.workload("Deployment", "app")
	if err := direct.Patch(context.Background(), obj, client.RawPatch(types.MergePatchType, patch)); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for h.revisionOf("Deployment", "app") != "1" {
		if time.Now().After(deadline) {
			t.Fatal("the annotation was not put back within 2s")
		}
		time.Sleep(25 * time.Millisecond)
	}
	h.eventually("a second restart", func() bool { return h.restarts("Deployment", "app") == 2 })
}

func TestAnApplyByAnotherManagerKeepsTheAnnotation(t *testing.T) {
	ns := newNamespace(t)
	h := start(t, ns, restart("app-tls", deploy("app")))
	h.createDeployment("app", map[string]string{"checksum/config": "x"})
	h.createCertificate("app-tls")
	h.setRevision("app-tls", 1)
	h.rolledTo("Deployment", "app", "1")

	// What Argo CD applies: the chart's manifest, which knows nothing of the key.
	manifest := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata":   map[string]any{"name": "app", "namespace": ns},
		"spec": map[string]any{
			"selector": map[string]any{"matchLabels": map[string]any{"app": "app"}},
			"template": map[string]any{
				"metadata": map[string]any{"labels": map[string]any{"app": "app"}, "annotations": map[string]any{"checksum/config": "y"}},
				"spec":     map[string]any{"containers": []any{map[string]any{"name": "c", "image": "busybox"}}},
			},
		},
	}}
	if err := direct.Apply(context.Background(), client.ApplyConfigurationFromUnstructured(manifest), client.FieldOwner("argocd-controller"), client.ForceOwnership); err != nil {
		t.Fatal(err)
	}
	if got := h.annotation("Deployment", "app", "checksum/config"); got != "y" {
		t.Fatalf("checksum/config = %q, the apply did not land", got)
	}
	settle()
	if got := h.revisionOf("Deployment", "app"); got != "1" {
		t.Errorf("the annotation is %q after another manager's apply", got)
	}
	if got := h.restarts("Deployment", "app"); got != 1 {
		t.Errorf("restarts = %v, want 1", got)
	}
	want := `{"f:spec":{"f:template":{"f:metadata":{"f:annotations":{"f:` + Annotation + `":{}}}}}}`
	if got := h.ownedFields("Deployment", "app", FieldOwner); got != want {
		t.Errorf("%s owns %s, want %s", FieldOwner, got, want)
	}
}

func TestARevisionMissedWhileStoppedIsCaughtUpOnce(t *testing.T) {
	ns := newNamespace(t)
	h := start(t, ns, restart("app-tls", deploy("app")))
	h.createDeployment("app", nil)
	h.createCertificate("app-tls")
	h.setRevision("app-tls", 1)
	h.rolledTo("Deployment", "app", "1")
	h.stop()
	gen := h.generation("Deployment", "app")

	h.setRevision("app-tls", 2)
	h2 := start(t, ns, restart("app-tls", deploy("app")))
	h2.rolledTo("Deployment", "app", "2")
	settle()
	if got := h2.generation("Deployment", "app"); got != gen+1 {
		t.Errorf("generation %d -> %d, want exactly one roll", gen, got)
	}
	if got := h2.restarts("Deployment", "app"); got != 1 {
		t.Errorf("restarts after the restart = %v, want 1", got)
	}
}

func TestARecreatedCertificateRollsAgain(t *testing.T) {
	ns := newNamespace(t)
	h := start(t, ns, restart("app-tls", deploy("app")))
	h.createDeployment("app", nil)
	h.createCertificate("app-tls")
	h.setRevision("app-tls", 1)
	h.setRevision("app-tls", 2)
	h.rolledTo("Deployment", "app", "2")

	h.deleteCertificate("app-tls")
	h.eventually("the Certificate to be gone", func() bool {
		err := direct.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: "app-tls"}, newCertificate(ns, "app-tls"))
		return err != nil
	})
	h.createCertificate("app-tls")
	h.setRevision("app-tls", 1)
	h.rolledTo("Deployment", "app", "1")
}

func TestAMissingWorkloadDoesNotStopTheOthers(t *testing.T) {
	ns := newNamespace(t)
	h := start(t, ns, restart("app-tls", deploy("ghost"), deploy("app")))
	h.createDeployment("app", nil)
	h.createCertificate("app-tls")
	h.setRevision("app-tls", 1)

	h.rolledTo("Deployment", "app", "1")
	h.eventually("a WorkloadNotFound event", func() bool { return h.event("WorkloadNotFound", "app-tls") })
	settle()
	if got := h.ready("app-tls"); got != 0 {
		t.Errorf("ready = %v with a listed workload missing", got)
	}
	if got := testutil.ToFloat64(h.metrics.BackendRequests.WithLabelValues(Target, "get", "notfound")); got < 1 {
		t.Errorf("notfound gets = %v", got)
	}
}

func TestACertificateNotIssuedYetWaits(t *testing.T) {
	ns := newNamespace(t)
	h := start(t, ns, restart("app-tls", deploy("app")))
	h.createDeployment("app", nil)
	h.createCertificate("app-tls")

	settle()
	if got := h.revisionOf("Deployment", "app"); got != "" {
		t.Errorf("app carries %q before the first issuance", got)
	}
	if !h.hasSeries("ohmlab_object_ready", "name", "app-tls") || h.ready("app-tls") != 0 {
		t.Error("the Certificate must be reported, not ready")
	}
	h.setRevision("app-tls", 1)
	h.rolledTo("Deployment", "app", "1")
	h.eventually("the Certificate to be ready", func() bool { return h.ready("app-tls") == 1 })
}

func TestOnDeleteWarns(t *testing.T) {
	ns := newNamespace(t)
	h := start(t, ns, restart("db-tls", sts("db")))
	h.createStatefulSet("db", true)
	h.createCertificate("db-tls")
	h.setRevision("db-tls", 1)

	h.rolledTo("StatefulSet", "db", "1")
	h.eventually("an OnDelete warning", func() bool { return h.event("OnDelete", "db-tls") })
}

// A listed Certificate that never existed sends no event: the controller must
// still report it, or a typo would let a certificate expire unnoticed.
func TestAnAbsentCertificateIsReported(t *testing.T) {
	ns := newNamespace(t)
	h := start(t, ns, restart("ghost-tls", deploy("app")))
	h.createDeployment("app", nil)

	h.eventually("the absent Certificate to be reported", func() bool { return h.hasSeries("ohmlab_object_ready", "name", "ghost-tls") })
	if got := h.ready("ghost-tls"); got != 0 {
		t.Errorf("ready = %v for a Certificate that does not exist", got)
	}
	if got := h.revisionOf("Deployment", "app"); got != "" {
		t.Errorf("app carries %q", got)
	}
}
