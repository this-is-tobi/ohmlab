package kubeclient

import (
	"os"
	"path/filepath"
	"testing"
)

const kubeconfig = `apiVersion: v1
kind: Config
current-context: a
clusters:
- name: a
  cluster: {server: "https://a.example:6443"}
- name: b
  cluster: {server: "https://b.example:6443"}
contexts:
- name: a
  context: {cluster: a, user: u}
- name: b
  context: {cluster: b, user: u}
users:
- name: u
  user: {token: t}
`

func TestLoadPicksTheContext(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(p, []byte(kubeconfig), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ context, wantName, wantHost string }{
		{"", "a", "https://a.example:6443"},
		{"b", "b", "https://b.example:6443"},
	} {
		cfg, name, err := Load(p, c.context)
		if err != nil {
			t.Fatal(err)
		}
		if name != c.wantName || cfg.Host != c.wantHost {
			t.Errorf("Load(%q) = %s at %s, want %s at %s", c.context, name, cfg.Host, c.wantName, c.wantHost)
		}
	}
	if _, _, err := Load(p, "missing"); err == nil {
		t.Error("Load of a missing context: want an error")
	}
}
