package seed

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/this-is-tobi/ohmlab/utils/internal/secretgen"
	"github.com/this-is-tobi/ohmlab/utils/internal/vault"
)

func TestVaultCreatesThenSettlesUnderTheControllerPolicy(t *testing.T) {
	d := devVaultOrSkip(t)
	mount, c := d.mount()
	src, dst := mount+"/platforms/production/core/keycloak", mount+"/platforms/production/core/grafana"

	// The source is created first by its own CR; the copy waits for it.
	spec := func(data map[string]any) Spec { return Spec{Owner: owner, Data: data} }
	srcSpec := spec(tree("clients", tree("grafana", tree("secret", "<random:32>"))))
	dstSpec := spec(tree("oidc", tree("clientSecret", "<ref:"+src+"#clients.grafana.secret>"), "admin", tree("password", "<random:16>", "hashed", "<bcrypt:admin.password>")))

	res, err := Run(background(), c, dst, dstSpec, Options{})
	if err != nil || res.Plan.Blocked != ReasonRefPending {
		t.Fatalf("before the source exists: %+v %v", res.Plan, err)
	}
	if _, err := Run(background(), c, src, srcSpec, Options{}); err != nil {
		t.Fatal(err)
	}
	if res, err = Run(background(), c, dst, dstSpec, Options{}); err != nil || !res.DataWritten {
		t.Fatalf("after: %+v %v", res, err)
	}

	srcData, _, _ := d.rootRead(mount, "platforms/production/core/keycloak")
	dstData, v1, custom := d.rootRead(mount, "platforms/production/core/grafana")
	if got := dstData["oidc"].(map[string]any)["clientSecret"]; got != srcData["clients"].(map[string]any)["grafana"].(map[string]any)["secret"] {
		t.Errorf("the copy differs from its source")
	}
	admin := dstData["admin"].(map[string]any)
	if !secretgen.BcryptMatches(admin["hashed"].(string), admin["password"].(string)) {
		t.Error("the stored hash does not match the stored password")
	}
	if custom[OwnerKey] != owner || v1 != 1 {
		t.Errorf("owner=%q version=%d", custom[OwnerKey], v1)
	}

	// A resync that finds nothing to do writes nothing at all.
	dstSpec.Managed = res.Plan.Managed
	again, err := Run(background(), c, dst, dstSpec, Options{})
	if err != nil || !again.Plan.Empty() {
		t.Fatalf("again: %+v %v", again.Plan, err)
	}
	if _, v2, _ := d.rootRead(mount, "platforms/production/core/grafana"); v2 != v1 {
		t.Errorf("a no-op resync moved the KV version %d -> %d", v1, v2)
	}
}

func TestVaultAdoptionAddsMarkersWithoutAVersion(t *testing.T) {
	d := devVaultOrSkip(t)
	mount, c := d.mount()
	d.rootPut(mount, "platforms/production/core/legacy", tree("pw", "set-by-the-job"))

	res, err := Run(background(), c, mount+"/platforms/production/core/legacy", Spec{Owner: owner, Data: tree("pw", "<random:8>", "extra", "<random:8>")}, Options{})
	if err != nil || !res.DataWritten {
		t.Fatalf("%+v %v", res, err)
	}
	data, version, custom := d.rootRead(mount, "platforms/production/core/legacy")
	if data["pw"] != "set-by-the-job" || len(data["extra"].(string)) != 8 || version != 2 || custom[OwnerKey] != owner {
		t.Errorf("data=%v version=%d custom=%v", data, version, custom)
	}

	// Adoption alone (nothing missing): only the marker, no new version.
	d.rootPut(mount, "platforms/production/core/legacy2", tree("pw", "x"))
	res, err = Run(background(), c, mount+"/platforms/production/core/legacy2", Spec{Owner: owner, Data: tree("pw", "<random:8>")}, Options{})
	if err != nil || res.DataWritten || !res.MetadataWritten {
		t.Fatalf("%+v %v", res, err)
	}
	if _, version, custom = d.rootRead(mount, "platforms/production/core/legacy2"); version != 1 || custom[OwnerKey] != owner {
		t.Errorf("version=%d custom=%v", version, custom)
	}
}

func TestVaultKeepsAnEditMadeBetweenTheReadAndTheWrite(t *testing.T) {
	d := devVaultOrSkip(t)
	mount, c := d.mount()
	path := "platforms/production/core/race"
	d.rootPut(mount, path, tree("a", "1"))

	racing := &hookKV{KV: c, before: func() { d.rootPut(mount, path, tree("a", "1", "hand", "typed-by-a-human")) }}
	res, err := Run(background(), racing, mount+"/"+path, Spec{Owner: owner, Data: tree("a", "<random:4>", "new", "<random:8>")}, Options{})
	if err != nil || !res.DataWritten {
		t.Fatalf("%+v %v", res, err)
	}
	data, version, _ := d.rootRead(mount, path)
	if data["hand"] != "typed-by-a-human" || len(data["new"].(string)) != 8 || version != 3 {
		t.Errorf("data=%v version=%d", data, version)
	}
}

// hookKV runs a function once before the first data write.
type hookKV struct {
	KV
	before func()
}

func (h *hookKV) KVWriteCAS(ctx context.Context, mount, path string, data map[string]any, cas int64) (int64, error) {
	if h.before != nil {
		f := h.before
		h.before = nil
		f()
	}
	return h.KV.KVWriteCAS(ctx, mount, path, data, cas)
}

func TestVaultRecreatesASoftDeletedPath(t *testing.T) {
	d := devVaultOrSkip(t)
	mount, c := d.mount()
	path := "platforms/production/core/gone"
	d.rootPut(mount, path, tree("a", "1"))
	d.must("DELETE", mount+"/data/"+path, nil)

	res, err := Run(background(), c, mount+"/"+path, Spec{Owner: owner, Data: tree("a", "<random:6>")}, Options{})
	if err != nil || !res.DataWritten || res.Version != 2 {
		t.Fatalf("%+v %v", res, err)
	}
	if data, _, custom := d.rootRead(mount, path); len(data["a"].(string)) != 6 || custom[OwnerKey] != owner {
		t.Errorf("data=%v custom=%v", data, custom)
	}
}

func TestVaultRotatesOnce(t *testing.T) {
	d := devVaultOrSkip(t)
	mount, c := d.mount()
	full, path := mount+"/platforms/production/core/rot", "platforms/production/core/rot"
	spec := Spec{Owner: owner, Data: tree("pw", "<random:12>", "keep", "<random:12>"), Rotate: &Rotate{Token: "t1", Keys: []string{"pw"}}}

	first, err := Run(background(), c, full, spec, Options{})
	if err != nil {
		t.Fatal(err)
	}
	spec.Managed = first.Plan.Managed
	before, _, _ := d.rootRead(mount, path)

	spec.Rotate = &Rotate{Token: "t2", Keys: []string{"pw"}}
	rot, err := Run(background(), c, full, spec, Options{})
	if err != nil || !rot.DataWritten {
		t.Fatalf("%+v %v", rot, err)
	}
	after, _, custom := d.rootRead(mount, path)
	if after["pw"] == before["pw"] || after["keep"] != before["keep"] || custom[RotationTokenKey] != "t2" {
		t.Errorf("before=%v after=%v custom=%v", before, after, custom)
	}
	again, err := Run(background(), c, full, spec, Options{})
	if err != nil || !again.Plan.Empty() {
		t.Errorf("a recorded token must not rotate again: %+v %v", again.Plan, err)
	}
}

func TestVaultPolicyStopsAtThePlatformsTree(t *testing.T) {
	d := devVaultOrSkip(t)
	mount, c := d.mount()
	d.rootPut(mount, "other/secret", tree("a", "1"))

	_, err := Run(background(), c, mount+"/other/secret", Spec{Owner: owner, Data: tree("a", "x")}, Options{})
	if !errors.Is(err, vault.ErrForbidden) {
		t.Errorf("outside the platforms tree: err = %v", err)
	}
	if err := c.KVDestroy(background(), mount, "platforms/production/core/x"); !errors.Is(err, vault.ErrForbidden) {
		t.Errorf("delete is not granted unless the chart allows it: err = %v", err)
	}
	if _, err := c.KVWriteCAS(background(), mount, "platforms/production/core/fresh", tree("a", "1"), 0); err != nil {
		t.Fatal(err)
	}
	// A metadata write on a path with no data would need `create`, which the policy withholds.
	if err := c.KVSetCustomMetadata(background(), mount, "platforms/production/core/nodata", map[string]string{"a": "b"}); !errors.Is(err, vault.ErrForbidden) {
		t.Errorf("metadata before data: err = %v", err)
	}
}

func TestVaultRevokedTokenIsReplacedByALogin(t *testing.T) {
	d := devVaultOrSkip(t)
	mount, _ := d.mount()
	c, _ := vault.NewClient(vault.ClientConfig{Address: d.addr})
	var issued []string
	c.UseLogin(func(context.Context) (string, time.Duration, error) {
		tok := d.controllerToken(mount)
		issued = append(issued, tok)
		return tok, 15 * time.Minute, nil
	})
	d.rootPut(mount, "platforms/production/core/x", tree("a", "1"))
	if _, err := c.KVRead(background(), mount, "platforms/production/core/x"); err != nil {
		t.Fatal(err)
	}
	d.must("POST", "auth/token/revoke", map[string]string{"token": issued[0]})
	if _, err := c.KVRead(background(), mount, "platforms/production/core/x"); err != nil {
		t.Fatalf("a revoked token must be replaced by a fresh login: %v", err)
	}
	if len(issued) != 2 {
		t.Errorf("logins = %d", len(issued))
	}
}
