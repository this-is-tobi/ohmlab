package seed

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/this-is-tobi/ohmlab/utils/internal/seed/seedtest"
	"github.com/this-is-tobi/ohmlab/utils/internal/vault"
)

const (
	pathA = "homelab/platforms/production/core/a"
	pathB = "homelab/platforms/production/core/b"
)

func run(t *testing.T, kv *seedtest.MemKV, path string, spec Spec, opts Options) Result {
	t.Helper()
	if spec.Owner == "" {
		spec.Owner = owner
	}
	res, err := Run(context.Background(), kv, path, spec, opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return res
}

func TestRunCreatesThenSettles(t *testing.T) {
	kv := seedtest.New()
	spec := Spec{Data: tree("pw", "<random:12>", "lit", "")}

	first := run(t, kv, pathA, spec, Options{})
	if !first.DataWritten || !first.MetadataWritten || first.Version != 1 {
		t.Fatalf("first = %+v", first)
	}
	data, meta, version, exists := kv.Get(pathA)
	if !exists || version != 1 || len(data["pw"].(string)) != 12 || meta[OwnerKey] != owner {
		t.Errorf("stored = %v %v %d", data, meta, version)
	}

	spec.Managed = first.Plan.Managed
	second := run(t, kv, pathA, spec, Options{})
	if second.DataWritten || second.MetadataWritten || !second.Plan.Empty() {
		t.Errorf("a settled path must not be written again: %+v", second)
	}
	if _, dw, mw := kv.Counters(); dw != 1 || mw != 1 {
		t.Errorf("writes = data %d, metadata %d; want 1 and 1", dw, mw)
	}
}

func TestRunAdoptsALegacyPathWithoutANewVersion(t *testing.T) {
	kv := seedtest.New()
	kv.Put(pathA, tree("pw", "from-the-job"), nil)

	res := run(t, kv, pathA, Spec{Data: tree("pw", "<random:12>")}, Options{})
	if res.DataWritten || !res.MetadataWritten {
		t.Fatalf("res = %+v", res)
	}
	data, meta, version, _ := kv.Get(pathA)
	if data["pw"] != "from-the-job" || version != 1 || meta[OwnerKey] != owner {
		t.Errorf("stored = %v %v %d", data, meta, version)
	}
}

func TestRunKeepsAnEditMadeBetweenTheReadAndTheWrite(t *testing.T) {
	kv := seedtest.New()
	kv.Put(pathA, tree("kept", "x"), map[string]string{OwnerKey: owner})
	kv.BeforeWrite = func() { kv.Put(pathA, tree("kept", "x", "hand", "added-by-a-human"), nil) }

	res := run(t, kv, pathA, Spec{Data: tree("kept", "<random:4>", "new", "<random:8>")}, Options{})
	if !res.DataWritten || res.Version != 3 {
		t.Fatalf("res = %+v", res)
	}
	data, _, _, _ := kv.Get(pathA)
	if data["hand"] != "added-by-a-human" || len(data["new"].(string)) != 8 {
		t.Errorf("the concurrent edit was lost: %v", data)
	}
}

func TestRunGivesUpAfterTooManyLostRaces(t *testing.T) {
	kv := seedtest.New()
	kv.Put(pathA, tree("a", "1"), map[string]string{OwnerKey: owner})
	racing := &racingKV{MemKV: kv, path: pathA}
	_, err := Run(context.Background(), racing, pathA, Spec{Owner: owner, Data: tree("b", "<random:4>")}, Options{Attempts: 2})
	if !errors.Is(err, vault.ErrCASMismatch) {
		t.Errorf("err = %v", err)
	}
	if racing.attempts != 2 {
		t.Errorf("Attempts: 2 means two tries, got %d", racing.attempts)
	}
}

// racingKV changes the path before every write, so every compare-and-set loses.
type racingKV struct {
	*seedtest.MemKV
	path     string
	attempts int
}

func (r *racingKV) KVWriteCAS(ctx context.Context, mount, path string, data map[string]any, cas int64) (int64, error) {
	r.attempts++
	r.MemKV.Put(r.path, tree("a", "1"), nil)
	return r.MemKV.KVWriteCAS(ctx, mount, path, data, cas)
}

func TestRunObserveOnlyWritesNothing(t *testing.T) {
	kv := seedtest.New()
	res := run(t, kv, pathA, Spec{Data: tree("pw", "<random:12>")}, Options{ObserveOnly: true})
	if res.DataWritten || res.MetadataWritten || res.Plan.Empty() {
		t.Errorf("res = %+v", res)
	}
	if _, dw, mw := kv.Counters(); dw != 0 || mw != 0 {
		t.Errorf("writes = %d %d", dw, mw)
	}
}

func TestRunWaitsForAReferenceThenFollowsIt(t *testing.T) {
	kv := seedtest.New()
	spec := Spec{Data: tree("secret", "<ref:"+pathB+"#clients.x.secret>")}

	pending := run(t, kv, pathA, spec, Options{})
	if pending.Plan.Blocked != ReasonRefPending || pending.DataWritten {
		t.Fatalf("pending = %+v", pending)
	}
	if _, _, _, exists := kv.Get(pathA); exists {
		t.Fatal("nothing may be written while the reference is pending")
	}

	kv.Put(pathB, tree("clients", tree("x", tree("secret", "S1"))), nil)
	first := run(t, kv, pathA, spec, Options{})
	if !first.DataWritten {
		t.Fatalf("first = %+v", first)
	}
	if data, _, _, _ := kv.Get(pathA); data["secret"] != "S1" {
		t.Errorf("data = %v", data)
	}

	kv.Put(pathB, tree("clients", tree("x", tree("secret", "S2"))), nil)
	spec.Managed = first.Plan.Managed
	follow := run(t, kv, pathA, spec, Options{})
	if data, _, _, _ := kv.Get(pathA); !follow.DataWritten || data["secret"] != "S2" {
		t.Errorf("the copy must follow its source: %v %+v", data, follow)
	}
	if !reflect.DeepEqual(follow.Plan.Changes, []Change{{Action: ActionUpdateField, Key: "secret"}}) {
		t.Errorf("changes = %v", follow.Plan.Changes)
	}
}

func TestRunRecreatesADeletedPath(t *testing.T) {
	kv := seedtest.New()
	kv.Put(pathA, tree("a", "1"), map[string]string{OwnerKey: owner})
	kv.Delete(pathA)

	res := run(t, kv, pathA, Spec{Data: tree("a", "<random:6>")}, Options{})
	if !res.DataWritten || res.Version != 2 {
		t.Errorf("res = %+v", res)
	}
	if _, _, v, exists := kv.Get(pathA); !exists || v != 2 {
		t.Errorf("version %d exists %v", v, exists)
	}
}

func TestRunReturnsBackendFailures(t *testing.T) {
	kv := seedtest.New()
	kv.FailReads(pathA, errors.New("vault is sealed"))
	if _, err := Run(context.Background(), kv, pathA, Spec{Owner: owner, Data: tree("a", "x")}, Options{}); err == nil {
		t.Error("want the read error")
	}
}

func TestRunRejectsABadPath(t *testing.T) {
	for _, p := range []string{"", "homelab", "homelab/", "/a/b", "homelab/a//b", "homelab/../x", "homelab/a/"} {
		if _, err := Run(context.Background(), seedtest.New(), p, Spec{Owner: owner, Data: tree("a", "x")}, Options{}); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("path %q: err = %v", p, err)
		}
	}
}

func TestRunLeavesAPathOfAnotherOwnerAlone(t *testing.T) {
	kv := seedtest.New()
	kv.Put(pathA, tree("a", "1"), map[string]string{OwnerKey: "other/seed"})
	res := run(t, kv, pathA, Spec{Data: tree("b", "<random:4>")}, Options{})
	if res.Plan.Blocked != ReasonOwnedByAnother || res.DataWritten || res.MetadataWritten {
		t.Errorf("res = %+v", res)
	}
}
