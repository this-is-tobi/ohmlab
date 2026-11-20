// Package breakglass collects what a rebuild needs and the cluster cannot keep
// for itself: Vault's unseal keys and root token, each cloud replica's key,
// crypt passwords and settings, and the k3s server token. The owner keeps the
// bundle offline, in a password manager; it never goes to a store.
package breakglass

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"
)

// ReplicaLabel marks the ConfigMap of a store's cloud replica (the rustfs chart).
const ReplicaLabel = "ohmlab.fr/replica"

// Options says where the pieces are.
type Options struct {
	// VaultNamespace and VaultSecret hold the unseal keys and the root token
	// bank-vaults keeps; vault-operator-system and vault-unseal-keys by default.
	VaultNamespace, VaultSecret string
	// K3sTokenFile holds the k3s server token; NoK3s leaves it out, for a
	// cluster that is not self-hosted k3s. One of the two is required.
	K3sTokenFile string
	NoK3s        bool
	// Now is the clock; time.Now by default.
	Now func() time.Time
}

// Bundle is what the owner keeps.
type Bundle struct {
	Generated string    `json:"generated"`
	Cluster   string    `json:"cluster"`
	Vault     Vault     `json:"vault"`
	K3s       *K3s      `json:"k3s,omitempty"`
	Replicas  []Replica `json:"replicas"`
}

// Vault opens a Vault restored from a snapshot of this one.
type Vault struct {
	RootToken  string   `json:"rootToken"`
	UnsealKeys []string `json:"unsealKeys"`
}

// K3s restores an etcd snapshot of this cluster.
type K3s struct {
	Token string `json:"token"`
}

// Replica reads a store's cloud replica.
type Replica struct {
	Store                   string `json:"store"`
	Namespace               string `json:"namespace"`
	Provider                string `json:"provider"`
	Endpoint                string `json:"endpoint"`
	Region                  string `json:"region"`
	Bucket                  string `json:"bucket"`
	Prefix                  string `json:"prefix"`
	AccessKey               string `json:"accessKey"`
	SecretKey               string `json:"secretKey"`
	CryptPassword           string `json:"cryptPassword"`
	CryptPassword2          string `json:"cryptPassword2"`
	FilenameEncryption      string `json:"filenameEncryption"`
	DirectoryNameEncryption string `json:"directoryNameEncryption"`
	// Rclone sets the remotes replica (the cloud bucket) and crypt (its
	// decrypted view) in a shell's environment.
	Rclone string `json:"rclone"`
}

// Collect reads the bundle of one cluster. A missing piece fails, naming the
// object and the key, never a value.
func Collect(ctx context.Context, cs kubernetes.Interface, cluster string, o Options) (Bundle, error) {
	if o.VaultNamespace == "" {
		o.VaultNamespace = "vault-operator-system"
	}
	if o.VaultSecret == "" {
		o.VaultSecret = "vault-unseal-keys"
	}
	now := time.Now
	if o.Now != nil {
		now = o.Now
	}
	b := Bundle{Generated: now().UTC().Format(time.RFC3339), Cluster: cluster, Replicas: []Replica{}}

	switch {
	case o.NoK3s:
	case o.K3sTokenFile != "":
		raw, err := os.ReadFile(o.K3sTokenFile)
		if err != nil {
			return Bundle{}, fmt.Errorf("the k3s token: %w", err)
		}
		token := strings.TrimSpace(string(raw))
		if token == "" {
			return Bundle{}, fmt.Errorf("the k3s token: %s is empty", o.K3sTokenFile)
		}
		b.K3s = &K3s{Token: token}
	default:
		return Bundle{}, fmt.Errorf("the k3s token: pass --k3s-token-file <file> (Ansible writes inventory/group_vars/.k3sToken), or --no-k3s for a cluster that is not self-hosted k3s")
	}

	vault, err := vaultKeys(ctx, cs, o.VaultNamespace, o.VaultSecret)
	if err != nil {
		return Bundle{}, err
	}
	b.Vault = vault

	cms, err := cs.CoreV1().ConfigMaps("").List(ctx, metav1.ListOptions{LabelSelector: ReplicaLabel + "=true"})
	if err != nil {
		return Bundle{}, fmt.Errorf("list the replicas' ConfigMaps: %w", err)
	}
	for i := range cms.Items {
		r, err := replicaOf(ctx, cs, &cms.Items[i])
		if err != nil {
			return Bundle{}, err
		}
		b.Replicas = append(b.Replicas, r)
	}
	sort.Slice(b.Replicas, func(i, j int) bool { return b.Replicas[i].Store < b.Replicas[j].Store })
	return b, nil
}

// Write prints the bundle as YAML.
func Write(b Bundle, w io.Writer) error {
	out, err := yaml.Marshal(b)
	if err != nil {
		return err
	}
	_, err = w.Write(out)
	return err
}

func secret(ctx context.Context, cs kubernetes.Interface, ns, name string) (*corev1.Secret, error) {
	s, err := cs.CoreV1().Secrets(ns).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, fmt.Errorf("Secret %s/%s does not exist", ns, name)
	}
	if err != nil {
		return nil, fmt.Errorf("read Secret %s/%s: %w", ns, name, err)
	}
	return s, nil
}

// vaultKeys reads bank-vaults' Secret: vault-root, and vault-unseal-0 to -<n>
// with no hole.
func vaultKeys(ctx context.Context, cs kubernetes.Interface, ns, name string) (Vault, error) {
	s, err := secret(ctx, cs, ns, name)
	if err != nil {
		return Vault{}, err
	}
	root := string(s.Data["vault-root"])
	if root == "" {
		return Vault{}, fmt.Errorf("Secret %s/%s has no vault-root", ns, name)
	}
	last := -1
	for k := range s.Data {
		if n, err := strconv.Atoi(strings.TrimPrefix(k, "vault-unseal-")); err == nil && strings.HasPrefix(k, "vault-unseal-") && n > last {
			last = n
		}
	}
	if last < 0 {
		return Vault{}, fmt.Errorf("Secret %s/%s has no vault-unseal-<n> key", ns, name)
	}
	keys := make([]string, 0, last+1)
	for i := 0; i <= last; i++ {
		k := "vault-unseal-" + strconv.Itoa(i)
		v := string(s.Data[k])
		if v == "" {
			return Vault{}, fmt.Errorf("Secret %s/%s has no %s, though it has vault-unseal-%d", ns, name, k, last)
		}
		keys = append(keys, v)
	}
	return Vault{RootToken: root, UnsealKeys: keys}, nil
}

func replicaOf(ctx context.Context, cs kubernetes.Interface, cm *corev1.ConfigMap) (Replica, error) {
	at := cm.Namespace + "/" + cm.Name
	setting := func(key string) (string, error) {
		v := cm.Data[key]
		if v == "" {
			return "", fmt.Errorf("ConfigMap %s has no %s", at, key)
		}
		return v, nil
	}
	r := Replica{Namespace: cm.Namespace, Provider: cm.Data["provider"]}
	var target, crypt string
	for _, f := range []struct {
		key string
		to  *string
	}{
		{"store", &r.Store}, {"endpoint", &r.Endpoint}, {"region", &r.Region}, {"bucket", &r.Bucket}, {"prefix", &r.Prefix},
		{"filenameEncryption", &r.FilenameEncryption}, {"directoryNameEncryption", &r.DirectoryNameEncryption},
		{"targetSecret", &target}, {"cryptSecret", &crypt},
	} {
		v, err := setting(f.key)
		if err != nil {
			return Replica{}, err
		}
		*f.to = v
	}
	if r.Provider == "" {
		r.Provider = "Other"
	}
	for _, f := range []struct {
		name, key string
		to        *string
	}{
		{target, "ACCESS_KEY", &r.AccessKey}, {target, "SECRET_KEY", &r.SecretKey},
		{crypt, "PASSWORD", &r.CryptPassword}, {crypt, "PASSWORD2", &r.CryptPassword2},
	} {
		s, err := secret(ctx, cs, cm.Namespace, f.name)
		if err != nil {
			return Replica{}, err
		}
		v := string(s.Data[f.key])
		if v == "" {
			return Replica{}, fmt.Errorf("Secret %s/%s has no %s", cm.Namespace, f.name, f.key)
		}
		*f.to = v
	}
	r.Rclone = rcloneSnippet(r)
	return r, nil
}

// rcloneSnippet sets the replica's remotes in a shell's environment; rclone
// wants the crypt passwords obscured, which it does itself from stdin.
func rcloneSnippet(r Replica) string {
	q := func(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
	lines := []string{
		"# Then: rclone lsf crypt: lists the replicated buckets; rclone copy crypt:<bucket> <store>:<bucket> restores one.",
		"export RCLONE_CONFIG_REPLICA_TYPE=s3",
		"export RCLONE_CONFIG_REPLICA_PROVIDER=" + q(r.Provider),
		"export RCLONE_CONFIG_REPLICA_ENDPOINT=" + q(r.Endpoint),
		"export RCLONE_CONFIG_REPLICA_REGION=" + q(r.Region),
		"export RCLONE_CONFIG_REPLICA_ACCESS_KEY_ID=" + q(r.AccessKey),
		"export RCLONE_CONFIG_REPLICA_SECRET_ACCESS_KEY=" + q(r.SecretKey),
		"export RCLONE_CONFIG_CRYPT_TYPE=crypt",
		"export RCLONE_CONFIG_CRYPT_REMOTE=replica:" + r.Bucket + "/" + r.Prefix,
		"export RCLONE_CONFIG_CRYPT_FILENAME_ENCRYPTION=" + q(r.FilenameEncryption),
		"export RCLONE_CONFIG_CRYPT_DIRECTORY_NAME_ENCRYPTION=" + q(r.DirectoryNameEncryption),
		"RCLONE_CONFIG_CRYPT_PASSWORD=$(printf %s " + q(r.CryptPassword) + " | rclone obscure -)",
		"RCLONE_CONFIG_CRYPT_PASSWORD2=$(printf %s " + q(r.CryptPassword2) + " | rclone obscure -)",
		"export RCLONE_CONFIG_CRYPT_PASSWORD RCLONE_CONFIG_CRYPT_PASSWORD2",
	}
	return strings.Join(lines, "\n") + "\n"
}
