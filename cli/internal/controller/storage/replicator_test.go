package storage

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"

	v1alpha1 "github.com/this-is-tobi/ohmlab/cli/api/v1alpha1"
	"github.com/this-is-tobi/ohmlab/cli/internal/rustfs"
)

// The store app's own record, where its replicator's key goes.
const replicatorRecord = rootPath + "/s3"

var replicator = replicatorUser("backups")

// replicated gives the harness's store a replicator.
func replicated(r *Reconciler) {
	sc := r.Stores["backups"]
	sc.Replicator = replicatorRecord
	r.Stores["backups"] = sc
}

// app makes a claim the pg claim of another app, with its own record and bucket.
func app(name string) func(*v1alpha1.S3Bucket) {
	return func(o *v1alpha1.S3Bucket) {
		o.Spec.Credentials.Path = "ohmlab/platforms/e2e/core/" + name + "/s3"
		o.Spec.Bucket = "ohmlab-" + name + "-pg"
	}
}

func replicate(o *v1alpha1.S3Bucket) { o.Spec.Replicate = true }

// replicatedBuckets is what the replicator's record lists.
func (h *harness) replicatedBuckets() string {
	return h.entry(replicatorRecord, "replicator")["buckets"]
}

func (h *harness) waitBuckets(want string) {
	h.t.Helper()
	h.eventually("the replicator to list "+want, func() bool {
		e := h.entry(replicatorRecord, "replicator")
		_, listed := e["buckets"]
		return listed && e["buckets"] == want
	})
}

func TestAReplicatorCoversTheReadyReplicatedBuckets(t *testing.T) {
	h := start(t, replicated)
	h.claim("app1", app("app1"), replicate)
	h.claim("app2", app("app2"), replicate)
	h.claim("app3", app("app3"))
	for _, n := range []string{"app1", "app2", "app3"} {
		h.ready(n)
	}
	h.waitBuckets("ohmlab-app1-pg ohmlab-app2-pg")

	want, _ := rustfs.BucketsPolicy([]string{"ohmlab-app1-pg", "ohmlab-app2-pg"}, "readOnly")
	h.eventually("the replicator's policy", func() bool { return string(h.store.policy(replicator)) == string(want) })
	if u, ok := h.store.user(replicator); !ok || !slices.Equal(u.policies, []string{replicator}) {
		t.Errorf("replicator user = %+v (exists %v)", u, ok)
	}
	e := h.entry(replicatorRecord, "replicator")
	if keys := h.keys(replicator); len(keys) != 1 || keys[0] != e["accessKey"] {
		t.Errorf("the store holds %d replicator keys, want the record's only", len(keys))
	}
	for field, v := range map[string]string{"endpoint": "https://rustfs-svc.e2e-store.svc:9000", "region": "us-east-1", "pathStyle": "true", "ca": caOne} {
		if e[field] != v {
			t.Errorf("record %s = %q, want %q", field, e[field], v)
		}
	}
	if _, md, _, _ := h.kv.Get(replicatorRecord); md["ohmlab.fr/owner"] != PathOwner || md["ohmlab.fr/claim.replicator"] != "store:backups" {
		t.Errorf("markers = %v", md)
	}
}

func TestABucketJoinsOnceReady(t *testing.T) {
	h := prepare(t, replicated)
	h.store.failUntilHealed("create-bucket", errFake)
	h.run()
	h.claim("app1", app("app1"), replicate)
	h.reason("app1", v1alpha1.ConditionReady, v1alpha1.ReasonBackendUnavailable)
	h.waitBuckets("")

	h.store.heal("create-bucket")
	h.ready("app1")
	h.waitBuckets("ohmlab-app1-pg")
}

func TestANotReadyBucketStaysReplicated(t *testing.T) {
	h := start(t, replicated)
	h.claim("app1", app("app1"), replicate)
	h.ready("app1")
	h.waitBuckets("ohmlab-app1-pg")

	h.store.failUntilHealed("inspect-bucket", errFake)
	h.reason("app1", v1alpha1.ConditionReady, v1alpha1.ReasonBackendUnavailable)
	time.Sleep(600 * time.Millisecond) // a few replicator resyncs at 150 ms
	if got := h.replicatedBuckets(); got != "ohmlab-app1-pg" {
		t.Errorf("a not-Ready bucket left the replica: buckets %q", got)
	}
	if u, _ := h.store.user(replicator); !slices.Equal(u.policies, []string{replicator}) {
		t.Errorf("replicator policies = %v", u.policies)
	}
}

func TestTheLastReplicatedBucketLeavesAnEmptyListAndNoPolicy(t *testing.T) {
	h := start(t, replicated)
	h.claim("app1", app("app1"), replicate)
	h.claim("app2", app("app2"), replicate)
	h.ready("app1")
	h.ready("app2")
	h.waitBuckets("ohmlab-app1-pg ohmlab-app2-pg")

	h.update("app2", func(o *v1alpha1.S3Bucket) { o.Spec.Replicate = false })
	h.waitBuckets("ohmlab-app1-pg")
	one, _ := rustfs.BucketsPolicy([]string{"ohmlab-app1-pg"}, "readOnly")
	h.eventually("the policy over app1 only", func() bool { return string(h.store.policy(replicator)) == string(one) })

	puts := h.store.writeCount("put-policy")
	h.remove("app1")
	h.waitBuckets("")
	h.eventually("the replicator's policy to be detached", func() bool {
		u, ok := h.store.user(replicator)
		return ok && len(u.policies) == 0
	})
	if got := h.store.writeCount("put-policy"); got != puts {
		t.Errorf("an empty list put a policy: %d puts, then %d", puts, got)
	}
}

func TestAReplicatorResyncWritesNothing(t *testing.T) {
	h := start(t, replicated)
	h.claim("app1", app("app1"), replicate)
	h.ready("app1")
	h.waitBuckets("ohmlab-app1-pg")
	h.eventually("the replicator's key in the store", func() bool { return len(h.keys(replicator)) == 1 })
	time.Sleep(300 * time.Millisecond) // let the first passes settle

	writes := h.store.totalWrites()
	_, dw, mw := h.kv.Counters()
	time.Sleep(1700 * time.Millisecond) // about 10 resyncs at 150 ms
	if got := h.store.totalWrites(); got != writes {
		t.Errorf("a resync wrote to the store: %v", h.store.opsLog()[writes:])
	}
	if _, dw2, mw2 := h.kv.Counters(); dw2 != dw || mw2 != mw {
		t.Errorf("a resync wrote to Vault: data %d->%d, metadata %d->%d", dw, dw2, mw, mw2)
	}
}

func TestObserveOnlyPlansTheReplicatorAndWritesNothing(t *testing.T) {
	h := start(t, observeOnly, replicated)
	h.claim("app1", app("app1"), replicate)
	h.reason("app1", v1alpha1.ConditionReady, v1alpha1.ReasonObserveOnly)
	h.eventually("the replicator's planned changes", func() bool {
		return testutil.ToFloat64(h.metrics.PlannedChanges.WithLabelValues("storage", replicatorKind, "create-user")) == 1 &&
			testutil.ToFloat64(h.metrics.PlannedChanges.WithLabelValues("storage", replicatorKind, "add-service-account")) == 1
	})
	if n := h.store.totalWrites(); n != 0 {
		t.Errorf("observe-only wrote %d times to the store: %v", n, h.store.opsLog())
	}
	if _, dw, mw := h.kv.Counters(); dw != 0 || mw != 0 {
		t.Errorf("observe-only wrote to Vault: data %d, metadata %d", dw, mw)
	}
}

func TestAClaimOnTheReplicatorRecordIsRefused(t *testing.T) {
	h := start(t, replicated)
	h.claim("on-the-record", func(o *v1alpha1.S3Bucket) {
		o.Spec.Credentials.Path, o.Spec.Credentials.Key, o.Spec.Bucket = replicatorRecord, "replicator", "ohmlab-rustfs-replicator"
	})
	h.claim("as-the-user", func(o *v1alpha1.S3Bucket) {
		o.Spec.Credentials.Path, o.Spec.Credentials.Key, o.Spec.Bucket = "ohmlab/platforms/e2e/core/replicator/s3", "backups", replicator
	})
	for _, n := range []string{"on-the-record", "as-the-user"} {
		h.reason(n, v1alpha1.ConditionReady, v1alpha1.ReasonInvalidSpec)
		if c := cond(h.get(n), v1alpha1.ConditionReady); !strings.Contains(c.Message, "replicator") {
			t.Errorf("%s: the refusal does not name the replicator: %q", n, c.Message)
		}
	}
}

func TestAStoreWithoutReplicatorGetsNone(t *testing.T) {
	h := start(t)
	h.claim("app1", app("app1"), replicate)
	h.ready("app1")
	time.Sleep(600 * time.Millisecond)
	if _, ok := h.store.user(replicator); ok {
		t.Error("a store without a replicator got a replicator user")
	}
	if data, _, _, _ := h.kv.Get(replicatorRecord); data != nil {
		t.Error("a store without a replicator got a replicator record")
	}
}

// OhmlabReconcileStale takes the newest success of a target: a replicator's
// would hide an S3Bucket reconciler that stopped succeeding.
func TestTheReplicatorDoesNotMaskTheTargetsStaleness(t *testing.T) {
	h := start(t, replicated)
	h.claim("app1", app("app1"), replicate)
	h.ready("app1")
	h.waitBuckets("ohmlab-app1-pg")
	families, err := h.reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range families {
		if f.GetName() != "ohmlab_reconcile_last_success_timestamp_seconds" {
			continue
		}
		for _, m := range f.GetMetric() {
			for _, l := range m.GetLabel() {
				if l.GetName() == "kind" && l.GetValue() == replicatorKind {
					t.Errorf("the replicator sets %s", f.GetName())
				}
			}
		}
	}
}

func TestNoReplicatorKeyReachesALog(t *testing.T) {
	h := start(t, replicated)
	h.claim("app1", app("app1"), replicate)
	h.ready("app1")
	h.waitBuckets("ohmlab-app1-pg")
	e := h.entry(replicatorRecord, "replicator")
	if e["accessKey"] == "" || e["secretKey"] == "" {
		t.Fatal("no replicator key")
	}
	text := logBuf.String() + dumpEvents(t, h.ns)
	if strings.Contains(text, e["accessKey"]) || strings.Contains(text, e["secretKey"]) {
		t.Error("the replicator's key reached a log or an event")
	}
}
