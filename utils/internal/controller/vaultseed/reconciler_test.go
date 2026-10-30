package vaultseed

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/this-is-tobi/ohmlab/utils/api/v1alpha1"
	"github.com/this-is-tobi/ohmlab/utils/internal/secretgen"
	"github.com/this-is-tobi/ohmlab/utils/internal/seed"
	"github.com/this-is-tobi/ohmlab/utils/internal/vault"
)

func TestSeedsAPathAndReportsReady(t *testing.T) {
	h := start(t)
	p := h.path("keycloak")
	h.create("keycloak", p, `{"admin":{"username":"admin","password":"<random:24>"},"webhook":""}`)
	h.ready("keycloak")

	data, meta, version, exists := h.kv.Get(p)
	if !exists || version != 1 || len(data["admin"].(map[string]any)["password"].(string)) != 24 {
		t.Fatalf("stored = %v v%d", data, version)
	}
	if meta[seed.OwnerKey] != h.ns+"/keycloak" {
		t.Errorf("owner = %q", meta[seed.OwnerKey])
	}
	obj := h.get("keycloak")
	if obj.Status.KVVersion != 1 || obj.Status.ObservedGeneration != obj.Generation ||
		strings.Join(obj.Status.ManagedKeys, ",") != "admin.password,admin.username,webhook" {
		t.Errorf("status = %+v", obj.Status)
	}
	if obj.Status.Conditions[0].Reason != v1alpha1.ReasonReconciled {
		t.Errorf("conditions = %+v", obj.Status.Conditions)
	}
}

func TestAResyncThatFindsNothingWritesNothingAnywhere(t *testing.T) {
	h := start(t)
	h.create("a", h.path("a"), `{"pw":"<random:12>","n":{"k":"<hex:8>"}}`)
	h.ready("a")

	rv := h.get("a").ResourceVersion
	reads, dw, mw := h.kv.Counters()
	time.Sleep(1200 * time.Millisecond) // several resyncs at 150ms

	reads2, dw2, mw2 := h.kv.Counters()
	if reads2 <= reads {
		t.Fatalf("no resync happened: %d reads", reads2)
	}
	if dw2 != dw || mw2 != mw {
		t.Errorf("a no-op resync wrote to Vault: data %d->%d metadata %d->%d", dw, dw2, mw, mw2)
	}
	if got := h.get("a").ResourceVersion; got != rv {
		t.Errorf("a no-op resync wrote to the API: resourceVersion %s -> %s", rv, got)
	}
}

func TestADeletedGeneratedKeyComesBackAndAHandFilledPlaceholderStays(t *testing.T) {
	h := start(t)
	p := h.path("b")
	h.create("b", p, `{"keep":"<random:8>","gone":"<random:8>","webhook":""}`)
	h.ready("b")
	data, meta, _, _ := h.kv.Get(p)
	data["webhook"] = "https://hooks.example/x" // filled in by a human
	oldKeep := data["keep"]
	delete(data, "gone")
	h.kv.Put(p, data, meta)

	h.eventually("the deleted key to be regenerated", func() bool {
		d, _, _, _ := h.kv.Get(p)
		return d["gone"] != nil
	})
	d, _, _, _ := h.kv.Get(p)
	if d["webhook"] != "https://hooks.example/x" || d["keep"] != oldKeep {
		t.Errorf("hand-filled or existing keys changed: %v", d)
	}
	h.eventually("the write event to name the key", func() bool { return strings.Contains(dumpEvents(t, h.ns), ": 1 add-key (gone)") })
}

func TestAReferenceFollowsItsSourceAndAHandEditIsPutBack(t *testing.T) {
	h := start(t)
	src, dst := h.path("keycloak"), h.path("grafana")
	h.kv.Put(src, map[string]any{"clients": map[string]any{"g": map[string]any{"secret": "S1"}}}, nil)
	h.create("grafana", dst, `{"secret":"<ref:`+src+`#clients.g.secret>"}`)
	h.ready("grafana")
	if d, _, _, _ := h.kv.Get(dst); d["secret"] != "S1" {
		t.Fatalf("stored = %v", d)
	}

	h.kv.Put(src, map[string]any{"clients": map[string]any{"g": map[string]any{"secret": "S2"}}}, nil)
	h.eventually("the copy to follow the source", func() bool { d, _, _, _ := h.kv.Get(dst); return d["secret"] == "S2" })

	_, meta, _, _ := h.kv.Get(dst)
	h.kv.Put(dst, map[string]any{"secret": "edited-by-hand"}, meta)
	h.eventually("the hand edit to be reverted", func() bool { d, _, _, _ := h.kv.Get(dst); return d["secret"] == "S2" })
}

func TestAPendingReferenceWaitsThenSucceeds(t *testing.T) {
	h := start(t)
	src, dst := h.path("source"), h.path("copy")
	h.create("copy", dst, `{"k":"<ref:`+src+`#value>","own":"<random:6>"}`)

	h.reason("copy", v1alpha1.ConditionReady, v1alpha1.ReasonRefPending)
	if c := cond(h.get("copy"), v1alpha1.ConditionReconciling); c == nil || c.Status != metav1.ConditionTrue {
		t.Errorf("a waiting object is Reconciling: %+v", h.get("copy").Status.Conditions)
	}
	if _, _, _, exists := h.kv.Get(dst); exists {
		t.Fatal("nothing may be written while a reference is pending")
	}

	h.kv.Put(src, map[string]any{"value": "V"}, nil)
	h.ready("copy")
	if cond(h.get("copy"), v1alpha1.ConditionReconciling) != nil {
		t.Error("Reconciling must clear once Ready")
	}
}

func TestReferencesThatFormACycleStall(t *testing.T) {
	// Both objects exist before the controller starts: one it reconciled
	// alone would read Vault for a reference that has no cycle yet.
	h := prepare(t)
	a, b := h.path("a"), h.path("b")
	h.create("a", a, `{"x":"<ref:`+b+`#y>"}`)
	h.create("b", b, `{"y":"<ref:`+a+`#x>"}`)
	h.run()
	h.reason("a", v1alpha1.ConditionStalled, v1alpha1.ReasonRefCycle)
	h.reason("b", v1alpha1.ConditionStalled, v1alpha1.ReasonRefCycle)
	if msg := cond(h.get("a"), v1alpha1.ConditionStalled).Message; !strings.Contains(msg, a) || !strings.Contains(msg, b) {
		t.Errorf("the message must name the cycle: %q", msg)
	}
	if reads, dw, _ := h.kv.Counters(); reads != 0 || dw != 0 {
		t.Errorf("a cycle must be caught before Vault is touched: %d reads %d writes", reads, dw)
	}
}

func TestASemanticErrorStallsWithoutTouchingVault(t *testing.T) {
	h := start(t)
	h.create("bad", h.path("bad"), `{"k":"<hex:7>","h":"<bcrypt:nope>"}`)
	h.reason("bad", v1alpha1.ConditionStalled, v1alpha1.ReasonInvalidSpec)
	msg := cond(h.get("bad"), v1alpha1.ConditionStalled).Message
	if !strings.Contains(msg, "k:") || !strings.Contains(msg, "h:") {
		t.Errorf("message = %q", msg)
	}
	if reads, _, _ := h.kv.Counters(); reads != 0 {
		t.Errorf("reads = %d", reads)
	}
	// Fixing the spec un-stalls it.
	var obj v1alpha1.VaultSeed
	obj = *h.get("bad")
	obj.Spec.Data.Raw = []byte(`{"k":"<hex:8>"}`)
	if err := direct.Update(context.Background(), &obj); err != nil {
		t.Fatal(err)
	}
	h.ready("bad")
	if cond(h.get("bad"), v1alpha1.ConditionStalled) != nil {
		t.Error("Stalled must clear")
	}
}

func TestAPathOwnedByAnotherObjectIsLeftAlone(t *testing.T) {
	h := start(t)
	p := h.path("shared")
	h.create("first", p, `{"a":"<random:6>"}`)
	h.ready("first")
	h.create("second", p, `{"b":"<random:6>"}`)
	h.reason("second", v1alpha1.ConditionReady, v1alpha1.ReasonOwnedByAnother)
	if d, _, _, _ := h.kv.Get(p); d["b"] != nil {
		t.Errorf("the second object wrote: %v", d)
	}
	if cond(h.get("second"), v1alpha1.ConditionStalled) != nil {
		t.Error("an owned path is not a stall: the owner marker can change")
	}
}

func TestAdoptsAPathTheJobCreatedWithoutANewVersion(t *testing.T) {
	h := start(t)
	p := h.path("legacy")
	h.kv.Put(p, map[string]any{"pw": "from-the-job"}, nil)
	h.create("legacy", p, `{"pw":"<random:12>"}`)
	h.ready("legacy")
	d, meta, version, _ := h.kv.Get(p)
	if d["pw"] != "from-the-job" || version != 1 || meta[seed.OwnerKey] != h.ns+"/legacy" {
		t.Errorf("data=%v version=%d meta=%v", d, version, meta)
	}
	// Metrics are recorded just after the status is written.
	h.eventually("the adoption to be counted", func() bool {
		return testutil.ToFloat64(h.metrics.Adopted.WithLabelValues("vault", "VaultSeed")) == 1
	})
}

func TestABackendErrorRetriesAndRecovers(t *testing.T) {
	h := start(t)
	p := h.path("flaky")
	h.kv.FailReads(p, errors.New("vault is sealed"))
	h.create("flaky", p, `{"a":"<random:6>"}`)
	h.reason("flaky", v1alpha1.ConditionReady, v1alpha1.ReasonBackendUnavailable)

	h.kv.ClearFailures()
	h.ready("flaky")
}

func TestAForbiddenBackendIsReportedAsSuch(t *testing.T) {
	h := start(t)
	p := h.path("denied")
	h.kv.FailReads(p, vault.ErrForbidden)
	h.create("denied", p, `{"a":"<random:6>"}`)
	h.reason("denied", v1alpha1.ConditionReady, v1alpha1.ReasonForbidden)
}

func TestRotationHappensOnceForAToken(t *testing.T) {
	h := start(t)
	p := h.path("rot")
	h.create("rot", p, `{"pw":"<random:12>","keep":"<random:12>"}`)
	h.ready("rot")
	before, _, _, _ := h.kv.Get(p)

	obj := h.get("rot")
	obj.Spec.Rotate = &v1alpha1.RotateSpec{Token: "2026-10-12", Keys: []string{"pw"}}
	if err := direct.Update(context.Background(), obj); err != nil {
		t.Fatal(err)
	}
	h.eventually("the rotation", func() bool { d, _, _, _ := h.kv.Get(p); return d["pw"] != before["pw"] })
	after, meta, version, _ := h.kv.Get(p)
	if after["keep"] != before["keep"] || meta[seed.RotationTokenKey] != "2026-10-12" {
		t.Errorf("after=%v meta=%v", after, meta)
	}
	time.Sleep(700 * time.Millisecond)
	if _, _, v2, _ := h.kv.Get(p); v2 != version {
		t.Errorf("a recorded token rotated again: v%d -> v%d", version, v2)
	}
	h.eventually("the rotation to be counted", func() bool {
		return testutil.ToFloat64(h.metrics.Rotations.WithLabelValues("vault")) == 1
	})
}

func TestObserveOnlyReportsDriftAndWritesNothing(t *testing.T) {
	h := start(t, observeOnly)
	h.kv.Put(h.path("legacy"), map[string]any{"pw": "x"}, nil)
	h.create("legacy", h.path("legacy"), `{"pw":"<random:8>","extra":"<random:8>"}`)
	h.create("fresh", h.path("fresh"), `{"a":"<random:8>"}`)

	h.reason("legacy", v1alpha1.ConditionDrifted, v1alpha1.ReasonDrifted)
	h.reason("fresh", v1alpha1.ConditionDrifted, v1alpha1.ReasonDrifted)
	if c := cond(h.get("legacy"), v1alpha1.ConditionReady); c.Status != metav1.ConditionFalse || c.Reason != v1alpha1.ReasonObserveOnly {
		t.Errorf("ready = %+v", c)
	}
	// The message names the key behind each change, so nobody has to read the
	// path to find it.
	if msg := cond(h.get("legacy"), v1alpha1.ConditionDrifted).Message; !strings.Contains(msg, "1 add-key (extra)") || !strings.Contains(msg, "1 adopt") {
		t.Errorf("drifted message = %q", msg)
	}
	if st := h.get("legacy").Status; len(st.ManagedKeys) != 0 {
		t.Errorf("nothing was written, so nothing is managed: %+v", st)
	}
	time.Sleep(500 * time.Millisecond)
	if _, dw, mw := h.kv.Counters(); dw != 0 || mw != 0 {
		t.Errorf("observe-only wrote: data %d metadata %d", dw, mw)
	}
	if _, _, _, exists := h.kv.Get(h.path("fresh")); exists {
		t.Error("observe-only created a path")
	}
	// legacy plans an adopt and an add-key; fresh plans one add-key.
	for action, want := range map[string]float64{"adopt": 1, "add-key": 2} {
		action, want := action, want
		h.eventually("planned "+action, func() bool {
			return testutil.ToFloat64(h.metrics.PlannedChanges.WithLabelValues("vault", "VaultSeed", action)) == want
		})
	}
}

func TestDeletionPolicyDeleteRemovesOnlyAPathItOwns(t *testing.T) {
	h := start(t, allowDelete)
	mine, other := h.path("mine"), h.path("other")
	del := func(o *v1alpha1.VaultSeed) { o.Spec.DeletionPolicy = v1alpha1.DeletionPolicyDelete }
	h.create("mine", mine, `{"a":"<random:6>"}`, del)
	h.ready("mine")
	if !hasFinalizer(h.get("mine")) {
		t.Fatal("deletionPolicy Delete needs the finalizer")
	}
	if err := direct.Delete(context.Background(), h.get("mine")); err != nil {
		t.Fatal(err)
	}
	h.eventually("the path to be removed", func() bool { _, _, _, e := h.kv.Get(mine); return !e })
	h.eventually("the object to go", func() bool {
		return apierrors.IsNotFound(direct.Get(context.Background(), client.ObjectKey{Namespace: h.ns, Name: "mine"}, &v1alpha1.VaultSeed{}))
	})

	// A path somebody else owns is not removed, even though the object asked.
	h.kv.Put(other, map[string]any{"a": "1"}, map[string]string{seed.OwnerKey: "another/seed"})
	h.create("claims", other, `{"a":"<random:6>"}`, del)
	h.reason("claims", v1alpha1.ConditionReady, v1alpha1.ReasonOwnedByAnother)
	if err := direct.Delete(context.Background(), h.get("claims")); err != nil {
		t.Fatal(err)
	}
	h.eventually("the object to go", func() bool {
		return apierrors.IsNotFound(direct.Get(context.Background(), client.ObjectKey{Namespace: h.ns, Name: "claims"}, &v1alpha1.VaultSeed{}))
	})
	if _, _, _, exists := h.kv.Get(other); !exists {
		t.Error("a path owned by another object was removed")
	}
}

func TestRetainIsTheDefaultAndNeverAddsAFinalizer(t *testing.T) {
	h := start(t, allowDelete)
	h.create("r", h.path("r"), `{"a":"<random:6>"}`)
	h.ready("r")
	if hasFinalizer(h.get("r")) {
		t.Error("Retain must not block deletion")
	}
	if err := direct.Delete(context.Background(), h.get("r")); err != nil {
		t.Fatal(err)
	}
	h.eventually("the object to go", func() bool {
		return apierrors.IsNotFound(direct.Get(context.Background(), client.ObjectKey{Namespace: h.ns, Name: "r"}, &v1alpha1.VaultSeed{}))
	})
	if _, _, _, exists := h.kv.Get(h.path("r")); !exists {
		t.Error("Retain removed the path")
	}
}

func TestDeletePolicyWithoutAllowDeleteStalls(t *testing.T) {
	h := start(t) // AllowDelete is false
	h.create("d", h.path("d"), `{"a":"<random:6>"}`, func(o *v1alpha1.VaultSeed) { o.Spec.DeletionPolicy = v1alpha1.DeletionPolicyDelete })
	h.reason("d", v1alpha1.ConditionStalled, v1alpha1.ReasonForbidden)
	if hasFinalizer(h.get("d")) {
		t.Error("no finalizer without allowDelete: deleting the object must never block")
	}
}

func TestAForbiddenDeleteKeepsTheFinalizerAndSaysSo(t *testing.T) {
	h := start(t, allowDelete)
	h.create("f", h.path("f"), `{"a":"<random:6>"}`, func(o *v1alpha1.VaultSeed) { o.Spec.DeletionPolicy = v1alpha1.DeletionPolicyDelete })
	h.ready("f")
	h.kv.FailDestroy(vault.ErrForbidden)
	if err := direct.Delete(context.Background(), h.get("f")); err != nil {
		t.Fatal(err)
	}
	h.reason("f", v1alpha1.ConditionReady, v1alpha1.ReasonForbidden)
	if !hasFinalizer(h.get("f")) {
		t.Error("the path is still there, so the finalizer stays")
	}
}

func TestNoSecretValueEverReachesALogAnEventTheStatusOrAMetric(t *testing.T) {
	const canary = "CANARY-7f3a9c-DO-NOT-LEAK"
	h := start(t)
	src, dst := h.path("source"), h.path("target")
	h.kv.Put(src, map[string]any{"v": canary}, nil)
	h.create("target", dst, `{"copy":"<ref:`+src+`#v>","own":"<random:16>"}`)
	h.ready("target")
	// A change of the source, a hand edit and a failure all produce logs and events too.
	h.kv.Put(src, map[string]any{"v": canary + "-2"}, nil)
	h.eventually("the copy to follow", func() bool { d, _, _, _ := h.kv.Get(dst); return d["copy"] == canary+"-2" })
	h.kv.FailReads(dst, errors.New("vault is sealed"))
	h.reason("target", v1alpha1.ConditionReady, v1alpha1.ReasonBackendUnavailable)

	own, _, _, _ := h.kv.Get(dst)
	secrets := []string{canary, own["own"].(string)}

	events := dumpEvents(t, h.ns)
	status, _ := jsonOf(h.get("target"))
	mfs, _ := h.reg.Gather()
	var metricText strings.Builder
	for _, mf := range mfs {
		metricText.WriteString(mf.String())
	}
	for name, haystack := range map[string]string{"logs": logBuf.String(), "events": events, "object": status, "metrics": metricText.String()} {
		for _, s := range secrets {
			if strings.Contains(haystack, s) {
				t.Errorf("a secret value reached the %s", name)
			}
		}
	}
	if logBuf.String() == "" {
		t.Error("the canary check read an empty log: the logger is not wired")
	}
}

func TestTheCRDRefusesWhatTheSchemaForbids(t *testing.T) {
	h := start(t)
	// empty data
	obj := &v1alpha1.VaultSeed{}
	obj.Name, obj.Namespace = "empty", h.ns
	obj.Spec.Path = h.path("e")
	obj.Spec.Data.Raw = []byte(`{}`)
	if err := direct.Create(context.Background(), obj); err == nil {
		t.Error("data: {} must be refused")
	}
	// bad path
	obj.Name, obj.Spec.Path, obj.Spec.Data.Raw = "badpath", "no-slash", []byte(`{"a":"b"}`)
	if err := direct.Create(context.Background(), obj); err == nil {
		t.Error("a path without a mount must be refused")
	}
	// path is immutable
	h.create("fixed", h.path("fixed"), `{"a":"<random:4>"}`)
	cur := h.get("fixed")
	cur.Spec.Path = h.path("moved")
	if err := direct.Update(context.Background(), cur); err == nil {
		t.Error("changing the path must be refused")
	}
}

func hasFinalizer(o *v1alpha1.VaultSeed) bool {
	for _, f := range o.Finalizers {
		if f == v1alpha1.Finalizer {
			return true
		}
	}
	return false
}

func TestObjectsOutsideTheAllowListedNamespacesAreIgnored(t *testing.T) {
	h := start(t)
	other := h.ns + "-other"
	if err := direct.Create(context.Background(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: other}}); err != nil {
		t.Fatal(err)
	}
	stranger := &v1alpha1.VaultSeed{
		ObjectMeta: metav1.ObjectMeta{Name: "stranger", Namespace: other},
		Spec:       v1alpha1.VaultSeedSpec{Path: h.path("stranger"), Data: apiextensionsv1.JSON{Raw: []byte(`{"a":"<random:6>"}`)}},
	}
	if err := direct.Create(context.Background(), stranger); err != nil {
		t.Fatal(err)
	}
	h.create("inside", h.path("inside"), `{"a":"<random:6>"}`)
	h.ready("inside")

	time.Sleep(600 * time.Millisecond)
	var got v1alpha1.VaultSeed
	if err := direct.Get(context.Background(), client.ObjectKeyFromObject(stranger), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Status.Conditions) != 0 {
		t.Errorf("an object outside the allow-list was reconciled: %+v", got.Status)
	}
	if _, _, _, exists := h.kv.Get(h.path("stranger")); exists {
		t.Error("an object outside the allow-list was written to Vault")
	}
}

// failMetadata makes every custom-metadata write fail the way a policy without
// the capability does, after the data write has gone through.
type failMetadata struct{ KV }

func (failMetadata) KVSetCustomMetadata(context.Context, string, string, map[string]string) error {
	return fmt.Errorf("write metadata: %w", vault.ErrForbidden)
}

func withoutMetadataWrites(r *Reconciler) { r.KV = failMetadata{r.KV} }

func TestAMarkerThatCannotBeWrittenStillShowsWhatWasWritten(t *testing.T) {
	h := start(t, withoutMetadataWrites)
	p := h.path("rot")
	h.kv.Put(p, map[string]any{"pw": "old"}, map[string]string{seed.OwnerKey: h.ns + "/rot"})
	h.create("rot", p, `{"pw":"<random:12>"}`, func(o *v1alpha1.VaultSeed) {
		o.Spec.Rotate = &v1alpha1.RotateSpec{Token: "t1"}
	})
	h.reason("rot", v1alpha1.ConditionReady, v1alpha1.ReasonForbidden)
	h.eventually("the written version and key to reach the status", func() bool {
		o := h.get("rot")
		return o.Status.KVVersion >= 2 && len(o.Status.ManagedKeys) == 1
	})
	h.eventually("the rotation to be an event", func() bool { return strings.Contains(dumpEvents(t, h.ns), "Rotated") })
}

func TestAReferenceCycleClearsOnceTheOtherObjectIsFixed(t *testing.T) {
	h := start(t)
	a, b := h.path("a"), h.path("b")
	h.create("a", a, `{"x":"<ref:`+b+`#y>"}`)
	h.create("b", b, `{"y":"<ref:`+a+`#x>"}`)
	h.reason("a", v1alpha1.ConditionStalled, v1alpha1.ReasonRefCycle)
	// b settles too before it is updated: a can stall first, and b's own
	// status patch would then make the update below conflict.
	h.reason("b", v1alpha1.ConditionStalled, v1alpha1.ReasonRefCycle)

	fixed := h.get("b")
	fixed.Spec.Data = apiextensionsv1.JSON{Raw: []byte(`{"y":"<random:8>"}`)}
	if err := direct.Update(context.Background(), fixed); err != nil {
		t.Fatal(err)
	}
	h.ready("b")
	h.ready("a") // a's own spec never changed
}

func TestAHashOfAValueThatIsStillEmptyWaitsForTheValue(t *testing.T) {
	h := start(t)
	p := h.path("hashed")
	h.create("hashed", p, `{"password":"","passwordHashed":"<bcrypt:password>"}`)
	h.reason("hashed", v1alpha1.ConditionStalled, v1alpha1.ReasonInvalidSpec)

	h.kv.Put(p, map[string]any{"password": "hunter2"}, nil)
	h.ready("hashed")
	d, _, _, _ := h.kv.Get(p)
	if hash, _ := d["passwordHashed"].(string); !secretgen.BcryptMatches(hash, "hunter2") {
		t.Errorf("the hash must follow the value that was filled in: %v", d)
	}
}

func TestDescribeNamesTheKeysAndCutsALongList(t *testing.T) {
	changes := []seed.Change{{Action: seed.ActionUpdateField, Key: "keycloak.clientSecret"}, {Action: seed.ActionAdopt}}
	for i := 6; i >= 0; i-- {
		changes = append(changes, seed.Change{Action: seed.ActionAddKey, Key: fmt.Sprintf("k%d", i)})
	}
	want := "7 add-key (k0, k1, k2, k3, k4, and 2 more), 1 adopt, 1 update-field (keycloak.clientSecret)"
	if got := describe(changes); got != want {
		t.Errorf("describe = %q, want %q", got, want)
	}
}
