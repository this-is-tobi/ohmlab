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

	s, err := Load(write(t, validStorage))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Validate([]string{"storage"}); err != nil {
		t.Fatal(err)
	}
	if g, err := s.Storage.Grace(); err != nil || g != 26*time.Hour {
		t.Errorf("grace = %s, err = %v", g, err)
	}
	if b, err := s.Storage.Stores[0].CapacityBytes(); err != nil || b != 53687091200 {
		t.Errorf("capacity = %d, err = %v", b, err)
	}
	if r := s.Storage.Stores[0].RootCredentials; r.Path != "ohmlab/platforms/production/core/rustfs-backups" || r.Key != "admin" {
		t.Errorf("rootCredentials = %+v", r)
	}
}

const validStorage = `
leaseNamespace: ohmlab-system
storage:
  watchNamespaces: [ohmlab-system]
  backend:
    address: https://vault.vault-operator-system.svc:8200
    caFile: /etc/ohmlab/vault-ca/ca.crt
    auth: {role: ohmlab-controller-storage}
  caFile: /etc/ohmlab/store-ca/ca.crt
  stores:
  - name: backups
    type: rustfs
    endpoint: https://rustfs-svc.rustfs-backups.svc:9000
    region: us-east-1
    capacity: 50Gi
    rootCredentials: {path: ohmlab/platforms/production/core/rustfs-backups, key: admin}
`

func goodStorage() *Storage {
	return &Storage{
		WatchNamespaces: []string{"ohmlab-system"},
		Backend:         Backend{Address: "https://v", Auth: Auth{Role: "ohmlab-controller-storage"}},
		Stores: []Store{{
			Name: "s", Type: "rustfs", Endpoint: "https://rustfs-svc.s.svc:9000", Region: "us-east-1", Capacity: "50Gi",
			RootCredentials: SecretRef{Path: "ohmlab/platforms/e2e/core/rustfs", Key: "admin"},
		}},
	}
}

// withStorage replaces the vault section by a valid storage section, edited.
func withStorage(edit func(*Storage)) func(*Config) {
	return func(c *Config) {
		c.Vault = nil
		s := goodStorage()
		edit(s)
		c.Storage = s
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
		{"storage target without section", []string{"storage"}, func(c *Config) { c.Vault = nil }, "the storage section and the storage target must go together"},
		{"storage section without target", []string{"vault"}, func(c *Config) { c.Storage = goodStorage() }, "the storage section and the storage target must go together"},
		{"storage grouped with vault", []string{"storage", "vault"}, func(c *Config) { c.Storage = goodStorage() }, "the storage target runs alone: it holds the stores' root keys"},
		{"no store", []string{"storage"}, withStorage(func(s *Storage) { s.Stores = []Store{} }), "storage.stores lists nothing"},
		{"an empty store", []string{"storage"}, withStorage(func(s *Storage) { s.Stores = []Store{{}} }), "storage.stores[0]: missing [capacity endpoint name region rootCredentials.key rootCredentials.path type]"},
		{"an external store", []string{"storage"}, withStorage(func(s *Storage) { s.Stores[0].Type = "external" }), `storage.stores[0] (s): type "external" is not one of rustfs`},
		{"a plain http endpoint", []string{"storage"}, withStorage(func(s *Storage) { s.Stores[0].Endpoint = "http://x:9000" }), `storage.stores[0] (s): endpoint "http://x:9000" must be https://<host>:<port>`},
		{"an endpoint without a port", []string{"storage"}, withStorage(func(s *Storage) { s.Stores[0].Endpoint = "https://x" }), `storage.stores[0] (s): endpoint "https://x" must be https://<host>:<port>`},
		{"an endpoint with a path", []string{"storage"}, withStorage(func(s *Storage) { s.Stores[0].Endpoint = "https://x:9000/b" }), `storage.stores[0] (s): endpoint "https://x:9000/b" must be https://<host>:<port>`},
		{"a bucket prefix on rustfs", []string{"storage"}, withStorage(func(s *Storage) { s.Stores[0].BucketPrefix = "p-" }), "storage.stores[0] (s): bucketPrefix must be empty on a rustfs store"},
		{"a capacity that is no quantity", []string{"storage"}, withStorage(func(s *Storage) { s.Stores[0].Capacity = "big" }), `storage.stores[0] (s): capacity "big" is not a quantity`},
		{"a capacity of zero", []string{"storage"}, withStorage(func(s *Storage) { s.Stores[0].Capacity = "0" }), `storage.stores[0] (s): capacity "0" must be above zero`},
		{"a store twice", []string{"storage"}, withStorage(func(s *Storage) { s.Stores = append(s.Stores, s.Stores[0]) }), "storage: store s is listed twice"},
		{"a short rotation grace", []string{"storage"}, withStorage(func(s *Storage) { s.RotationGrace = "30s" }), `storage.rotationGrace "30s": want a duration of at least 1m`},
		{"no storage identity", []string{"storage"}, withStorage(func(s *Storage) {
			s.WatchNamespaces, s.Backend.Address, s.Backend.Auth.Role = nil, "", ""
		}), "missing [storage.backend.address storage.backend.auth.role storage.watchNamespaces]"},
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
