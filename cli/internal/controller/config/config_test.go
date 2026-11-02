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
