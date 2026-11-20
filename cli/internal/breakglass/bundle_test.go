package breakglass

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/yaml"
)

var t0 = time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)

func unsealSecret(keys ...string) *corev1.Secret {
	data := map[string][]byte{"vault-root": []byte("root-token"), "vault-test": []byte("x")}
	for _, k := range keys {
		data[k] = []byte("key-" + strings.TrimPrefix(k, "vault-unseal-"))
	}
	return &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "vault-operator-system", Name: "vault-unseal-keys"}, Data: data}
}

// replica is a store's replica ConfigMap and its two Secrets, as the rustfs chart renders them.
func replica(ns, store string) []runtime.Object {
	return []runtime.Object{
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "rustfs-replica", Labels: map[string]string{"ohmlab.fr/replica": "true"}},
			Data: map[string]string{
				"store": store, "provider": "Scaleway", "endpoint": "https://s3.fr-par.scw.cloud", "region": "fr-par",
				"bucket": "cloud-" + store, "prefix": "homelab", "storageClass": "", "filenameEncryption": "standard",
				"directoryNameEncryption": "true", "targetSecret": "rustfs-replica-target", "cryptSecret": "rustfs-replica-crypt",
				"replicate.sh": "#!/bin/sh",
			},
		},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "rustfs-replica-target"},
			Data: map[string][]byte{"ACCESS_KEY": []byte("AK-" + store), "SECRET_KEY": []byte("SK-" + store)}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "rustfs-replica-crypt"},
			Data: map[string][]byte{"PASSWORD": []byte("P1-" + store), "PASSWORD2": []byte("P2-" + store)}},
	}
}

func noK3s() Options { return Options{NoK3s: true, Now: func() time.Time { return t0 }} }

func TestCollectReadsVaultAndEachReplica(t *testing.T) {
	var twelve []string
	for i := range 12 {
		twelve = append(twelve, "vault-unseal-"+strconv.Itoa(i))
	}
	b, err := Collect(t.Context(), fake.NewClientset(unsealSecret(twelve...)), "homelab", noK3s())
	if err != nil {
		t.Fatal(err)
	}
	if b.Cluster != "homelab" || b.Generated != "2026-11-20T12:00:00Z" {
		t.Errorf("cluster %q, generated %q", b.Cluster, b.Generated)
	}
	if b.Vault.RootToken != "root-token" {
		t.Error("the root token is not the Secret's")
	}
	if got := strings.Join(b.Vault.UnsealKeys[9:], " "); got != "key-9 key-10 key-11" {
		t.Errorf("unseal keys end %q: want them in numeric order", got)
	}
	_, err = Collect(t.Context(), fake.NewClientset(unsealSecret("vault-unseal-0", "vault-unseal-2")), "c", noK3s())
	if err == nil || !strings.Contains(err.Error(), "vault-unseal-1") {
		t.Errorf("a hole in the unseal keys must name the missing key, got %v", err)
	}
	b, err = Collect(t.Context(), fake.NewClientset(append([]runtime.Object{unsealSecret("vault-unseal-0", "vault-unseal-1", "vault-unseal-2")},
		append(replica("rustfs-z", "zeta"), replica("rustfs-backups", "rustfs-backups")...)...)...), "homelab", noK3s())
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(b.Vault.UnsealKeys, " "); got != "key-0 key-1 key-2" {
		t.Errorf("unseal keys = %q, want them in order", got)
	}
	if len(b.Replicas) != 2 || b.Replicas[0].Store != "rustfs-backups" || b.Replicas[1].Store != "zeta" {
		t.Fatalf("replicas = %+v, want two sorted by store", b.Replicas)
	}
	r := b.Replicas[0]
	want := Replica{Store: "rustfs-backups", Namespace: "rustfs-backups", Provider: "Scaleway", Endpoint: "https://s3.fr-par.scw.cloud",
		Region: "fr-par", Bucket: "cloud-rustfs-backups", Prefix: "homelab", AccessKey: "AK-rustfs-backups", SecretKey: "SK-rustfs-backups",
		CryptPassword: "P1-rustfs-backups", CryptPassword2: "P2-rustfs-backups", FilenameEncryption: "standard", DirectoryNameEncryption: "true"}
	r.Rclone = ""
	if r != want {
		t.Errorf("replica = %+v\nwant      %+v", r, want)
	}
	for _, s := range []string{"RCLONE_CONFIG_CRYPT_REMOTE=replica:cloud-rustfs-backups/homelab", "RCLONE_CONFIG_REPLICA_PROVIDER='Scaleway'", "rclone obscure -", "'P1-rustfs-backups'"} {
		if !strings.Contains(b.Replicas[0].Rclone, s) {
			t.Errorf("the rclone snippet lacks %q:\n%s", s, b.Replicas[0].Rclone)
		}
	}
}

func TestAMissingPieceFailsAndNamesIt(t *testing.T) {
	full := func() []runtime.Object {
		return append([]runtime.Object{unsealSecret("vault-unseal-0")}, replica("rustfs-backups", "rustfs-backups")...)
	}
	for _, tt := range []struct {
		name string
		edit func([]runtime.Object) []runtime.Object
		want []string
	}{
		{"no unseal Secret", func(o []runtime.Object) []runtime.Object { return o[1:] }, []string{"vault-operator-system/vault-unseal-keys"}},
		{"no root token", func(o []runtime.Object) []runtime.Object { delete(o[0].(*corev1.Secret).Data, "vault-root"); return o }, []string{"vault-root"}},
		{"no unseal key", func(o []runtime.Object) []runtime.Object {
			delete(o[0].(*corev1.Secret).Data, "vault-unseal-0")
			return o
		}, []string{"vault-unseal"}},
		{"a crypt password missing", func(o []runtime.Object) []runtime.Object { delete(o[3].(*corev1.Secret).Data, "PASSWORD2"); return o }, []string{"rustfs-backups/rustfs-replica-crypt", "PASSWORD2"}},
		{"no target Secret", func(o []runtime.Object) []runtime.Object { return append(o[:2], o[3]) }, []string{"rustfs-backups/rustfs-replica-target"}},
		{"a setting missing", func(o []runtime.Object) []runtime.Object { delete(o[1].(*corev1.ConfigMap).Data, "bucket"); return o }, []string{"rustfs-backups/rustfs-replica", "bucket"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Collect(t.Context(), fake.NewClientset(tt.edit(full())...), "c", noK3s())
			if err == nil {
				t.Fatal("want an error")
			}
			for _, w := range tt.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("the error %q does not name %s", err, w)
				}
			}
			for _, secret := range []string{"root-token", "key-0", "AK-", "SK-", "P1-", "P2-"} {
				if strings.Contains(err.Error(), secret) {
					t.Errorf("the error carries a value: %q", err)
				}
			}
		})
	}
}

func TestTheK3sTokenIsRequiredOrExplicitlyLeftOut(t *testing.T) {
	cs := func() *fake.Clientset { return fake.NewClientset(unsealSecret("vault-unseal-0")) }
	if _, err := Collect(t.Context(), cs(), "c", Options{}); err == nil ||
		!strings.Contains(err.Error(), "--k3s-token-file") || !strings.Contains(err.Error(), "--no-k3s") {
		t.Errorf("neither flag must fail, naming both: %v", err)
	}
	b, err := Collect(t.Context(), cs(), "c", noK3s())
	if err != nil || b.K3s != nil {
		t.Errorf("--no-k3s leaves the section out: %+v %v", b.K3s, err)
	}
	file := filepath.Join(t.TempDir(), ".k3sToken")
	if err := os.WriteFile(file, []byte("K10abc::server:secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err = Collect(t.Context(), cs(), "c", Options{K3sTokenFile: file})
	if err != nil || b.K3s == nil || b.K3s.Token != "K10abc::server:secret" {
		t.Errorf("the token file, trimmed: %+v %v", b.K3s, err)
	}
	if _, err := Collect(t.Context(), cs(), "c", Options{K3sTokenFile: filepath.Join(t.TempDir(), "none")}); err == nil {
		t.Error("a token file that does not exist must fail")
	}
}

func TestWriteIsYAMLWithEverySection(t *testing.T) {
	b, err := Collect(t.Context(), fake.NewClientset(append([]runtime.Object{unsealSecret("vault-unseal-0")}, replica("rustfs-backups", "rustfs-backups")...)...), "homelab", noK3s())
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := Write(b, &buf); err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if err := yaml.Unmarshal(buf.Bytes(), &back); err != nil {
		t.Fatalf("not YAML: %v", err)
	}
	for _, k := range []string{"generated", "cluster", "vault", "replicas"} {
		if _, ok := back[k]; !ok {
			t.Errorf("no %s section", k)
		}
	}
	if _, ok := back["k3s"]; ok {
		t.Error("a k3s section with --no-k3s")
	}
}
