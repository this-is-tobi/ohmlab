// Package config is the controller's own configuration file. Backend URLs and
// credentials live here and only here: a custom resource says what, never where
// or as whom.
package config

import (
	"fmt"
	"os"
	"sort"
	"strings"
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
	// Certificates configures the certificates target.
	Certificates *Certificates `json:"certificates,omitempty"`
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

// Certificates configures the certificates target: the workloads to roll when
// their Certificate is issued again.
type Certificates struct {
	Restarts []Restart `json:"restarts"`
}

// Restart is one Certificate and the workloads that read it only when they
// start. They live in the Certificate's namespace: a Secret mounts nowhere else.
type Restart struct {
	Namespace   string     `json:"namespace"`
	Certificate string     `json:"certificate"`
	Workloads   []Workload `json:"workloads"`
}

// Workload is an apps/v1 workload, by kind and name.
type Workload struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

// WorkloadKinds are the kinds a Restart may list.
var WorkloadKinds = []string{"DaemonSet", "Deployment", "StatefulSet"}

// Namespaces returns the namespaces of the restarts, unique and sorted.
func (c *Certificates) Namespaces() []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range c.Restarts {
		if !seen[r.Namespace] {
			seen[r.Namespace] = true
			out = append(out, r.Namespace)
		}
	}
	sort.Strings(out)
	return out
}

func (c *Certificates) validate() error {
	if len(c.Restarts) == 0 {
		return fmt.Errorf("certificates.restarts lists nothing")
	}
	certs := map[string]bool{}
	owner := map[string]string{} // "Kind ns/name" -> "ns/certificate"
	for i, r := range c.Restarts {
		var missing []string
		if r.Certificate == "" {
			missing = append(missing, "certificate")
		}
		if r.Namespace == "" {
			missing = append(missing, "namespace")
		}
		if len(missing) > 0 {
			return fmt.Errorf("certificates.restarts[%d]: missing %v", i, missing)
		}
		id := r.Namespace + "/" + r.Certificate
		if len(r.Workloads) == 0 {
			return fmt.Errorf("certificates.restarts[%d] (%s) lists no workload", i, id)
		}
		if certs[id] {
			return fmt.Errorf("certificates: %s is listed twice", id)
		}
		certs[id] = true
		for j, w := range r.Workloads {
			if w.Kind == "" || w.Name == "" {
				return fmt.Errorf("certificates.restarts[%d] (%s): workloads[%d] needs kind and name", i, id, j)
			}
			if !contains(WorkloadKinds, w.Kind) {
				return fmt.Errorf("certificates.restarts[%d] (%s): kind %q is not one of %s", i, id, w.Kind, strings.Join(WorkloadKinds, ", "))
			}
			key := w.Kind + " " + r.Namespace + "/" + w.Name
			if prev, ok := owner[key]; ok {
				return fmt.Errorf("certificates: %s is listed under %s and %s: a workload carries one certificate's revision", key, prev, id)
			}
			owner[key] = id
		}
	}
	return nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// Targets known to the controller.
const (
	TargetVault        = "vault"
	TargetCertificates = "certificates"
)

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
		if t != TargetVault && t != TargetCertificates {
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
	if want[TargetCertificates] != (c.Certificates != nil) {
		return fmt.Errorf("the certificates section and the certificates target must go together")
	}
	if c.Certificates != nil {
		if err := c.Certificates.validate(); err != nil {
			return err
		}
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
