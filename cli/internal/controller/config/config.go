// Package config is the controller's own configuration file. Backend URLs and
// credentials live here and only here: a custom resource says what, never where
// or as whom.
package config

import (
	"fmt"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
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
	// Storage configures the storage target.
	Storage *Storage `json:"storage,omitempty"`
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

// Storage configures the storage target: the stores it manages and its own
// Vault identity, with which it writes each claim's record.
type Storage struct {
	// ObserveOnly plans every store and Vault change and writes neither.
	ObserveOnly bool `json:"observeOnly,omitempty"`
	// AllowDelete lets deletionPolicy: Delete remove a claim's identity, and its
	// bucket when that holds no object version.
	AllowDelete bool `json:"allowDelete,omitempty"`
	// WatchNamespaces are the only namespaces whose S3Buckets are reconciled.
	WatchNamespaces []string `json:"watchNamespaces"`
	// RotationGrace is how long a rotated key keeps working. Defaults to 26h:
	// VSO's refresh plus a day's backup cycle.
	RotationGrace string  `json:"rotationGrace,omitempty"`
	Backend       Backend `json:"backend"`
	// CAFile is the stores' CA bundle. Empty: the system roots.
	CAFile string  `json:"caFile,omitempty"`
	Stores []Store `json:"stores"`
}

// Store is one S3 endpoint the storage target manages.
type Store struct {
	Name string `json:"name"`
	Type string `json:"type"`
	// Endpoint is https://<host>:<port>.
	Endpoint     string `json:"endpoint"`
	Region       string `json:"region"`
	PathStyle    bool   `json:"pathStyle,omitempty"`
	BucketPrefix string `json:"bucketPrefix,omitempty"`
	// Capacity is the store's volume, a quantity: the quotas it holds are
	// measured against it.
	Capacity string `json:"capacity"`
	// RootCredentials is where the store's root keys live in Vault. The target
	// reads them itself, so no Secret holds a copy.
	RootCredentials SecretRef `json:"rootCredentials"`
}

// SecretRef names a Vault KV entry.
type SecretRef struct {
	// Path is <mount>/<path>.
	Path string `json:"path"`
	// Key is the key whose value holds accessKey and secretKey.
	Key string `json:"key"`
}

// StoreTypes are the store types the storage target manages.
var StoreTypes = []string{"rustfs"}

// Grace returns how long a rotated key keeps working.
func (s *Storage) Grace() (time.Duration, error) {
	if s.RotationGrace == "" {
		return 26 * time.Hour, nil
	}
	d, err := time.ParseDuration(s.RotationGrace)
	if err != nil || d < time.Minute {
		return 0, fmt.Errorf("storage.rotationGrace %q: want a duration of at least 1m", s.RotationGrace)
	}
	return d, nil
}

// CapacityBytes returns the store's capacity in bytes.
func (s Store) CapacityBytes() (int64, error) {
	q, err := resource.ParseQuantity(s.Capacity)
	if err != nil {
		return 0, fmt.Errorf("capacity %q is not a quantity", s.Capacity)
	}
	if q.Sign() <= 0 {
		return 0, fmt.Errorf("capacity %q must be above zero", s.Capacity)
	}
	return q.Value(), nil
}

func (s *Storage) validate() error {
	var missing []string
	if len(s.WatchNamespaces) == 0 {
		missing = append(missing, "storage.watchNamespaces")
	}
	if s.Backend.Address == "" {
		missing = append(missing, "storage.backend.address")
	}
	if s.Backend.Auth.Role == "" {
		missing = append(missing, "storage.backend.auth.role")
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("missing %v", missing)
	}
	if _, err := s.Grace(); err != nil {
		return err
	}
	if len(s.Stores) == 0 {
		return fmt.Errorf("storage.stores lists nothing")
	}
	names := map[string]bool{}
	for i, st := range s.Stores {
		var unset []string
		for _, f := range []struct{ name, value string }{
			{"capacity", st.Capacity}, {"endpoint", st.Endpoint}, {"name", st.Name}, {"region", st.Region},
			{"rootCredentials.key", st.RootCredentials.Key}, {"rootCredentials.path", st.RootCredentials.Path}, {"type", st.Type},
		} {
			if f.value == "" {
				unset = append(unset, f.name)
			}
		}
		if len(unset) > 0 {
			return fmt.Errorf("storage.stores[%d]: missing %v", i, unset)
		}
		at := fmt.Sprintf("storage.stores[%d] (%s)", i, st.Name)
		if !contains(StoreTypes, st.Type) {
			return fmt.Errorf("%s: type %q is not one of %s", at, st.Type, strings.Join(StoreTypes, ", "))
		}
		if !isHTTPSHostPort(st.Endpoint) {
			return fmt.Errorf("%s: endpoint %q must be https://<host>:<port>", at, st.Endpoint)
		}
		if st.BucketPrefix != "" {
			return fmt.Errorf("%s: bucketPrefix must be empty on a %s store", at, st.Type)
		}
		if _, err := st.CapacityBytes(); err != nil {
			return fmt.Errorf("%s: %w", at, err)
		}
		if names[st.Name] {
			return fmt.Errorf("storage: store %s is listed twice", st.Name)
		}
		names[st.Name] = true
	}
	return nil
}

// isHTTPSHostPort reports whether s is exactly https://<host>:<port>.
func isHTTPSHostPort(s string) bool {
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Opaque != "" || u.Hostname() == "" {
		return false
	}
	if u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return false
	}
	port, err := strconv.Atoi(u.Port())
	return err == nil && port > 0 && port < 65536
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
	TargetStorage      = "storage"
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
		if t != TargetVault && t != TargetCertificates && t != TargetStorage {
			return fmt.Errorf("unknown target %q", t)
		}
		if want[t] {
			return fmt.Errorf("target %q listed twice", t)
		}
		want[t] = true
	}
	if want[TargetStorage] && len(targets) > 1 {
		return fmt.Errorf("the storage target runs alone: it holds the stores' root keys")
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
	if want[TargetStorage] != (c.Storage != nil) {
		return fmt.Errorf("the storage section and the storage target must go together")
	}
	if c.Storage != nil {
		if err := c.Storage.validate(); err != nil {
			return err
		}
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
