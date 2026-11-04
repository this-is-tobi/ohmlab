package kubeconfig

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/this-is-tobi/ohmlab/cli/internal/runner"
	"github.com/this-is-tobi/ohmlab/cli/internal/runner/runnertest"
)

func TestFetchCommand(t *testing.T) {
	plain, withVault := inventory(t, "masters: {}\n", false), inventory(t, "masters: {}\n", true)
	for _, c := range []struct {
		inv, password, want string
	}{
		{plain, "", "ANSIBLE_CONFIG=/p/ansible/ansible.cfg ansible 'masters[0]' -i " + plain + " --become -m ansible.builtin.fetch -a 'src=/etc/rancher/k3s/k3s.yaml dest=/d/k3s.yaml flat=true'"},
		{withVault, "/s/pw", "ANSIBLE_CONFIG=/p/ansible/ansible.cfg ansible 'masters[0]' -i " + withVault + " --become -m ansible.builtin.fetch -a 'src=/etc/rancher/k3s/k3s.yaml dest=/d/k3s.yaml flat=true' -e @" + withVault + "/group_vars/vault.yml --vault-password-file /s/pw"},
		{plain, "/s/pw", "ANSIBLE_CONFIG=/p/ansible/ansible.cfg ansible 'masters[0]' -i " + plain + " --become -m ansible.builtin.fetch -a 'src=/etc/rancher/k3s/k3s.yaml dest=/d/k3s.yaml flat=true' --vault-password-file /s/pw"},
	} {
		if got := FetchCommand(Options{Product: "/p", Inventory: c.inv, VaultPasswordFile: c.password}, "/d").String(); got != c.want {
			t.Errorf("got  %s\nwant %s", got, c.want)
		}
	}
}

// fetchedTo is the dest= of a fetch command.
func fetchedTo(t *testing.T, c runner.Cmd) string {
	t.Helper()
	for i, a := range c.Args {
		if a != "-a" || i+1 == len(c.Args) {
			continue
		}
		for _, kv := range strings.Fields(c.Args[i+1]) {
			if d, ok := strings.CutPrefix(kv, "dest="); ok {
				return d
			}
		}
	}
	t.Fatalf("no dest= in %s", c)
	return ""
}

func TestFetchLeavesNoCopy(t *testing.T) {
	k3s, err := os.ReadFile("testdata/k3s.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var dirs []string
	ok := &runnertest.Fake{Handle: func(c runner.Cmd) error {
		dest := fetchedTo(t, c)
		dirs = append(dirs, filepath.Dir(dest))
		return os.WriteFile(dest, k3s, 0o600)
	}}
	cfg, err := Fetch(context.Background(), ok, Options{Product: "/p", Inventory: "testdata/inventory"})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Clusters) != 1 || len(cfg.AuthInfos) != 1 || len(cfg.Contexts) != 1 {
		t.Errorf("fetched %d clusters, %d users, %d contexts; want one each", len(cfg.Clusters), len(cfg.AuthInfos), len(cfg.Contexts))
	}

	failing := &runnertest.Fake{Handle: func(c runner.Cmd) error {
		dest := fetchedTo(t, c)
		dirs = append(dirs, filepath.Dir(dest))
		_ = os.WriteFile(dest, k3s, 0o600)
		return &runner.ExitError{Name: "ansible", Code: 4}
	}}
	if _, err := Fetch(context.Background(), failing, Options{Product: "/p", Inventory: "testdata/inventory"}); err == nil {
		t.Error("a failed fetch returned no error")
	}
	for _, d := range dirs {
		if _, err := os.Stat(d); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s is still there (%v)", d, err)
		}
	}
}

func fetched(t *testing.T) *clientcmdapi.Config {
	t.Helper()
	cfg, err := clientcmd.LoadFromFile("testdata/k3s.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// existing writes a kubeconfig with a context work, current, and a context
// lab at labServer when it is not empty.
func existing(t *testing.T, labServer string) string {
	t.Helper()
	cfg := clientcmdapi.NewConfig()
	cfg.Clusters["work"] = &clientcmdapi.Cluster{Server: "https://work.example.com"}
	cfg.AuthInfos["work"] = &clientcmdapi.AuthInfo{Token: "fake-token"}
	cfg.Contexts["work"] = &clientcmdapi.Context{Cluster: "work", AuthInfo: "work"}
	cfg.CurrentContext = "work"
	if labServer != "" {
		cfg.Clusters["lab"] = &clientcmdapi.Cluster{Server: labServer}
		cfg.AuthInfos["lab"] = &clientcmdapi.AuthInfo{Token: "old-token"}
		cfg.Contexts["lab"] = &clientcmdapi.Context{Cluster: "lab", AuthInfo: "lab"}
	}
	target := filepath.Join(t.TempDir(), "config")
	if err := clientcmd.WriteToFile(*cfg, target); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(target, 0o644); err != nil {
		t.Fatal(err)
	}
	return target
}

func noQuestion(t *testing.T) func(string) (bool, error) {
	return func(q string) (bool, error) {
		t.Errorf("asked %q", q)
		return false, nil
	}
}

func TestMergeIntoAnExistingFile(t *testing.T) {
	target := existing(t, "")
	before, _ := os.ReadFile(target)
	backup, err := Merge(fetched(t), Options{Name: "lab", Target: target}, "192.168.1.100", noQuestion(t))
	if err != nil {
		t.Fatal(err)
	}
	if backup != target+".bak" {
		t.Errorf("backup %q, want %q", backup, target+".bak")
	}
	if saved, _ := os.ReadFile(backup); !bytes.Equal(saved, before) {
		t.Error("the backup is not the old file")
	}
	got, err := clientcmd.LoadFromFile(target)
	if err != nil {
		t.Fatal(err)
	}
	c, u, x := got.Clusters["lab"], got.AuthInfos["lab"], got.Contexts["lab"]
	if c == nil || u == nil || x == nil {
		t.Fatalf("lab missing: %+v %+v %+v", c, u, x)
	}
	if c.Server != "https://192.168.1.100:6443" || string(c.CertificateAuthorityData) != "fake cluster CA" {
		t.Errorf("cluster %s, CA %q", c.Server, c.CertificateAuthorityData)
	}
	if string(u.ClientCertificateData) != "fake client certificate" || string(u.ClientKeyData) != "fake client key" {
		t.Errorf("user %q %q", u.ClientCertificateData, u.ClientKeyData)
	}
	if x.Cluster != "lab" || x.AuthInfo != "lab" {
		t.Errorf("context %+v", x)
	}
	if got.Clusters["work"] == nil || got.Clusters["work"].Server != "https://work.example.com" || got.Contexts["work"] == nil {
		t.Error("work changed")
	}
	if got.CurrentContext != "work" {
		t.Errorf("current context %q, want work", got.CurrentContext)
	}
	if _, ok := got.Clusters["default"]; ok {
		t.Error("k3s's default names leaked")
	}
	if st, _ := os.Stat(target); st.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, want 0600", st.Mode().Perm())
	}
}

func TestMergeIntoANewFile(t *testing.T) {
	target := filepath.Join(t.TempDir(), "kube", "config")
	backup, err := Merge(fetched(t), Options{Name: "lab", Target: target}, "192.168.1.100", noQuestion(t))
	if err != nil {
		t.Fatal(err)
	}
	if backup != "" {
		t.Errorf("backup %q of a new file", backup)
	}
	got, err := clientcmd.LoadFromFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if got.CurrentContext != "lab" {
		t.Errorf("current context %q, want lab", got.CurrentContext)
	}
	if st, _ := os.Stat(target); st.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, want 0600", st.Mode().Perm())
	}
}

func TestMergeAsksBeforeReplacingAnotherServer(t *testing.T) {
	target := existing(t, "https://10.0.0.1:6443")
	before, _ := os.ReadFile(target)
	var asked string
	no := func(q string) (bool, error) { asked = q; return false, nil }
	if _, err := Merge(fetched(t), Options{Name: "lab", Target: target}, "192.168.1.100", no); err == nil {
		t.Error("refusing the question gave no error")
	}
	if !strings.Contains(asked, "https://10.0.0.1:6443") {
		t.Errorf("question %q does not name the old server", asked)
	}
	if after, _ := os.ReadFile(target); !bytes.Equal(after, before) {
		t.Error("the target changed although the answer was no")
	}
	if _, err := os.Stat(target + ".bak"); !errors.Is(err, os.ErrNotExist) {
		t.Error("a backup was written although nothing changed")
	}

	yes := func(string) (bool, error) { return true, nil }
	if _, err := Merge(fetched(t), Options{Name: "lab", Target: target}, "192.168.1.100", yes); err != nil {
		t.Fatal(err)
	}
	if got, _ := clientcmd.LoadFromFile(target); got.Clusters["lab"].Server != "https://192.168.1.100:6443" {
		t.Errorf("server %s, want the new one", got.Clusters["lab"].Server)
	}

	if _, err := Merge(fetched(t), Options{Name: "lab", Target: target}, "192.168.1.100", noQuestion(t)); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultTargetIsTheFileKubectlWrites(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	t.Setenv("KUBECONFIG", a+string(os.PathListSeparator)+b)
	if got := DefaultTarget(); got != b {
		t.Errorf("neither file exists: got %s, want the last, %s (as kubectl)", got, b)
	}
	if err := os.WriteFile(a, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := DefaultTarget(); got != a {
		t.Errorf("got %s, want the first existing file %s", got, a)
	}
	t.Setenv("KUBECONFIG", "")
	if got := DefaultTarget(); got != clientcmd.RecommendedHomeFile || !strings.HasSuffix(got, filepath.Join(".kube", "config")) {
		t.Errorf("without KUBECONFIG: got %s, want ~/.kube/config", got)
	}
}
