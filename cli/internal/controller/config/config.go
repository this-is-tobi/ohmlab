// Package config is the controller's own configuration file. Backend URLs and
// credentials live here and only here: a custom resource says what, never where
// or as whom.
package config

import (
	"fmt"
	"os"
	"sort"
	"time"

	"sigs.k8s.io/yaml"
)

// Config is the file. Each process receives only the sections of the targets
// it runs.
type Config struct {
	// Resync is how often every object is reconciled again. Defaults to 10m.
	Resync string `json:"resync,omitempty"`
	// LeaseNamespace is where each target keeps its Lease.
	LeaseNamespace string `json:"leaseNamespace,omitempty"`
	// Vault configures the vault target.
	Vault *Vault `json:"vault,omitempty"`
}

// Vault configures the vault target.
type Vault struct {
	// ObserveOnly runs the whole reconcile and writes nothing to Vault.
	ObserveOnly bool `json:"observeOnly,omitempty"`
	// AllowDelete lets deletionPolicy: Delete remove paths. The Vault policy must
	// grant delete too.
	AllowDelete bool `json:"allowDelete,omitempty"`
	// WatchNamespaces are the only namespaces whose VaultSeeds are reconciled.
	WatchNamespaces []string `json:"watchNamespaces"`
	Backend         Backend  `json:"backend"`
}

// Backend is how to reach and log in to Vault.
type Backend struct {
	Address       string `json:"address"`
	CAFile        string `json:"caFile,omitempty"`
	SkipTLSVerify bool   `json:"skipTLSVerify,omitempty"`
	Auth          Auth   `json:"auth"`
}

// Auth is a Kubernetes-auth login.
type Auth struct {
	Mount     string `json:"mount,omitempty"`
	Role      string `json:"role"`
	TokenFile string `json:"tokenFile,omitempty"`
}

// Targets known to the controller.
const TargetVault = "vault"

// Load reads and checks a configuration file. Unknown keys are errors, so a
// typo does not silently become a default.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var c Config
	if err := yaml.UnmarshalStrict(raw, &c); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	return &c, nil
}

// ResyncPeriod returns the resync interval.
func (c *Config) ResyncPeriod() (time.Duration, error) {
	if c.Resync == "" {
		return 10 * time.Minute, nil
	}
	d, err := time.ParseDuration(c.Resync)
	if err != nil || d < time.Second {
		return 0, fmt.Errorf("resync %q: want a duration of at least 1s", c.Resync)
	}
	return d, nil
}

// Validate checks the sections of the given targets. A target that is asked
// for and has no section is an error, and so is a section for a target that is
// not asked for: each process gets only what it needs.
func (c *Config) Validate(targets []string) error {
	if len(targets) == 0 {
		return fmt.Errorf("no target: pass --target")
	}
	want := map[string]bool{}
	for _, t := range targets {
		if t != TargetVault {
			return fmt.Errorf("unknown target %q", t)
		}
		if want[t] {
			return fmt.Errorf("target %q listed twice", t)
		}
		want[t] = true
	}
	if _, err := c.ResyncPeriod(); err != nil {
		return err
	}
	if want[TargetVault] != (c.Vault != nil) {
		return fmt.Errorf("the vault section and the vault target must go together")
	}
	if v := c.Vault; v != nil {
		var missing []string
		if len(v.WatchNamespaces) == 0 {
			missing = append(missing, "vault.watchNamespaces")
		}
		if v.Backend.Address == "" {
			missing = append(missing, "vault.backend.address")
		}
		if v.Backend.Auth.Role == "" {
			missing = append(missing, "vault.backend.auth.role")
		}
		if len(missing) > 0 {
			sort.Strings(missing)
			return fmt.Errorf("missing %v", missing)
		}
	}
	return nil
}
