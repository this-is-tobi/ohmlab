package storage

import (
	"errors"
	"reflect"
	"regexp"
	"slices"
	"testing"
	"time"

	"github.com/this-is-tobi/ohmlab/cli/internal/vault"
)

var (
	t0         = time.Date(2026, 11, 12, 20, 0, 0, 0, time.UTC)
	testGrace  = 26 * time.Hour
	testFields = map[string]string{
		"endpoint": "https://rustfs-svc.e2e-store.svc:9000", "bucket": bucket1, "region": "us-east-1", "pathStyle": "true", "ca": caOne,
	}
)

func pgClaim() claim {
	return claim{owner: "ohmlab-system/ohmlab-app1-pg", key: "pg", fields: testFields}
}

// noKeys fails a plan that would generate a key it must not.
func noKeys() (string, string, error) { return "", "", errors.New("no key may be generated here") }

// written is the record a plan leaves once it is written.
func written(p recordPlan, before *vault.KVSecret) *vault.KVSecret {
	out := &vault.KVSecret{Exists: true, Version: before.Version, Data: before.Data, Metadata: before.Metadata}
	if p.data != nil {
		out.Data, out.Version = p.data, before.Version+1
	}
	if p.metadata != nil {
		out.Metadata = p.metadata
	}
	return out
}

func actions(p recordPlan) []string {
	var out []string
	for _, c := range p.changes {
		out = append(out, c.action)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func TestPlanGeneratesKeysOnce(t *testing.T) {
	fresh := &vault.KVSecret{}
	p, err := planRecord(fresh, pgClaim(), t0, testGrace, newKeyPair)
	if err != nil || p.blocked != "" {
		t.Fatalf("plan: %v %q", err, p.blocked)
	}
	e, _ := p.data["pg"].(map[string]any)
	if !regexp.MustCompile(`^[A-Z0-9]{20}$`).MatchString(e["accessKey"].(string)) ||
		!regexp.MustCompile(`^[A-Za-z0-9]{40}$`).MatchString(e["secretKey"].(string)) {
		t.Errorf("keys are not 20 [A-Z0-9] and 40 [A-Za-z0-9] characters")
	}
	for k, v := range testFields {
		if e[k] != v {
			t.Errorf("field %s = %q, want %q", k, e[k], v)
		}
	}
	if p.accessKey != e["accessKey"] || p.secretKey != e["secretKey"] {
		t.Error("the plan's key is not the one written")
	}
	if p.metadata["ohmlab.fr/owner"] != PathOwner || p.metadata["ohmlab.fr/claim.pg"] != "ohmlab-system/ohmlab-app1-pg" {
		t.Errorf("markers = %v", p.metadata)
	}
	if got := actions(p); !slices.Equal(got, []string{"add-key", "update-field"}) {
		t.Errorf("actions = %v", got)
	}

	again, err := planRecord(written(p, fresh), pgClaim(), t0, testGrace, noKeys)
	if err != nil || again.data != nil || again.metadata != nil || len(again.changes) != 0 {
		t.Errorf("replanning the written record: err=%v data=%v metadata=%v changes=%v", err, again.data != nil, again.metadata != nil, again.changes)
	}
	if again.accessKey != p.accessKey {
		t.Error("replanning changed the key")
	}
}

func TestPlanKeepsOtherClaims(t *testing.T) {
	lfs := map[string]any{"accessKey": "LFSACCESSKEY00000000", "secretKey": "s", "bucket": "ohmlab-app1-lfs"}
	sec := &vault.KVSecret{Exists: true, Version: 3,
		Data:     map[string]any{"lfs": lfs},
		Metadata: map[string]string{"ohmlab.fr/owner": PathOwner, "ohmlab.fr/claim.lfs": "ohmlab-system/ohmlab-app1-lfs", "ohmlab.fr/rotation-token.lfs": "t"},
	}
	p, err := planRecord(sec, pgClaim(), t0, testGrace, newKeyPair)
	if err != nil || p.blocked != "" {
		t.Fatalf("plan: %v %q", err, p.blocked)
	}
	if !reflect.DeepEqual(p.data["lfs"], lfs) {
		t.Errorf("lfs = %v, want it untouched", p.data["lfs"])
	}
	for _, k := range []string{"ohmlab.fr/claim.lfs", "ohmlab.fr/rotation-token.lfs"} {
		if p.metadata[k] != sec.Metadata[k] {
			t.Errorf("%s = %q, want %q", k, p.metadata[k], sec.Metadata[k])
		}
	}
	if p.data["pg"] == nil || p.metadata["ohmlab.fr/claim.pg"] == "" {
		t.Error("pg was not planned")
	}
}

func TestPlanRefusesAnotherOwner(t *testing.T) {
	entry := map[string]any{"accessKey": "AAAAAAAAAAAAAAAAAAAA", "secretKey": "s"}
	tests := []struct {
		name string
		meta map[string]string
	}{
		{"the path is a VaultSeed's", map[string]string{"ohmlab.fr/owner": "vault-operator-system/app1"}},
		{"the key is another object's", map[string]string{"ohmlab.fr/owner": PathOwner, "ohmlab.fr/claim.pg": "ohmlab-system/other"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sec := &vault.KVSecret{Exists: true, Version: 1, Data: map[string]any{"pg": entry}, Metadata: tt.meta}
			p, err := planRecord(sec, pgClaim(), t0, testGrace, noKeys)
			if err != nil || p.blocked == "" || p.data != nil || p.metadata != nil {
				t.Errorf("err=%v blocked=%q data=%v metadata=%v: want blocked with nothing to write", err, p.blocked, p.data != nil, p.metadata != nil)
			}
		})
	}
}

func TestPlanAdoptsAnEntryWithKeysAndNoMarker(t *testing.T) {
	entry := map[string]any{"accessKey": "HANDMADEACCESSKEY000", "secretKey": "hand-made-secret"}
	for k, v := range testFields {
		entry[k] = v
	}
	sec := &vault.KVSecret{Exists: true, Version: 1, Data: map[string]any{"pg": entry}}
	p, err := planRecord(sec, pgClaim(), t0, testGrace, noKeys)
	if err != nil || p.blocked != "" {
		t.Fatalf("plan: %v %q", err, p.blocked)
	}
	if p.data != nil || p.accessKey != "HANDMADEACCESSKEY000" {
		t.Errorf("adoption rewrote the data or the key: data=%v key=%v", p.data != nil, p.accessKey)
	}
	if p.metadata["ohmlab.fr/claim.pg"] != "ohmlab-system/ohmlab-app1-pg" || p.metadata["ohmlab.fr/owner"] != PathOwner {
		t.Errorf("markers = %v", p.metadata)
	}
	if got := actions(p); !slices.Equal(got, []string{"adopt"}) {
		t.Errorf("actions = %v", got)
	}
}

func TestRotationKeepsTheOldKeyUntilTheDeadline(t *testing.T) {
	first, err := planRecord(&vault.KVSecret{}, pgClaim(), t0, testGrace, newKeyPair)
	if err != nil {
		t.Fatal(err)
	}
	settled := written(first, &vault.KVSecret{})
	old := first.accessKey

	c := pgClaim()
	c.token = "2026-11-12"
	rot, err := planRecord(settled, c, t0, testGrace, newKeyPair)
	if err != nil {
		t.Fatal(err)
	}
	if rot.accessKey == old || rot.accessKey == "" || rot.retiring != old {
		t.Fatalf("rotation: new key %v, retiring %v", rot.accessKey != old, rot.retiring == old)
	}
	deadline := t0.Add(testGrace).Format(time.RFC3339)
	if rot.metadata["ohmlab.fr/rotation-token.pg"] != "2026-11-12" || rot.metadata["ohmlab.fr/retiring.pg"] != old+" "+deadline {
		t.Errorf("markers = %v", rot.metadata)
	}
	if got := actions(rot); !slices.Contains(got, "rotate") {
		t.Errorf("actions = %v", got)
	}

	rotated := written(rot, settled)
	before, err := planRecord(rotated, c, t0.Add(testGrace-time.Second), testGrace, noKeys)
	if err != nil || before.retiring != old || before.metadata != nil || len(before.changes) != 0 {
		t.Errorf("before the deadline: err=%v retiring kept=%v changes=%v", err, before.retiring == old, before.changes)
	}
	after, err := planRecord(rotated, c, t0.Add(testGrace+time.Second), testGrace, noKeys)
	if err != nil || after.retiring != "" || after.accessKey != rot.accessKey {
		t.Fatalf("after the deadline: err=%v retiring=%v", err, after.retiring != "")
	}
	if _, ok := after.metadata["ohmlab.fr/retiring.pg"]; ok || after.metadata == nil {
		t.Errorf("the retiring marker must go: %v", after.metadata)
	}
	if got := actions(after); !slices.Equal(got, []string{"retire-key"}) {
		t.Errorf("actions = %v", got)
	}
}
