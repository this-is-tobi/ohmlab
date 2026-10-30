package seed

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/this-is-tobi/ohmlab/utils/internal/secretgen"
)

const owner = "vault-operator-system/keycloak"

// refs is a map-backed Refs: "path#key" -> value.
type refs map[string]string

func (r refs) Lookup(_ context.Context, path, key string) (string, error) {
	if v, ok := r[path+"#"+key]; ok && v != "" {
		return v, nil
	}
	return "", secretgen.ErrRefNotFound
}

type failingRefs struct{}

func (failingRefs) Lookup(context.Context, string, string) (string, error) {
	return "", errors.New("vault is sealed")
}

func compute(t *testing.T, in Input) Plan {
	t.Helper()
	if in.Refs == nil {
		in.Refs = refs{}
	}
	if in.Spec.Owner == "" {
		in.Spec.Owner = owner
	}
	p, err := Compute(context.Background(), in)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	return p
}

// applied returns what Vault holds after the plan was written, the way a KV v2
// mount with compare-and-set would.
func applied(obs Observed, p Plan) Observed {
	out := Observed{Exists: obs.Exists, Version: obs.Version, Data: obs.Data, Metadata: obs.Metadata}
	if p.WriteData {
		out.Data, out.Exists, out.Version = p.Data, true, obs.Version+1
	}
	if p.WriteMetadata {
		out.Metadata = p.Metadata
	}
	return out
}

func actions(p Plan) []string {
	var out []string
	for _, c := range p.Changes {
		out = append(out, string(c.Action)+":"+c.Key)
	}
	sort.Strings(out)
	return out
}

func tree(kv ...any) map[string]any {
	m := map[string]any{}
	for i := 0; i < len(kv); i += 2 {
		m[kv[i].(string)] = kv[i+1]
	}
	return m
}

func TestCreatesAMissingPath(t *testing.T) {
	p := compute(t, Input{Spec: Spec{Data: tree(
		"admin", tree("username", "admin", "password", "<random:24>"),
		"token", "<hex:16>",
	)}})

	if p.Blocked != "" || !p.WriteData || p.CAS != 0 {
		t.Fatalf("plan = %+v", p)
	}
	admin := p.Data["admin"].(map[string]any)
	if admin["username"] != "admin" || len(admin["password"].(string)) != 24 || len(p.Data["token"].(string)) != 16 {
		t.Errorf("data = %v", p.Data)
	}
	want := []string{"add-key:admin.password", "add-key:admin.username", "add-key:token"}
	if !reflect.DeepEqual(actions(p), want) {
		t.Errorf("changes = %v, want %v", actions(p), want)
	}
	if !p.WriteMetadata || p.Metadata[OwnerKey] != owner {
		t.Errorf("a new path must get the owner marker after its data: %+v", p)
	}
	if !reflect.DeepEqual(p.Managed, []string{"admin.password", "admin.username", "token"}) {
		t.Errorf("managed = %v", p.Managed)
	}
}

func TestExistingValuesAreNeverOverwritten(t *testing.T) {
	obs := Observed{Exists: true, Version: 4,
		Data:     tree("admin", tree("password", "hand-set"), "webhook", "https://hooks/x"),
		Metadata: map[string]string{OwnerKey: owner}}
	p := compute(t, Input{
		Spec:     Spec{Data: tree("admin", tree("password", "<random:24>"), "webhook", "")},
		Observed: obs,
	})
	if !p.Empty() || len(p.Changes) != 0 {
		t.Errorf("an existing generated key and a hand-filled placeholder must survive: %+v", p)
	}
}

func TestOnlyTheMissingKeyIsRegenerated(t *testing.T) {
	obs := Observed{Exists: true, Version: 2,
		Data:     tree("a", "keep-a"),
		Metadata: map[string]string{OwnerKey: owner}}
	p := compute(t, Input{Spec: Spec{Data: tree("a", "<random:8>", "b", "<random:8>")}, Observed: obs})
	if !reflect.DeepEqual(actions(p), []string{"add-key:b"}) {
		t.Fatalf("changes = %v", actions(p))
	}
	if p.Data["a"] != "keep-a" || p.CAS != 2 || p.WriteMetadata {
		t.Errorf("plan = %+v", p)
	}
}

func TestPlanIsEmptyOnceApplied(t *testing.T) {
	spec := Spec{Data: tree(
		"a", "<random:8>",
		"n", tree("k", "<uuid>", "pair", tree("secret", "<age:secret>", "public", "<age:public>")),
		"pw", "<random:10>", "hash", "<bcrypt:pw>",
		"list", []any{"x", "<random:4>"},
		"num", 3, "flag", true,
	)}
	first := compute(t, Input{Spec: spec})
	obs := applied(Observed{}, first)
	spec.Managed = first.Managed
	second := compute(t, Input{Spec: spec, Observed: obs})
	if !second.Empty() || len(second.Changes) != 0 {
		t.Fatalf("a second pass over the written state must do nothing: %+v", second)
	}
	if !reflect.DeepEqual(second.Managed, first.Managed) {
		t.Errorf("managed moved: %v vs %v", second.Managed, first.Managed)
	}
}

func TestRefsFollowTheirSource(t *testing.T) {
	src := refs{"homelab/platforms/production/core/keycloak#clients.grafana.secret": "S2"}
	spec := Spec{Data: tree("clientSecret", "<ref:homelab/platforms/production/core/keycloak#clients.grafana.secret>")}
	meta := map[string]string{OwnerKey: owner}

	stale := compute(t, Input{Spec: spec, Refs: src, Observed: Observed{Exists: true, Version: 3, Data: tree("clientSecret", "S1"), Metadata: meta}})
	if !reflect.DeepEqual(actions(stale), []string{"update-field:clientSecret"}) || stale.Data["clientSecret"] != "S2" {
		t.Errorf("a stale copy must follow its source: %+v", stale)
	}
	fresh := compute(t, Input{Spec: spec, Refs: src, Observed: Observed{Exists: true, Version: 3, Data: tree("clientSecret", "S2"), Metadata: meta}})
	if !fresh.Empty() {
		t.Errorf("an up-to-date copy is a no-op: %+v", fresh)
	}
}

func TestAPendingReferenceBlocksTheWholePath(t *testing.T) {
	p := compute(t, Input{Spec: Spec{Data: tree(
		"fine", "<random:8>",
		"copy", "<ref:homelab/platforms/production/core/keycloak#missing>",
	)}})
	if p.Blocked != ReasonRefPending || p.WriteData || p.WriteMetadata {
		t.Errorf("a path is written only when every missing key can be computed: %+v", p)
	}
	if !strings.Contains(p.Message, "copy") {
		t.Errorf("the message must name the key: %q", p.Message)
	}
}

func TestAnUnreadableSourceIsABackendError(t *testing.T) {
	_, err := Compute(context.Background(), Input{
		Spec: Spec{Owner: owner, Data: tree("copy", "<ref:homelab/a/b/c#k>")},
		Refs: failingRefs{},
	})
	if err == nil || !strings.Contains(err.Error(), "vault is sealed") {
		t.Errorf("err = %v", err)
	}
}

func TestAReferenceInsideAGeneratedValueIsMadeOnce(t *testing.T) {
	src := refs{"homelab/a/b/c#host": "db.example"}
	tmpl := "postgres://<ref:homelab/a/b/c#host>/<random:6>"
	first := compute(t, Input{Spec: Spec{Data: tree("dsn", tmpl)}, Refs: src})
	dsn := first.Data["dsn"].(string)
	if !strings.HasPrefix(dsn, "postgres://db.example/") || len(dsn) != len("postgres://db.example/")+6 {
		t.Fatalf("dsn = %q", dsn)
	}
	src["homelab/a/b/c#host"] = "moved.example"
	again := compute(t, Input{Spec: Spec{Data: tree("dsn", tmpl)}, Refs: src,
		Observed: Observed{Exists: true, Version: 1, Data: tree("dsn", dsn), Metadata: map[string]string{OwnerKey: owner}}})
	if !again.Empty() {
		t.Errorf("a value that holds a generated part is seed-once: %+v", again)
	}
}

func TestAdoptionWritesOnlyTheMarker(t *testing.T) {
	obs := Observed{Exists: true, Version: 7, Data: tree("a", "x"), Metadata: nil}
	p := compute(t, Input{Spec: Spec{Data: tree("a", "<random:8>")}, Observed: obs})
	if p.WriteData || !p.WriteMetadata || p.Metadata[OwnerKey] != owner {
		t.Errorf("plan = %+v", p)
	}
	if !reflect.DeepEqual(actions(p), []string{"adopt:"}) {
		t.Errorf("changes = %v", actions(p))
	}
}

func TestAMetadataWriteKeepsTheOtherMarkers(t *testing.T) {
	obs := Observed{Exists: true, Version: 1, Data: tree("a", "x"), Metadata: map[string]string{"team": "platform"}}
	p := compute(t, Input{Spec: Spec{Data: tree("a", "x")}, Observed: obs})
	if p.Metadata["team"] != "platform" || p.Metadata[OwnerKey] != owner {
		t.Errorf("metadata = %v", p.Metadata)
	}
}

func TestAPathOwnedByAnotherCRIsLeftAlone(t *testing.T) {
	obs := Observed{Exists: true, Version: 1, Data: tree(), Metadata: map[string]string{OwnerKey: "other/seed"}}
	p := compute(t, Input{Spec: Spec{Data: tree("a", "<random:8>")}, Observed: obs})
	if p.Blocked != ReasonOwnedByAnother || p.WriteData || p.WriteMetadata {
		t.Errorf("plan = %+v", p)
	}
	if !strings.Contains(p.Message, "other/seed") {
		t.Errorf("message = %q", p.Message)
	}
}

func TestRotationRegeneratesTheListedKeysOnce(t *testing.T) {
	obs := Observed{Exists: true, Version: 5,
		Data:     tree("admin", tree("password", "old-pw", "username", "admin"), "token", "old-token"),
		Metadata: map[string]string{OwnerKey: owner, RotationTokenKey: "2026-01-01"}}
	spec := Spec{
		Data:   tree("admin", tree("password", "<random:24>", "username", "admin"), "token", "<random:8>"),
		Rotate: &Rotate{Token: "2026-10-12", Keys: []string{"admin.password"}},
	}
	p := compute(t, Input{Spec: spec, Observed: obs})
	if !reflect.DeepEqual(actions(p), []string{"rotate:admin.password"}) {
		t.Fatalf("changes = %v", actions(p))
	}
	if p.Data["admin"].(map[string]any)["password"] == "old-pw" || p.Data["token"] != "old-token" {
		t.Errorf("data = %v", p.Data)
	}
	if p.Metadata[RotationTokenKey] != "2026-10-12" || !p.WriteMetadata {
		t.Errorf("the token must be recorded: %+v", p.Metadata)
	}

	// Once recorded, leaving the token set does nothing.
	after := compute(t, Input{Spec: spec, Observed: applied(obs, p)})
	if !after.Empty() {
		t.Errorf("a recorded token must not rotate again: %+v", after)
	}
}

func TestRotationWithoutKeysRegeneratesEveryGeneratedKey(t *testing.T) {
	obs := Observed{Exists: true, Version: 2,
		Data:     tree("a", "old", "b", "old", "lit", "hand", "copy", "S"),
		Metadata: map[string]string{OwnerKey: owner}}
	src := refs{"homelab/a/b/c#k": "S"}
	p := compute(t, Input{
		Spec: Spec{
			Data:   tree("a", "<random:8>", "b", "<hex:8>", "lit", "", "copy", "<ref:homelab/a/b/c#k>"),
			Rotate: &Rotate{Token: "t1"},
		},
		Refs: src, Observed: obs,
	})
	if !reflect.DeepEqual(actions(p), []string{"rotate:a", "rotate:b"}) {
		t.Errorf("only generated keys rotate, not literals or references: %v", actions(p))
	}
}

func TestRotationRejectsAKeyThatIsNotGenerated(t *testing.T) {
	for _, key := range []string{"lit", "copy", "nope"} {
		p := compute(t, Input{
			Spec: Spec{
				Data:   tree("lit", "x", "copy", "<ref:homelab/a/b/c#k>"),
				Rotate: &Rotate{Token: "t", Keys: []string{key}},
			},
			Refs: refs{"homelab/a/b/c#k": "S"},
		})
		if p.Blocked != ReasonInvalidRotation || p.WriteData {
			t.Errorf("key %q: plan = %+v", key, p)
		}
	}
}

func TestRotationOnANewPathRecordsTheToken(t *testing.T) {
	p := compute(t, Input{Spec: Spec{Data: tree("a", "<random:8>"), Rotate: &Rotate{Token: "t"}}})
	if p.Metadata[RotationTokenKey] != "t" || actions(p)[0] != "add-key:a" {
		t.Errorf("plan = %+v", p)
	}
}

func TestAgePair(t *testing.T) {
	spec := Spec{Data: tree("age", tree("secret", "<age:secret>", "public", "<age:public>"))}
	meta := map[string]string{OwnerKey: owner}

	t.Run("fresh pair is consistent", func(t *testing.T) {
		p := compute(t, Input{Spec: spec})
		a := p.Data["age"].(map[string]any)
		pair, err := secretgen.AgePairFromSecret(a["secret"].(string))
		if err != nil || pair.Public != a["public"] {
			t.Errorf("not a pair: %v %v", a, err)
		}
	})

	t.Run("a deleted public key comes back from the secret", func(t *testing.T) {
		pair, _ := secretgen.NewAgePair()
		p := compute(t, Input{Spec: spec, Observed: Observed{Exists: true, Version: 1, Metadata: meta,
			Data: tree("age", tree("secret", pair.Secret))}})
		if !reflect.DeepEqual(actions(p), []string{"add-key:age.public"}) || p.Data["age"].(map[string]any)["public"] != pair.Public {
			t.Errorf("plan = %+v", p)
		}
	})

	t.Run("a deleted secret key makes a new pair and rewrites the public key", func(t *testing.T) {
		old, _ := secretgen.NewAgePair()
		p := compute(t, Input{Spec: spec, Observed: Observed{Exists: true, Version: 1, Metadata: meta,
			Data: tree("age", tree("public", old.Public))}})
		a := p.Data["age"].(map[string]any)
		pair, err := secretgen.AgePairFromSecret(a["secret"].(string))
		if err != nil || pair.Public != a["public"] || a["public"] == old.Public {
			t.Errorf("the pair must be renewed together: %v %v", a, err)
		}
	})

	t.Run("rotating one half rotates the pair", func(t *testing.T) {
		old, _ := secretgen.NewAgePair()
		p := compute(t, Input{
			Spec: Spec{Data: spec.Data, Rotate: &Rotate{Token: "t", Keys: []string{"age.secret"}}},
			Observed: Observed{Exists: true, Version: 1, Metadata: meta,
				Data: tree("age", tree("secret", old.Secret, "public", old.Public))}})
		a := p.Data["age"].(map[string]any)
		pair, err := secretgen.AgePairFromSecret(a["secret"].(string))
		if err != nil || pair.Public != a["public"] || a["secret"] == old.Secret {
			t.Errorf("rotated pair: %v %v", a, err)
		}
	})

	t.Run("a rotation that names no key rotates the pair", func(t *testing.T) {
		old, _ := secretgen.NewAgePair()
		p := compute(t, Input{
			Spec: Spec{Data: spec.Data, Rotate: &Rotate{Token: "t"}},
			Observed: Observed{Exists: true, Version: 1, Metadata: meta,
				Data: tree("age", tree("secret", old.Secret, "public", old.Public))}})
		a := p.Data["age"].(map[string]any)
		pair, err := secretgen.AgePairFromSecret(a["secret"].(string))
		if err != nil || pair.Public != a["public"] || a["secret"] == old.Secret || a["public"] == old.Public {
			t.Errorf("rotated pair: %v %v", a, err)
		}
	})

	t.Run("an intact pair is left alone", func(t *testing.T) {
		pair, _ := secretgen.NewAgePair()
		p := compute(t, Input{Spec: spec, Observed: Observed{Exists: true, Version: 1, Metadata: meta,
			Data: tree("age", tree("secret", pair.Secret, "public", pair.Public))}})
		if !p.Empty() {
			t.Errorf("plan = %+v", p)
		}
	})
}

func TestBcrypt(t *testing.T) {
	meta := map[string]string{OwnerKey: owner}
	spec := Spec{Data: tree("admin", tree("password", "<random:12>", "passwordHashed", "<bcrypt:admin.password>"))}

	t.Run("hashes the generated password", func(t *testing.T) {
		p := compute(t, Input{Spec: spec})
		a := p.Data["admin"].(map[string]any)
		if !secretgen.BcryptMatches(a["passwordHashed"].(string), a["password"].(string)) {
			t.Errorf("the hash does not match the password: %v", a)
		}
	})

	t.Run("a matching hash is kept, a new salt is not written", func(t *testing.T) {
		h, _ := secretgen.Bcrypt("pw")
		p := compute(t, Input{Spec: spec, Observed: Observed{Exists: true, Version: 1, Metadata: meta,
			Data: tree("admin", tree("password", "pw", "passwordHashed", h))}})
		if !p.Empty() {
			t.Errorf("plan = %+v", p)
		}
	})

	t.Run("a hash of an older password follows the password", func(t *testing.T) {
		h, _ := secretgen.Bcrypt("old")
		p := compute(t, Input{Spec: spec, Observed: Observed{Exists: true, Version: 1, Metadata: meta,
			Data: tree("admin", tree("password", "new", "passwordHashed", h))}})
		if !reflect.DeepEqual(actions(p), []string{"update-field:admin.passwordHashed"}) {
			t.Fatalf("changes = %v", actions(p))
		}
		if !secretgen.BcryptMatches(p.Data["admin"].(map[string]any)["passwordHashed"].(string), "new") {
			t.Error("the new hash does not match")
		}
	})

	t.Run("a hash of a missing field is a spec error", func(t *testing.T) {
		p := compute(t, Input{Spec: Spec{Data: tree("h", "<bcrypt:nope.nothing>")}})
		if p.Blocked != ReasonInvalidSpec || p.WriteData {
			t.Errorf("plan = %+v", p)
		}
	})
}

func TestPrune(t *testing.T) {
	meta := map[string]string{OwnerKey: owner}
	obs := Observed{Exists: true, Version: 3, Metadata: meta, Data: tree(
		"keep", "k", "gone", "g", "hand", "h",
		"nested", tree("only", "o"),
	)}
	declared := tree("keep", "<random:4>")

	t.Run("removes only what the controller wrote", func(t *testing.T) {
		p := compute(t, Input{Spec: Spec{Data: declared, Prune: true, Managed: []string{"gone", "keep", "nested.only"}}, Observed: obs})
		if !reflect.DeepEqual(actions(p), []string{"prune:gone", "prune:nested.only"}) {
			t.Fatalf("changes = %v", actions(p))
		}
		if _, ok := p.Data["hand"]; !ok {
			t.Error("a key nobody tracked must stay")
		}
		if _, ok := p.Data["gone"]; ok {
			t.Error("a top-level key the controller wrote and the CR dropped must go")
		}
		if _, ok := p.Data["nested"]; ok {
			t.Error("a map emptied by pruning must go too")
		}
		if !reflect.DeepEqual(p.Managed, []string{"keep"}) {
			t.Errorf("managed = %v", p.Managed)
		}
	})
	t.Run("a lost status prunes nothing", func(t *testing.T) {
		p := compute(t, Input{Spec: Spec{Data: declared, Prune: true}, Observed: obs})
		if !p.Empty() {
			t.Errorf("plan = %+v", p)
		}
	})
	t.Run("off by default", func(t *testing.T) {
		p := compute(t, Input{Spec: Spec{Data: declared, Managed: []string{"gone"}}, Observed: obs})
		if !p.Empty() {
			t.Errorf("plan = %+v", p)
		}
	})
	t.Run("a tracked key that vanished is forgotten", func(t *testing.T) {
		p := compute(t, Input{Spec: Spec{Data: declared, Prune: true, Managed: []string{"ghost"}}, Observed: obs})
		if len(p.Managed) != 0 || !p.Empty() {
			t.Errorf("plan = %+v", p)
		}
	})
}

func TestShapesThatDoNotMatchKeepTheExistingValue(t *testing.T) {
	obs := Observed{Exists: true, Version: 1, Metadata: map[string]string{OwnerKey: owner},
		Data: tree("a", "scalar", "m", tree("k", "v"))}
	p := compute(t, Input{Spec: Spec{Data: tree("a", tree("x", "<random:4>"), "m", "now-a-string")}, Observed: obs})
	if !p.Empty() {
		t.Errorf("an existing value wins whatever its shape: %+v", p)
	}
}

func TestNonStringValuesAreAddedWhenAbsentAndKeptWhenPresent(t *testing.T) {
	spec := Spec{Data: tree("n", 3, "b", true, "l", []any{"a", "<random:4>"})}
	p := compute(t, Input{Spec: spec})
	if p.Data["n"] != 3 || p.Data["b"] != true {
		t.Errorf("data = %v", p.Data)
	}
	l := p.Data["l"].([]any)
	if l[0] != "a" || len(l[1].(string)) != 4 {
		t.Errorf("list = %v", l)
	}
	again := compute(t, Input{Spec: spec, Observed: applied(Observed{}, p)})
	if !again.Empty() {
		t.Errorf("plan = %+v", again)
	}
}

func TestADeletedPathIsWrittenAgainAgainstItsVersion(t *testing.T) {
	// Soft-deleted: no data, but the version number counts for compare-and-set.
	p := compute(t, Input{Spec: Spec{Data: tree("a", "<random:8>")}, Observed: Observed{Exists: false, Version: 2,
		Metadata: map[string]string{OwnerKey: owner}}})
	if !p.WriteData || p.CAS != 2 {
		t.Errorf("plan = %+v", p)
	}
}

func TestMalformedTemplatesAreSpecErrors(t *testing.T) {
	for _, tmpl := range []string{"<hex:7>", "<random:0>"} {
		p := compute(t, Input{Spec: Spec{Data: tree("k", tmpl)}})
		if p.Blocked != ReasonInvalidSpec || p.WriteData {
			t.Errorf("%q: plan = %+v", tmpl, p)
		}
	}
}

func TestNothingToWriteForAnEmptySpecOnAnAbsentPath(t *testing.T) {
	p := compute(t, Input{Spec: Spec{Data: tree()}})
	if !p.Empty() {
		t.Errorf("a metadata write on a path with no data would be denied: %+v", p)
	}
}

func TestSecretsNeverAppearInMessagesOrChangeKeys(t *testing.T) {
	src := refs{"homelab/a/b/c#k": "TOP-SECRET-VALUE"}
	obs := Observed{Exists: true, Version: 1, Metadata: map[string]string{OwnerKey: owner}, Data: tree("copy", "TOP-SECRET-OLD")}
	p := compute(t, Input{Spec: Spec{Data: tree("copy", "<ref:homelab/a/b/c#k>")}, Refs: src, Observed: obs})
	for _, c := range p.Changes {
		if strings.Contains(c.Key, "SECRET") {
			t.Errorf("change key leaks a value: %q", c.Key)
		}
	}
	blocked := compute(t, Input{Spec: Spec{Data: tree("copy", "<ref:homelab/a/b/c#absent>")}, Refs: src})
	if strings.Contains(blocked.Message, "TOP-SECRET") {
		t.Errorf("message leaks a value: %q", blocked.Message)
	}
}

func TestACopyIsLeftAloneWhenItsSourceDisappears(t *testing.T) {
	obs := Observed{Exists: true, Version: 3, Metadata: map[string]string{OwnerKey: owner}, Data: tree("copy", "S1", "own", "kept")}
	p := compute(t, Input{
		Spec:     Spec{Data: tree("copy", "<ref:homelab/a/b/c#k>", "own", "<random:8>")},
		Refs:     refs{}, // the source was deleted
		Observed: obs,
	})
	if p.Blocked != ReasonRefPending || p.WriteData || p.WriteMetadata {
		t.Errorf("a source that is gone blocks the path and changes nothing: %+v", p)
	}
}

func TestANewLengthDoesNotRegenerateAnExistingKey(t *testing.T) {
	obs := Observed{Exists: true, Version: 1, Metadata: map[string]string{OwnerKey: owner}, Data: tree("pw", "short")}
	p := compute(t, Input{Spec: Spec{Data: tree("pw", "<random:64>")}, Observed: obs})
	if !p.Empty() {
		t.Errorf("seed-once: a longer template is a rotation, not a rewrite: %+v", p)
	}
}

func TestValuesOfEveryShapeSurviveAReadAndAWriteBack(t *testing.T) {
	obs := Observed{Exists: true, Version: 1, Metadata: map[string]string{OwnerKey: owner}, Data: tree(
		"nothing", nil, "empty", tree(), "list", []any{}, "n", json.Number("12345678901234567890"), "b", false,
		"nested", tree("deeper", tree("x", nil)),
	)}
	p := compute(t, Input{Spec: Spec{Data: tree("added", "<random:6>", "nothing", "x", "nested", tree("deeper", tree("y", "<random:4>")))}, Observed: obs})
	if !p.WriteData {
		t.Fatalf("plan = %+v", p)
	}
	for _, k := range []string{"nothing", "empty", "list", "n", "b"} {
		if !reflect.DeepEqual(p.Data[k], obs.Data[k]) {
			t.Errorf("%s changed: %#v -> %#v", k, obs.Data[k], p.Data[k])
		}
	}
	deeper := p.Data["nested"].(map[string]any)["deeper"].(map[string]any)
	if _, ok := deeper["x"]; !ok || deeper["y"] == nil {
		t.Errorf("nested = %v", p.Data["nested"])
	}
}
