package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), DefaultPath)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadReadsExceptions(t *testing.T) {
	f, err := Load(write(t, `audit:
  tls:
    exceptions:
    - namespace: homepage
      service: homepage
      ports: [3000]
      reason: serves HTTP only
`))
	if err != nil {
		t.Fatal(err)
	}
	want := []TLSException{{Namespace: "homepage", Service: "homepage", Ports: []int32{3000}, Reason: "serves HTTP only"}}
	if !reflect.DeepEqual(f.Audit.TLS.Exceptions, want) {
		t.Errorf("exceptions = %+v, want %+v", f.Audit.TLS.Exceptions, want)
	}
}

func TestLoadEmptyFile(t *testing.T) {
	f, err := Load(write(t, "# nothing yet\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Audit.TLS.Exceptions) != 0 {
		t.Errorf("exceptions = %+v, want none", f.Audit.TLS.Exceptions)
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	_, err := Load(write(t, "audit:\n  tls:\n    exeptions: []\n"))
	if err == nil || !strings.Contains(err.Error(), "exeptions") {
		t.Fatalf("err = %v, want one naming exeptions", err)
	}
	if _, err := Load(write(t, "instanse: x\n")); err == nil || !strings.Contains(err.Error(), "instanse") {
		t.Fatalf("err = %v, want one naming instanse", err)
	}
}

func TestLoadRequiresNamespaceServiceReason(t *testing.T) {
	_, err := Load(write(t, "audit:\n  tls:\n    exceptions:\n    - namespace: homepage\n      service: homepage\n"))
	if err == nil || !strings.Contains(err.Error(), "homepage/homepage") || !strings.Contains(err.Error(), "reason") {
		t.Fatalf("err = %v, want one naming homepage/homepage and reason", err)
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), DefaultPath))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want fs.ErrNotExist", err)
	}
}

// tree writes files under a temporary root, a trailing slash making a
// directory, and returns the root.
func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		p := filepath.Join(root, name)
		dir := p
		if !strings.HasSuffix(name, "/") {
			dir = filepath.Dir(p)
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(name, "/") {
			if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	return root
}

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestResolveDefaults(t *testing.T) {
	got, file, err := Resolve("/w", Flags{}, env(nil))
	if err != nil {
		t.Fatal(err)
	}
	want := Settings{Workspace: "/w", Product: "/w", Instances: "/w/argo-cd/instances", Inventory: "/w/ansible/inventory", ContextName: "ohmlab"}
	if got != want {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
	if file == nil || len(file.Audit.TLS.Exceptions) != 0 {
		t.Errorf("file %+v, want an empty one", file)
	}
}

func TestResolveFindsTheWorkspaceVaultPasswordFile(t *testing.T) {
	root := tree(t, map[string]string{".git/": "", "ansible/.vault_password": "x"})
	got, _, err := Resolve(root, Flags{}, env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "ansible", ".vault_password"); got.VaultPasswordFile != want {
		t.Errorf("got %q, want %q", got.VaultPasswordFile, want)
	}
}

func TestResolvePrecedence(t *testing.T) {
	root := tree(t, map[string]string{".git/": "", "ansible/.vault_password": "x", DefaultPath: "contextName: a\ninventory: inv-a\nvaultPasswordFile: pw-a\n"})
	flags := Flags{ContextName: "c", Inventory: "inv-c", VaultPasswordFile: "pw-c"}
	vars := map[string]string{"KUBE_CONTEXT_NAME": "b", "ANSIBLE_INVENTORY": "inv-b", "ANSIBLE_VAULT_PASSWORD_FILE": "pw-b"}
	for _, c := range []struct {
		name  string
		flags Flags
		env   map[string]string
		want  string
	}{
		{"flag", flags, vars, "c"},
		{"environment", Flags{}, vars, "b"},
		{"file", Flags{}, nil, "a"},
	} {
		got, _, err := Resolve(root, c.flags, env(c.env))
		if err != nil {
			t.Fatal(err)
		}
		want := Settings{Workspace: root, Product: root, Instances: filepath.Join(root, "argo-cd/instances"),
			Inventory: filepath.Join(root, "inv-"+c.want), VaultPasswordFile: filepath.Join(root, "pw-"+c.want), ContextName: c.want}
		if got != want {
			t.Errorf("%s: got %+v\nwant %+v", c.name, got, want)
		}
	}
}

func TestResolveFilePathsAreRelativeToTheFile(t *testing.T) {
	root := tree(t, map[string]string{
		"repo/.git/":          "",
		"repo/argo-cd/":       "",
		"repo/" + DefaultPath: "product: ../ohmlab\ninventory: ansible/inventory\ninstance: lab\n",
	})
	repo, cwd := filepath.Join(root, "repo"), filepath.Join(root, "repo", "argo-cd")
	got, _, err := Resolve(cwd, Flags{}, env(nil))
	if err != nil {
		t.Fatal(err)
	}
	want := Settings{Workspace: repo, Product: filepath.Join(root, "ohmlab"), Instances: filepath.Join(repo, "argo-cd/instances"),
		Instance: "lab", Inventory: filepath.Join(repo, "ansible/inventory"), ContextName: "ohmlab"}
	if got != want {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
	got, _, err = Resolve(cwd, Flags{Inventory: "inv"}, env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(cwd, "inv"); got.Inventory != want {
		t.Errorf("a relative flag gave %s, want %s", got.Inventory, want)
	}
}

func TestFindStopsAtTheRepository(t *testing.T) {
	root := tree(t, map[string]string{DefaultPath: "", "repo/.git/": "", "repo/sub/": ""})
	sub := filepath.Join(root, "repo", "sub")
	if p, err := Find(sub); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("found %s above the repository (err %v)", p, err)
	}
	inRepo := filepath.Join(root, "repo", DefaultPath)
	if err := os.WriteFile(inRepo, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if p, err := Find(sub); err != nil || p != inRepo {
		t.Errorf("got %s, %v; want %s", p, err, inRepo)
	}
}

func TestResolveNamedConfigMustExist(t *testing.T) {
	if _, _, err := Resolve(t.TempDir(), Flags{Config: "missing.yaml"}, env(nil)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("err = %v, want a missing file", err)
	}
}
