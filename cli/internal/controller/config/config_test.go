package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "controller.yaml")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

const valid = `
resync: 15s
leaseNamespace: ohmlab-system
vault:
  allowDelete: false
  watchNamespaces: [vault-operator-system]
  backend:
    address: https://vault.vault-operator-system.svc:8200
    caFile: /etc/ohmlab/vault-ca/ca.crt
    auth: {role: ohmlab-controller-vault}
`

func TestLoadAndValidate(t *testing.T) {
	c, err := Load(write(t, valid))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Validate([]string{"vault"}); err != nil {
		t.Fatal(err)
	}
	if d, _ := c.ResyncPeriod(); d != 15*time.Second {
		t.Errorf("resync = %s", d)
	}
	if c.Vault.Backend.Auth.Role != "ohmlab-controller-vault" || c.Vault.WatchNamespaces[0] != "vault-operator-system" {
		t.Errorf("config = %+v", c.Vault)
	}
}

const withCertificates = valid + `
certificates:
  restarts:
  - namespace: gitea
    certificate: gitea-tls
    workloads:
    - {kind: Deployment, name: gitea}
  - namespace: kube-system
    certificate: hubble-oauth2-proxy-tls
    workloads:
    - {kind: Deployment, name: hubble-oauth2-proxy}
  - namespace: gitea
    certificate: other-tls
    workloads:
    - {kind: StatefulSet, name: gitea-valkey}
`

func TestBothTargetsValidate(t *testing.T) {
	c, err := Load(write(t, withCertificates))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Validate([]string{"vault", "certificates"}); err != nil {
		t.Fatal(err)
	}
}

func TestCertificateNamespacesAreUniqueAndSorted(t *testing.T) {
	c, _ := Load(write(t, withCertificates))
	if got := strings.Join(c.Certificates.Namespaces(), " "); got != "gitea kube-system" {
		t.Errorf("namespaces = %q", got)
	}
}

func TestCertificatesValidateRejects(t *testing.T) {
	entry := func(kind, name string) Restart {
		return Restart{Namespace: "gitea", Certificate: "gitea-tls", Workloads: []Workload{{Kind: kind, Name: name}}}
	}
	tests := []struct {
		name    string
		targets []string
		certs   *Certificates
		want    string
	}{
		{"target without section", []string{"certificates"}, nil, "the certificates section and the certificates target must go together"},
		{"section without target", []string{"vault"}, &Certificates{Restarts: []Restart{entry("Deployment", "gitea")}}, "the certificates section and the certificates target must go together"},
		{"empty list", []string{"certificates"}, &Certificates{}, "certificates.restarts lists nothing"},
		{"no namespace nor certificate", []string{"certificates"}, &Certificates{Restarts: []Restart{{Workloads: []Workload{{Kind: "Deployment", Name: "gitea"}}}}}, "certificates.restarts[0]: missing [certificate namespace]"},
		{"no workload", []string{"certificates"}, &Certificates{Restarts: []Restart{{Namespace: "gitea", Certificate: "gitea-tls"}}}, "certificates.restarts[0] (gitea/gitea-tls) lists no workload"},
		{"unknown kind", []string{"certificates"}, &Certificates{Restarts: []Restart{entry("Pod", "gitea")}}, `certificates.restarts[0] (gitea/gitea-tls): kind "Pod" is not one of DaemonSet, Deployment, StatefulSet`},
		{"workload without name", []string{"certificates"}, &Certificates{Restarts: []Restart{entry("Deployment", "")}}, "certificates.restarts[0] (gitea/gitea-tls): workloads[0] needs kind and name"},
		{"certificate twice", []string{"certificates"}, &Certificates{Restarts: []Restart{entry("Deployment", "a"), entry("Deployment", "b")}}, "certificates: gitea/gitea-tls is listed twice"},
		{"workload under two certificates", []string{"certificates"}, &Certificates{Restarts: []Restart{
			{Namespace: "gitea", Certificate: "a-tls", Workloads: []Workload{{Kind: "Deployment", Name: "gitea"}}},
			{Namespace: "gitea", Certificate: "b-tls", Workloads: []Workload{{Kind: "Deployment", Name: "gitea"}}},
		}}, "certificates: Deployment gitea/gitea is listed under gitea/a-tls and gitea/b-tls: a workload carries one certificate's revision"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Config{Certificates: tt.certs}
			if has(tt.targets, "vault") {
				c.Vault = &Vault{WatchNamespaces: []string{"v"}, Backend: Backend{Address: "https://v", Auth: Auth{Role: "r"}}}
			}
			err := c.Validate(tt.targets)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func has(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func TestResyncDefaultsToTenMinutes(t *testing.T) {
	if d, err := (&Config{}).ResyncPeriod(); err != nil || d != 10*time.Minute {
		t.Errorf("d=%s err=%v", d, err)
	}
}

func TestAnUnknownKeyIsAnError(t *testing.T) {
	if _, err := Load(write(t, "vualt: {}\n")); err == nil {
		t.Error("a typo must not become a default")
	}
}

func TestValidateRejects(t *testing.T) {
	c, _ := Load(write(t, valid))
	tests := []struct {
		name    string
		targets []string
		mutate  func(*Config)
		want    string
	}{
		{"no target", nil, nil, "no target"},
		{"unknown target", []string{"nope"}, nil, "unknown target"},
		{"a reserved target", []string{"harbor"}, nil, "unknown target"},
		{"duplicate", []string{"vault", "vault"}, nil, "twice"},
		{"section without target", []string{"vault"}, func(c *Config) { c.Vault = nil }, "go together"},
		{"no namespaces", []string{"vault"}, func(c *Config) { c.Vault.WatchNamespaces = nil }, "watchNamespaces"},
		{"no address", []string{"vault"}, func(c *Config) { c.Vault.Backend.Address = "" }, "address"},
		{"no role", []string{"vault"}, func(c *Config) { c.Vault.Backend.Auth.Role = "" }, "role"},
		{"bad resync", []string{"vault"}, func(c *Config) { c.Resync = "soon" }, "resync"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cp := *c
			v := *c.Vault
			cp.Vault = &v
			if tt.mutate != nil {
				tt.mutate(&cp)
			}
			err := cp.Validate(tt.targets)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestATargetNotAskedForMustHaveNoSection(t *testing.T) {
	c, _ := Load(write(t, valid))
	if err := c.Validate([]string{}); err == nil {
		t.Error("want an error")
	}
}
