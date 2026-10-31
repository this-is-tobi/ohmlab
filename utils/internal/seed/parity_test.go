package seed

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/this-is-tobi/ohmlab/utils/internal/secretgen"
	"github.com/this-is-tobi/ohmlab/utils/internal/vault"
)

// Parity: the engine the controller runs must leave Vault in the state the CLI
// engine does, for the secrets this repository really declares. Both run
// against a real Vault and are compared modulo randomness: same keys, same
// shapes and lengths, references equal to their source, age keys paired,
// hashes valid.

// mountToken stands for the mount name in declared references.
const mountToken = "@mount@"

var parityFiles = []string{
	"testdata/parity-homelab.yaml",
	"../../../argo-cd/instances/_example/values/core/vault-operator.yaml",
}

func loadEntries(t *testing.T, file, mount string) []vault.SecretEntry {
	t.Helper()
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Skipf("%s is not here: %v", file, err)
	}
	var doc struct {
		Ohmlab struct {
			Vault struct {
				Secrets []map[string]any `yaml:"secrets"`
			} `yaml:"vault"`
		} `yaml:"ohmlab"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(doc.Ohmlab.Vault.Secrets)
	// The entries name the mount `homelab`; the test works in a mount of its own.
	b = []byte(strings.ReplaceAll(string(b), "homelab/", mount+"/"))
	var entries []vault.SecretEntry
	if err := json.Unmarshal(b, &entries); err != nil {
		t.Fatal(err)
	}
	return entries
}

// side is one engine's final state.
type side struct {
	name  string
	mount string
	state map[string]map[string]any // full path -> data
}

func TestParityWithTheCLIEngineOnTheDeclaredSecrets(t *testing.T) {
	d := devVaultOrSkip(t)
	for _, file := range parityFiles {
		file := file
		t.Run(filepath.Base(filepath.Dir(filepath.Dir(filepath.Dir(file))))+"-"+filepath.Base(filepath.Dir(file)), func(t *testing.T) {
			legacyMount, _ := d.mount()
			newMount, controller := d.mount()
			legacyEntries := loadEntries(t, file, legacyMount)
			newEntries := loadEntries(t, file, newMount)
			// The same entries with a placeholder for the mount, to compare against.
			declared := loadEntries(t, file, mountToken)
			if len(legacyEntries) == 0 {
				t.Skip("no secrets declared")
			}

			// The CLI engine, in file order (Keycloak's paths come first for a reason).
			legacy, err := vault.NewClient(vault.ClientConfig{Address: d.addr})
			if err != nil {
				t.Fatal(err)
			}
			if err := legacy.Authenticate(vault.AuthToken, "", map[string]string{"token": d.root}); err != nil {
				t.Fatal(err)
			}
			for _, r := range vault.RunInit(legacy, legacyEntries, vault.InitOptions{}) {
				if r.Error != nil {
					t.Fatalf("CLI engine: %s: %v", r.Path, r.Error)
				}
			}

			// The controller engine, in the worst order: sources last. Rounds stand in
			// for the retries a controller makes until every reference resolves.
			owner := func(path string) string { return "vault-operator-system/" + strings.ReplaceAll(path, "/", "-") }
			managed := map[string][]string{}
			for round := 0; ; round++ {
				if round > len(newEntries) {
					t.Fatal("references never resolved")
				}
				pending := 0
				for i := len(newEntries) - 1; i >= 0; i-- {
					e := newEntries[i]
					res, err := Run(background(), controller, e.Path, Spec{Owner: owner(e.Path), Data: e.Data, Managed: managed[e.Path]}, Options{})
					if err != nil {
						t.Fatalf("controller engine: %s: %v", e.Path, err)
					}
					switch res.Plan.Blocked {
					case "":
						managed[e.Path] = res.Plan.Managed
					case ReasonRefPending:
						pending++
					default:
						t.Fatalf("%s blocked: %s %s", e.Path, res.Plan.Blocked, res.Plan.Message)
					}
				}
				if pending == 0 {
					break
				}
			}

			read := func(name, mount string, entries []vault.SecretEntry) side {
				s := side{name: name, mount: mount, state: map[string]map[string]any{}}
				for _, e := range entries {
					m, p, _ := strings.Cut(e.Path, "/")
					s.state[e.Path], _, _ = d.rootRead(m, p)
				}
				return s
			}
			cli, ctl := read("CLI", legacyMount, legacyEntries), read("controller", newMount, newEntries)
			for i := range legacyEntries {
				l, n, dcl := legacyEntries[i], newEntries[i], declared[i]
				t.Run(strings.TrimPrefix(l.Path, legacyMount+"/"), func(t *testing.T) {
					lroot, nroot := cli.state[l.Path], ctl.state[n.Path]
					compare(t, "", dcl.Data, lroot, nroot, cli, ctl, lroot, nroot)
					checkAgePairs(t, dcl.Data, lroot, nroot)
				})
			}

			// Settled: a second pass over every path writes nothing.
			for _, e := range newEntries {
				res, err := Run(background(), controller, e.Path, Spec{Owner: owner(e.Path), Data: e.Data, Managed: managed[e.Path]}, Options{})
				if err != nil || !res.Plan.Empty() {
					t.Errorf("%s is not settled: %+v %v", e.Path, res.Plan, err)
				}
			}
		})
	}
}

// compare walks the declared tree and checks both engines' states against it.
func compare(t *testing.T, prefix string, declared, lvl, nvl map[string]any, cli, ctl side, lroot, nroot map[string]any) {
	t.Helper()
	keys := make([]string, 0, len(declared))
	for k := range declared {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		key := join(prefix, k)
		lv, lok := lvl[k]
		nv, nok := nvl[k]
		if lok != nok {
			t.Errorf("%s: present in the CLI state %v, in the controller state %v", key, lok, nok)
			continue
		}
		switch d := declared[k].(type) {
		case map[string]any:
			lm, _ := lv.(map[string]any)
			nm, _ := nv.(map[string]any)
			compare(t, key, d, lm, nm, cli, ctl, lroot, nroot)
		case string:
			ls, _ := lv.(string)
			ns, _ := nv.(string)
			checkLeaf(t, key, d, ls, ns, cli, ctl, lroot, nroot)
		default:
			if fmt.Sprint(lv) != fmt.Sprint(nv) {
				t.Errorf("%s: %v vs %v", key, lv, nv)
			}
		}
	}
}

func checkLeaf(t *testing.T, key, tmpl, legacy, got string, cli, ctl side, lroot, nroot map[string]any) {
	t.Helper()
	switch secretgen.Classify(tmpl) {
	case secretgen.KindLiteral:
		if legacy != tmpl || got != tmpl {
			t.Errorf("%s: the literal %q became %q (CLI) and %q (controller)", key, tmpl, legacy, got)
		}
	case secretgen.KindRef:
		for _, s := range []struct {
			side side
			val  string
		}{{cli, legacy}, {ctl, got}} {
			r := secretgen.Renderer{Ref: func(path, keyPath string) (string, error) {
				if v := secretgen.NavigateJSON(s.side.state[strings.Replace(path, mountToken, s.side.mount, 1)], keyPath); v != "" {
					return v, nil
				}
				return "", secretgen.ErrRefNotFound
			}}
			if want, err := r.Render(tmpl); err != nil || s.val != want {
				t.Errorf("%s (%s): a reference must equal its source (%v)", key, s.side.name, err)
			}
		}
	case secretgen.KindBcrypt:
		target, _ := secretgen.BcryptTarget(tmpl)
		if !secretgen.BcryptMatches(legacy, secretgen.NavigateJSON(lroot, target)) {
			t.Errorf("%s (CLI): the hash does not match %s", key, target)
		}
		if !secretgen.BcryptMatches(got, secretgen.NavigateJSON(nroot, target)) {
			t.Errorf("%s (controller): the hash does not match %s", key, target)
		}
	default: // generated
		if a, b := shapeOf(tmpl, legacy), shapeOf(tmpl, got); a != b {
			t.Errorf("%s: shape %q (CLI) vs %q (controller)", key, a, b)
		}
	}
}

// checkAgePairs verifies each state's public key belongs to its secret key.
func checkAgePairs(t *testing.T, declared, lroot, nroot map[string]any) {
	t.Helper()
	secrets, publics := ageLeaves(declared)
	for i := range secrets {
		if i >= len(publics) {
			break
		}
		for name, root := range map[string]map[string]any{"CLI": lroot, "controller": nroot} {
			s, _ := stringAt(root, secrets[i])
			p, _ := stringAt(root, publics[i])
			pair, err := secretgen.AgePairFromSecret(s)
			if err != nil || pair.Public != p {
				t.Errorf("%s: %s and %s are not a pair (%v)", name, secrets[i], publics[i], err)
			}
		}
	}
}

// shapeOf describes a generated value without comparing its content.
func shapeOf(tmpl, v string) string {
	switch {
	case strings.HasPrefix(tmpl, "<age:secret>"):
		return "age-secret:" + strconv.FormatBool(strings.HasPrefix(v, "AGE-SECRET-KEY-"))
	case strings.HasPrefix(tmpl, "<age:public>"):
		return "age-public:" + strconv.FormatBool(strings.HasPrefix(v, "age1"))
	}
	return "len:" + strconv.Itoa(len(v))
}
