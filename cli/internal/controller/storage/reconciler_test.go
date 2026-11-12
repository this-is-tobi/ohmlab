package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	eventsv1 "k8s.io/api/events/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/this-is-tobi/ohmlab/cli/api/v1alpha1"
	"github.com/this-is-tobi/ohmlab/cli/internal/rustfs"
)

const caTwo = "-----BEGIN CERTIFICATE-----\nCA-TWO\n-----END CERTIFICATE-----\n"

var policy1 = "ohmlab-" + bucket1 + "-readWrite"

// dumpEvents returns every event of a namespace as text.
func dumpEvents(t *testing.T, ns string) string {
	t.Helper()
	var list eventsv1.EventList
	if err := direct.List(context.Background(), &list, client.InNamespace(ns)); err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	for _, e := range list.Items {
		b, _ := json.Marshal(e)
		sb.Write(b)
	}
	return sb.String()
}

func (h *harness) update(name string, edit func(*v1alpha1.S3Bucket)) {
	h.t.Helper()
	for range 10 {
		obj := h.get(name)
		edit(obj)
		err := direct.Update(context.Background(), obj)
		if err == nil {
			return
		}
		if !apierrors.IsConflict(err) {
			h.t.Fatal(err)
		}
	}
	h.t.Fatalf("update %s: too many conflicts", name)
}

func (h *harness) remove(name string) {
	h.t.Helper()
	if err := direct.Delete(context.Background(), h.get(name)); err != nil {
		h.t.Fatal(err)
	}
	h.eventually(name+" to be gone", func() bool {
		var obj v1alpha1.S3Bucket
		return apierrors.IsNotFound(direct.Get(context.Background(), types.NamespacedName{Namespace: h.ns, Name: name}, &obj))
	})
}

func (h *harness) keys(user string) []string {
	h.store.mu.Lock()
	defer h.store.mu.Unlock()
	return h.store.keysOf(user)
}

func TestAFreshClaimGetsItsBucketUserAndRecord(t *testing.T) {
	h := start(t)
	h.claim("ohmlab-app1-pg")
	h.ready("ohmlab-app1-pg")

	if b, ok := h.store.bucket(bucket1); !ok || !b.versioning || b.noncurrentDays != 7 || b.expireDays != 0 || b.quota != 64<<20 {
		t.Errorf("bucket = %+v (exists %v)", b, ok)
	}
	want, _ := rustfs.BucketPolicy(bucket1, "readWrite")
	if got := h.store.policy(policy1); string(got) != string(want) {
		t.Errorf("policy %s = %s", policy1, got)
	}
	if u, ok := h.store.user(bucket1); !ok || !slices.Equal(u.policies, []string{policy1}) {
		t.Errorf("user = %+v (exists %v)", u, ok)
	}
	e := h.entry(record1, "pg")
	if keys := h.keys(bucket1); len(keys) != 1 || keys[0] != e["accessKey"] {
		t.Errorf("the store holds %d keys, want the record's only", len(keys))
	}
	if k, _ := h.store.keyOf(e["accessKey"]); k.secret != e["secretKey"] || k.user != bucket1 {
		t.Error("the store's key is not the record's")
	}
	for field, v := range map[string]string{"endpoint": "https://rustfs-svc.e2e-store.svc:9000", "bucket": bucket1, "region": "us-east-1", "pathStyle": "true", "ca": caOne} {
		if e[field] != v {
			t.Errorf("record %s = %q, want %q", field, e[field], v)
		}
	}
	if got := testutil.ToFloat64(h.metrics.BucketQuota.WithLabelValues("backups", bucket1)); got != 64<<20 {
		t.Errorf("ohmlab_storage_bucket_quota_bytes = %v", got)
	}
	if st := h.get("ohmlab-app1-pg").Status; st.KVVersion != 1 || st.QuotaBytes != 64<<20 {
		t.Errorf("status = %+v", st)
	}
}

func TestObserveOnlyWritesNothingAndListsTheChanges(t *testing.T) {
	h := start(t, observeOnly)
	h.claim("a")
	h.reason("a", v1alpha1.ConditionReady, v1alpha1.ReasonObserveOnly)

	d := cond(h.get("a"), v1alpha1.ConditionDrifted)
	if d == nil {
		t.Fatal("no Drifted condition")
	}
	for _, name := range []string{"create-bucket", "add-service-account", "accessKey", "secretKey", "endpoint", "ca"} {
		if !strings.Contains(d.Message, name) {
			t.Errorf("Drifted does not name %s: %q", name, d.Message)
		}
	}
	if regexp.MustCompile(`[A-Z0-9]{20}|[A-Za-z0-9]{40}`).MatchString(d.Message) {
		t.Errorf("Drifted carries something shaped like a key: %q", d.Message)
	}
	if n := h.store.totalWrites(); n != 0 {
		t.Errorf("observe-only wrote %d times to the store", n)
	}
	if _, dw, mw := h.kv.Counters(); dw != 0 || mw != 0 {
		t.Errorf("observe-only wrote to Vault: data %d, metadata %d", dw, mw)
	}

	storeActions := []string{"create-bucket", "set-versioning", "set-lifecycle", "set-quota", "put-policy", "create-user", "attach-policy", "detach-policy", "add-service-account", "remove-service-account"}
	planned, total := map[string]int{}, 0
	for _, a := range storeActions {
		n := int(testutil.ToFloat64(h.metrics.PlannedChanges.WithLabelValues("storage", "S3Bucket", a)))
		planned[a], total = n, total+n
	}
	if total == 0 || planned["create-bucket"] != 1 || planned["add-service-account"] != 1 {
		t.Fatalf("ohmlab_planned_changes = %v", planned)
	}

	h.restart()
	h.ready("a")
	for _, a := range storeActions {
		if got := h.store.writeCount(a); got != planned[a] {
			t.Errorf("live: %s done %d times, planned %d", a, got, planned[a])
		}
	}
	if _, dw, mw := h.kv.Counters(); dw != 1 || mw != 1 {
		t.Errorf("live: data %d, metadata %d writes, want 1 and 1", dw, mw)
	}
}

func TestAResyncWithNothingToDoWritesNothing(t *testing.T) {
	h := start(t)
	h.claim("a")
	h.ready("a")

	rv := h.get("a").ResourceVersion
	writes := h.store.totalWrites()
	reads, dw, mw := h.kv.Counters()
	time.Sleep(1700 * time.Millisecond) // about 10 resyncs at 150 ms

	if reads2, _, _ := h.kv.Counters(); reads2 < reads+5 {
		t.Fatalf("no resync happened: %d reads, then %d", reads, reads2)
	}
	if got := h.store.totalWrites(); got != writes {
		t.Errorf("a resync wrote to the store: %v", h.store.opsLog()[writes:])
	}
	if _, dw2, mw2 := h.kv.Counters(); dw2 != dw || mw2 != mw {
		t.Errorf("a resync wrote to Vault: data %d->%d, metadata %d->%d", dw, dw2, mw, mw2)
	}
	if got := h.get("a").ResourceVersion; got != rv {
		t.Errorf("a resync wrote the status: resourceVersion %s -> %s", rv, got)
	}
}

func TestAStoreOutageConvergesWithoutAnOrphanKey(t *testing.T) {
	h := prepare(t)
	h.store.failUntilHealed("add-service-account", errFake)
	h.run()
	h.claim("a")
	h.reason("a", v1alpha1.ConditionReady, v1alpha1.ReasonBackendUnavailable)
	e := h.entry(record1, "pg")
	if e["accessKey"] == "" || e["secretKey"] == "" {
		t.Fatal("Vault must hold the key before the store does")
	}
	if c := cond(h.get("a"), v1alpha1.ConditionReconciling); c == nil {
		t.Error("an object waiting for its store is Reconciling")
	}

	h.store.heal("add-service-account")
	h.ready("a")
	if keys := h.keys(bucket1); len(keys) != 1 || keys[0] != e["accessKey"] {
		t.Errorf("the store holds %d keys, want only the one Vault held during the outage", len(keys))
	}
	if _, _, v, _ := h.kv.Get(record1); v != 1 {
		t.Errorf("record version %d: the key was generated again", v)
	}
}

func TestARebuiltStoreGetsEverythingBackFromVault(t *testing.T) {
	h := start(t)
	h.claim("a")
	h.ready("a")
	e := h.entry(record1, "pg")
	_, _, version, _ := h.kv.Get(record1)

	h.store.wipe()
	h.eventually("the store to be rebuilt from the record", func() bool {
		keys := h.keys(bucket1)
		u, _ := h.store.user(bucket1)
		b, ok := h.store.bucket(bucket1)
		return ok && b.quota == 64<<20 && b.versioning && slices.Equal(u.policies, []string{policy1}) && len(keys) == 1 && keys[0] == e["accessKey"]
	})
	if k, _ := h.store.keyOf(e["accessKey"]); k.secret != e["secretKey"] {
		t.Error("the rebuilt key has another secret than the record's")
	}
	if _, _, v, _ := h.kv.Get(record1); v != version {
		t.Errorf("record version %d -> %d: a rebuild must not touch Vault", version, v)
	}
}

func TestAServiceAccountVaultDoesNotNameIsRemoved(t *testing.T) {
	h := start(t)
	h.claim("a")
	h.ready("a")
	h.store.addKey(bucket1, "STRAYKEYADDEDBYHAND0", "stray-secret")
	h.eventually("the stray key to be removed", func() bool { _, ok := h.store.keyOf("STRAYKEYADDEDBYHAND0"); return !ok })
	if keys := h.keys(bucket1); len(keys) != 1 || keys[0] != h.entry(record1, "pg")["accessKey"] {
		t.Errorf("the record's key must stay alone: %d keys", len(keys))
	}
	h.eventually("a Normal event naming it", func() bool {
		ev := dumpEvents(t, h.ns)
		return strings.Contains(ev, `"reason":"Removed"`) && strings.Contains(ev, "STRA…")
	})
}

func TestAPolicyTheClaimDoesNotNameIsDetached(t *testing.T) {
	h := start(t)
	h.claim("a")
	h.ready("a")
	h.store.attach("consoleAdmin", bucket1)
	h.eventually("the other policy to be detached", func() bool {
		u, _ := h.store.user(bucket1)
		return slices.Equal(u.policies, []string{policy1})
	})
}

func TestAKeyChangedInTheRecordIsFollowed(t *testing.T) {
	h := start(t)
	h.claim("a")
	h.ready("a")
	old := h.entry(record1, "pg")["accessKey"]

	data, meta, _, _ := h.kv.Get(record1)
	pg := data["pg"].(map[string]any)
	ak, sk, err := newKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	pg["accessKey"], pg["secretKey"] = ak, sk
	h.kv.Put(record1, data, meta)

	h.eventually("the store to follow the record", func() bool {
		keys := h.keys(bucket1)
		return len(keys) == 1 && keys[0] == ak
	})
	if k, _ := h.store.keyOf(ak); k.secret != sk {
		t.Error("the new key's secret is not the record's")
	}
	if _, ok := h.store.keyOf(old); ok {
		t.Error("the old key must be removed")
	}
}

func TestTwoClaimsOnOneKeyTheSecondIsRefused(t *testing.T) {
	h := start(t)
	h.claim("first")
	h.ready("first")
	e := h.entry(record1, "pg")

	h.claim("second")
	h.reason("second", v1alpha1.ConditionReady, v1alpha1.ReasonOwnedByAnother)
	if got := h.entry(record1, "pg"); got["accessKey"] != e["accessKey"] || got["secretKey"] != e["secretKey"] {
		t.Error("the second claim changed the first one's key")
	}
	if _, meta, _, _ := h.kv.Get(record1); meta["ohmlab.fr/claim.pg"] != h.ns+"/first" {
		t.Errorf("claim marker = %q", meta["ohmlab.fr/claim.pg"])
	}
	if keys := h.keys(bucket1); len(keys) != 1 || keys[0] != e["accessKey"] {
		t.Errorf("the store holds %d keys, want the first claim's only", len(keys))
	}
}

// record2 is app1's record in another scope: its pg claim implies bucket1 too.
const record2 = "ohmlab/platforms/e2e/tenant/app1/s3"

func TestTwoRecordsNamingOneBucketTheYoungerWaits(t *testing.T) {
	h := start(t)
	h.claim("first")
	h.ready("first")
	e := h.entry(record1, "pg")

	h.claim("second", func(o *v1alpha1.S3Bucket) { o.Spec.Credentials.Path = record2 })
	h.reason("second", v1alpha1.ConditionReady, v1alpha1.ReasonOwnedByAnother)
	if msg := cond(h.get("second"), v1alpha1.ConditionReady).Message; !strings.Contains(msg, h.ns+"/first") {
		t.Errorf("message = %q, want the holder named", msg)
	}
	time.Sleep(400 * time.Millisecond)
	if _, _, _, exists := h.kv.Get(record2); exists {
		t.Error("the younger claim wrote its record")
	}
	if keys := h.keys(bucket1); len(keys) != 1 || keys[0] != e["accessKey"] {
		t.Errorf("the store holds %d keys, want the first claim's only", len(keys))
	}
	for _, op := range h.store.opsLog() {
		if strings.HasPrefix(op, "remove-service-account") {
			t.Fatal("a key of the shared bucket was removed")
		}
	}
	h.ready("first")
}

func TestDeletingAWaitingClaimLeavesTheHolderAlone(t *testing.T) {
	h := start(t, allowDelete)
	del := func(o *v1alpha1.S3Bucket) { o.Spec.DeletionPolicy = v1alpha1.DeletionPolicyDelete }
	h.claim("first", del)
	h.ready("first")
	e := h.entry(record1, "pg")
	// As when the cache lagged: the younger claim wrote its markers and holds
	// the finalizer.
	h.kv.Put(record2, map[string]any{}, map[string]string{ownerKey: PathOwner, claimPrefix + "pg": h.ns + "/second"})
	h.claim("second", del, func(o *v1alpha1.S3Bucket) {
		o.Spec.Credentials.Path = record2
		o.Finalizers = []string{v1alpha1.S3BucketFinalizer}
	})
	h.reason("second", v1alpha1.ConditionReady, v1alpha1.ReasonOwnedByAnother)

	h.remove("second")
	if _, ok := h.store.keyOf(e["accessKey"]); !ok {
		t.Error("deleting the younger claim removed the holder's key")
	}
	if _, ok := h.store.user(bucket1); !ok {
		t.Error("deleting the younger claim removed the holder's user")
	}
	if _, ok := h.store.bucket(bucket1); !ok {
		t.Error("deleting the younger claim removed the holder's bucket")
	}
}

func TestABucketThatDoesNotMatchItsRecordIsRefused(t *testing.T) {
	h := start(t)
	h.claim("a", func(o *v1alpha1.S3Bucket) { o.Spec.Bucket = "ohmlab-app2-pg" })
	h.reason("a", v1alpha1.ConditionStalled, v1alpha1.ReasonInvalidSpec)
	if msg := cond(h.get("a"), v1alpha1.ConditionStalled).Message; !strings.Contains(msg, `bucket must be "ohmlab-app1-pg"`) {
		t.Errorf("message = %q", msg)
	}
	if h.store.totalWrites() != 0 || h.dials.Load() != 0 {
		t.Error("an invalid spec must not reach the store")
	}
	if _, dw, mw := h.kv.Counters(); dw != 0 || mw != 0 {
		t.Error("an invalid spec must not write to Vault")
	}
}

func TestAnUnknownStoreOrAQuotaOfZeroStalls(t *testing.T) {
	h := start(t)
	h.claim("unknown", func(o *v1alpha1.S3Bucket) { o.Spec.Store = "elsewhere" })
	h.claim("zero", func(o *v1alpha1.S3Bucket) { o.Spec.Quota = resource.MustParse("0") })
	h.reason("unknown", v1alpha1.ConditionStalled, v1alpha1.ReasonInvalidSpec)
	h.reason("zero", v1alpha1.ConditionStalled, v1alpha1.ReasonInvalidSpec)
	if msg := cond(h.get("unknown"), v1alpha1.ConditionStalled).Message; !strings.Contains(msg, `store "elsewhere" is not configured`) {
		t.Errorf("message = %q", msg)
	}
	if msg := cond(h.get("zero"), v1alpha1.ConditionStalled).Message; !strings.Contains(msg, "quota must be above zero") {
		t.Errorf("message = %q", msg)
	}
	if h.store.totalWrites() != 0 || h.dials.Load() != 0 {
		t.Error("an invalid spec must not reach the store")
	}
}

// hungStore stops answering InspectBucket while hung is set, as a store whose
// packets are dropped: a call returns only when its context ends.
type hungStore struct {
	Store
	hung *atomic.Bool
}

func (s hungStore) InspectBucket(ctx context.Context, name string) (rustfs.Bucket, error) {
	if s.hung.Load() {
		<-ctx.Done()
		return rustfs.Bucket{}, ctx.Err()
	}
	return s.Store.InspectBucket(ctx, name)
}

func TestAHungStoreFailsEachCallAtItsDeadline(t *testing.T) {
	var hung atomic.Bool
	hung.Store(true)
	h := start(t, func(r *Reconciler) {
		r.StoreTimeout = 300 * time.Millisecond
		sc := r.Stores["backups"]
		dial := sc.Dial
		sc.Dial = func(accessKey, secretKey string) (Store, error) {
			s, err := dial(accessKey, secretKey)
			if err != nil {
				return nil, err
			}
			return hungStore{Store: s, hung: &hung}, nil
		}
		r.Stores["backups"] = sc
	})
	h.claim("a")
	h.reason("a", v1alpha1.ConditionReady, v1alpha1.ReasonBackendUnavailable)
	if msg := cond(h.get("a"), v1alpha1.ConditionReady).Message; !strings.Contains(msg, "deadline exceeded") {
		t.Errorf("message = %q", msg)
	}
	hung.Store(false)
	h.ready("a")
}

func TestAccessDeniedDropsTheStoreClient(t *testing.T) {
	h := start(t)
	h.claim("a")
	h.ready("a")
	dials := h.dials.Load()
	h.store.failNext("inspect-bucket", fmt.Errorf("inspect-bucket: %w", rustfs.ErrForbidden))
	h.eventually("the root keys to be read again", func() bool { return h.dials.Load() > dials })
	h.ready("a")
	if !strings.Contains(dumpEvents(t, h.ns), `"reason":"Forbidden"`) {
		t.Error("no Forbidden warning")
	}
}

func TestRotationSurvivesARestart(t *testing.T) {
	h := start(t, grace(2*time.Second))
	h.claim("a")
	h.ready("a")
	old := h.entry(record1, "pg")["accessKey"]

	h.update("a", func(o *v1alpha1.S3Bucket) { o.Spec.Rotate = &v1alpha1.S3RotateSpec{Token: "r1"} })
	h.eventually("a second key", func() bool { return h.entry(record1, "pg")["accessKey"] != old && len(h.keys(bucket1)) == 2 })
	rotated := h.entry(record1, "pg")["accessKey"]

	h.restart(grace(2 * time.Second))
	h.eventually("the old key to be retired", func() bool { _, ok := h.store.keyOf(old); return !ok })
	time.Sleep(500 * time.Millisecond)

	removed := 0
	for _, op := range h.store.opsLog() {
		if op == "remove-service-account "+bucket1+" "+old {
			removed++
		}
	}
	if removed != 1 {
		t.Errorf("the old key was removed %d times, want once", removed)
	}
	if keys := h.keys(bucket1); len(keys) != 1 || keys[0] != rotated {
		t.Errorf("the store holds %d keys, want the rotated one only", len(keys))
	}
	_, meta, _, _ := h.kv.Get(record1)
	if _, ok := meta["ohmlab.fr/retiring.pg"]; ok || meta["ohmlab.fr/rotation-token.pg"] != "r1" {
		t.Errorf("markers = %v", meta)
	}
	ev := dumpEvents(t, h.ns)
	if !strings.Contains(ev, `"reason":"Rotated"`) || !strings.Contains(ev, `"reason":"Retired"`) {
		t.Error("want a Rotated and a Retired event")
	}
}

func TestARotationWhoseMarkersFailedToWriteKeepsTheOldKey(t *testing.T) {
	h := start(t)
	h.claim("a")
	h.ready("a")
	old := h.entry(record1, "pg")["accessKey"]
	h.kv.FailNextWrite("metadata", fmt.Errorf("write metadata: status 503"))
	h.update("a", func(o *v1alpha1.S3Bucket) { o.Spec.Rotate = &v1alpha1.S3RotateSpec{Token: "r1"} })
	h.rotationKeeps(old)
}

func TestARotationWhoseNewKeyFailedToWriteResumes(t *testing.T) {
	h := start(t)
	h.claim("a")
	h.ready("a")
	old := h.entry(record1, "pg")["accessKey"]
	h.kv.FailNextWrite("data", fmt.Errorf("write: status 503"))
	h.update("a", func(o *v1alpha1.S3Bucket) { o.Spec.Rotate = &v1alpha1.S3RotateSpec{Token: "r1"} })
	h.rotationKeeps(old)
}

// rotationKeeps waits for claim a's rotation to settle, then checks that the
// replaced key works until its grace ends and that the record took one
// rotation.
func (h *harness) rotationKeeps(old string) {
	h.t.Helper()
	h.eventually("the new key beside the old one", func() bool {
		cur := h.entry(record1, "pg")["accessKey"]
		_, meta, _, _ := h.kv.Get(record1)
		keys := h.keys(bucket1)
		return cur != old && meta["ohmlab.fr/rotation-token.pg"] == "r1" && len(keys) == 2 && slices.Contains(keys, cur) && slices.Contains(keys, old)
	})
	h.ready("a")
	time.Sleep(400 * time.Millisecond)
	_, meta, version, _ := h.kv.Get(record1)
	if !strings.HasPrefix(meta["ohmlab.fr/retiring.pg"], old+" ") {
		h.t.Error("the retiring marker must name the replaced key")
	}
	if _, ok := h.store.keyOf(old); !ok {
		h.t.Error("the replaced key must work until its grace ends")
	}
	if version != 2 {
		h.t.Errorf("record version %d, want 2: one rotation", version)
	}
}

func TestRetainLeavesEverything(t *testing.T) {
	h := start(t, allowDelete)
	h.claim("a")
	h.ready("a")
	e := h.entry(record1, "pg")
	h.remove("a")
	time.Sleep(300 * time.Millisecond)

	if _, ok := h.store.bucket(bucket1); !ok {
		t.Error("Retain removed the bucket")
	}
	if _, ok := h.store.user(bucket1); !ok || h.store.policy(policy1) == nil {
		t.Error("Retain removed the identity")
	}
	if _, ok := h.store.keyOf(e["accessKey"]); !ok || h.entry(record1, "pg")["accessKey"] != e["accessKey"] {
		t.Error("Retain removed the key")
	}
}

func TestDeleteRemovesTheIdentityAndKeepsABucketWithData(t *testing.T) {
	h := start(t, allowDelete)
	del := func(o *v1alpha1.S3Bucket) { o.Spec.DeletionPolicy = v1alpha1.DeletionPolicyDelete }
	h.claim("data", del)
	h.claim("empty", del, func(o *v1alpha1.S3Bucket) { o.Spec.Bucket, o.Spec.Credentials.Key = "ohmlab-app1-files", "files" })
	h.ready("data")
	h.ready("empty")
	if !slices.Contains(h.get("data").Finalizers, v1alpha1.S3BucketFinalizer) {
		t.Fatal("no finalizer on a Delete object")
	}
	h.store.putObject(bucket1)

	h.remove("data")
	h.remove("empty")
	if _, ok := h.store.bucket(bucket1); !ok {
		t.Error("a bucket holding an object was removed")
	}
	if _, ok := h.store.bucket("ohmlab-app1-files"); ok {
		t.Error("the empty bucket was kept")
	}
	for _, b := range []string{bucket1, "ohmlab-app1-files"} {
		if _, ok := h.store.user(b); ok || h.store.policy("ohmlab-"+b+"-readWrite") != nil || len(h.keys(b)) != 0 {
			t.Errorf("the identity of %s is left", b)
		}
	}
	last := map[string]int{}
	for i, op := range h.store.opsLog() {
		for _, kind := range []string{"remove-service-account " + bucket1, "remove-user " + bucket1, "remove-policy " + policy1} {
			if strings.HasPrefix(op, kind) {
				last[kind] = i
			}
		}
	}
	if !(last["remove-service-account "+bucket1] < last["remove-user "+bucket1] && last["remove-user "+bucket1] < last["remove-policy "+policy1]) {
		t.Errorf("the removal order is not service accounts, user, policy: %v", h.store.opsLog())
	}
	data, meta, _, _ := h.kv.Get(record1)
	if data["pg"] != nil || data["files"] != nil {
		t.Errorf("the record keeps a claim's key: %v", slices.Collect(func(yield func(string) bool) {
			for k := range data {
				if !yield(k) {
					return
				}
			}
		}))
	}
	for k := range meta {
		if strings.HasSuffix(k, ".pg") || strings.HasSuffix(k, ".files") {
			t.Errorf("the record keeps the marker %s", k)
		}
	}

	h2 := start(t)
	h2.claim("x", del)
	h2.reason("x", v1alpha1.ConditionStalled, v1alpha1.ReasonForbidden)
}

func TestACAChangeReachesTheRecord(t *testing.T) {
	h := start(t)
	h.claim("a")
	h.ready("a")
	dials := h.dials.Load()
	h.writeCA(caTwo)
	h.eventually("the record's ca to follow the file", func() bool { return h.entry(record1, "pg")["ca"] == caTwo })
	if h.dials.Load() <= dials {
		t.Error("the store client must be built again with the new CA")
	}
}

func TestTheAPIRefusesMalformedObjects(t *testing.T) {
	h := prepare(t)
	ctx := context.Background()
	for name, edit := range map[string]func(*v1alpha1.S3Bucket){
		"an uppercase bucket":             func(o *v1alpha1.S3Bucket) { o.Spec.Bucket = "Ohmlab-App1-Pg" },
		"a path not ending in /s3":        func(o *v1alpha1.S3Bucket) { o.Spec.Credentials.Path = "ohmlab/platforms/e2e/core/app1/secrets" },
		"a key with a dot":                func(o *v1alpha1.S3Bucket) { o.Spec.Credentials.Key = "p.g" },
		"a rotation without token":        func(o *v1alpha1.S3Bucket) { o.Spec.Rotate = &v1alpha1.S3RotateSpec{} },
		"a token Vault cannot keep":       func(o *v1alpha1.S3Bucket) { o.Spec.Rotate = &v1alpha1.S3RotateSpec{Token: strings.Repeat("r", 129)} },
		"a noncurrent expiry under a day": func(o *v1alpha1.S3Bucket) { o.Spec.Lifecycle.NoncurrentDays = -1 },
	} {
		obj := &v1alpha1.S3Bucket{Spec: v1alpha1.S3BucketSpec{Store: "backups", Bucket: bucket1, Credentials: v1alpha1.S3CredentialsRef{Path: record1, Key: "pg"}, Quota: resource.MustParse("1Mi")}}
		obj.Name, obj.Namespace = "bad", h.ns
		edit(obj)
		if err := direct.Create(ctx, obj); !apierrors.IsInvalid(err) {
			t.Errorf("%s: want Invalid, got %v", name, err)
			_ = direct.Delete(ctx, obj)
		}
	}

	h.claim("good")
	for name, edit := range map[string]func(*v1alpha1.S3Bucket){
		"a bucket changed":      func(o *v1alpha1.S3Bucket) { o.Spec.Bucket = "ohmlab-app1-other" },
		"a record path changed": func(o *v1alpha1.S3Bucket) { o.Spec.Credentials.Path = "ohmlab/platforms/e2e/core/app2/s3" },
		"a record key changed":  func(o *v1alpha1.S3Bucket) { o.Spec.Credentials.Key = "files" },
	} {
		obj := h.get("good")
		edit(obj)
		if err := direct.Update(ctx, obj); !apierrors.IsInvalid(err) {
			t.Errorf("%s: want Invalid, got %v", name, err)
		}
	}
	if got := h.get("good").Spec; got.Access != v1alpha1.S3AccessReadWrite || got.Versioning == nil || !*got.Versioning ||
		got.Lifecycle.NoncurrentDays != 7 || got.DeletionPolicy != v1alpha1.DeletionPolicyRetain {
		t.Errorf("defaults = %+v", got)
	}
}

func TestNoKeyReachesALogAnEventOrTheStatus(t *testing.T) {
	h := start(t, grace(time.Second))
	h.claim("a")
	h.ready("a")
	first := h.entry(record1, "pg")
	h.update("a", func(o *v1alpha1.S3Bucket) { o.Spec.Rotate = &v1alpha1.S3RotateSpec{Token: "r1"} })
	h.eventually("the rotation", func() bool { return h.entry(record1, "pg")["accessKey"] != first["accessKey"] })
	h.eventually("the retirement", func() bool { _, ok := h.store.keyOf(first["accessKey"]); return !ok })
	second := h.entry(record1, "pg")

	status, _ := json.Marshal(h.get("a").Status)
	text := logBuf.String() + dumpEvents(t, h.ns) + string(status)
	for name, s := range map[string]string{
		"a secret key": first["secretKey"], "the rotated secret key": second["secretKey"],
		"an access key": first["accessKey"], "the rotated access key": second["accessKey"],
		"the root access key": rootAccess, "the root secret key": rootSecret,
	} {
		if strings.Contains(text, s) {
			t.Errorf("%s reached a log, an event or the status", name)
		}
	}
}
