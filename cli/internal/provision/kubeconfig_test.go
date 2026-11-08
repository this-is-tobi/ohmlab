package provision

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/client-go/tools/clientcmd"
)

// twoContexts is a workstation's kubeconfig: a work cluster, current, and the
// lab one.
const twoContexts = `apiVersion: v1
kind: Config
clusters:
- name: work
  cluster: {server: "https://work.example:443"}
- name: lab
  cluster: {server: "https://10.0.0.1:6443", certificate-authority-data: Y2E=}
users:
- name: work
  user:
    exec: {apiVersion: client.authentication.k8s.io/v1beta1, command: tsh, args: [kube, credentials]}
- name: lab
  user: {client-certificate-data: Y2VydA==, client-key-data: a2V5}
contexts:
- name: work
  context: {cluster: work, user: work}
- name: lab
  context: {cluster: lab, user: lab}
current-context: work
`

func writeKubeconfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kube", "config")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadContextKeepsOnlyTheNamedOne(t *testing.T) {
	c, err := LoadContext(writeKubeconfig(t, twoContexts), "lab")
	if err != nil {
		t.Fatal(err)
	}
	if !c.Found || c.Name != "lab" || c.Server != "https://10.0.0.1:6443" {
		t.Errorf("context %+v", c)
	}
	path, err := c.Write(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(path); err != nil || st.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, %v; want 0600", st.Mode().Perm(), err)
	}
	cfg, err := clientcmd.LoadFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CurrentContext != "lab" || len(cfg.Contexts) != 1 || cfg.Contexts["lab"] == nil || len(cfg.AuthInfos) != 1 || len(cfg.Clusters) != 1 {
		t.Errorf("current %q, contexts %v, users %v, clusters %v", cfg.CurrentContext, cfg.Contexts, cfg.AuthInfos, cfg.Clusters)
	}
	if x := cfg.Clusters[cfg.Contexts["lab"].Cluster]; x == nil || x.Server != "https://10.0.0.1:6443" {
		t.Errorf("cluster %+v", x)
	}
}

// The kubeconfig is written elsewhere than the user's: a path relative to
// the user's file has to be made absolute.
func TestLoadContextResolvesRelativePaths(t *testing.T) {
	src := writeKubeconfig(t, `apiVersion: v1
kind: Config
clusters:
- name: lab
  cluster: {server: "https://10.0.0.1:6443"}
users:
- name: lab
  user: {client-certificate: certs/c.crt, client-key: certs/c.key}
contexts:
- name: lab
  context: {cluster: lab, user: lab}
`)
	c, err := LoadContext(src, "lab")
	if err != nil {
		t.Fatal(err)
	}
	path, err := c.Write(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := clientcmd.LoadFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := cfg.AuthInfos["lab"].ClientCertificate, filepath.Join(filepath.Dir(src), "certs", "c.crt"); got != want {
		t.Errorf("client certificate %s, want %s", got, want)
	}
}

func TestLoadContextMissing(t *testing.T) {
	c, err := LoadContext(writeKubeconfig(t, twoContexts), "nope")
	if err != nil || c.Found {
		t.Fatalf("%+v, %v; want not found and no error", c, err)
	}
	path, err := c.Write(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := clientcmd.LoadFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CurrentContext != "" || len(cfg.Contexts)+len(cfg.Clusters)+len(cfg.AuthInfos) != 0 {
		t.Errorf("current %q, contexts %v, clusters %v, users %v; want none", cfg.CurrentContext, cfg.Contexts, cfg.Clusters, cfg.AuthInfos)
	}
}

// kubectl config set-context makes a context that names no cluster.
func TestLoadContextWithoutCluster(t *testing.T) {
	_, err := LoadContext(writeKubeconfig(t, `apiVersion: v1
kind: Config
contexts:
- name: lab
  context: {namespace: x}
`), "lab")
	if err == nil || !strings.Contains(err.Error(), "names no cluster") {
		t.Errorf("got %v, want an error saying the context names no cluster", err)
	}
}

func TestLoadContextFromEnvironment(t *testing.T) {
	t.Setenv("KUBECONFIG", writeKubeconfig(t, twoContexts))
	if c, err := LoadContext("", "lab"); err != nil || !c.Found {
		t.Errorf("%+v, %v; want lab", c, err)
	}
}

func TestLoadContextExplicitMissing(t *testing.T) {
	if _, err := LoadContext(filepath.Join(t.TempDir(), "nope"), "lab"); err == nil {
		t.Error("no error for a kubeconfig that does not exist")
	}
}
