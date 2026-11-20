package storage

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/this-is-tobi/ohmlab/cli/internal/breakglass"
)

func vaultOnly() *fake.Clientset {
	return fake.NewClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "vault-operator-system", Name: "vault-unseal-keys"},
		Data:       map[string][]byte{"vault-root": []byte("root-token"), "vault-unseal-0": []byte("key-0")},
	})
}

var opts = breakglass.Options{NoK3s: true, Now: func() time.Time { return time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC) }}

func TestNoReplicaWarns(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := breakGlass(t.Context(), vaultOnly(), "homelab", opts, "", &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "replicas: []") {
		t.Errorf("the bundle must list no replica:\n%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "no replica") {
		t.Errorf("want a warning on stderr, got %q", stderr.String())
	}
}

func TestOutputIsANewPrivateFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "bundle.yaml")
	var stdout, stderr bytes.Buffer
	if err := breakGlass(t.Context(), vaultOnly(), "homelab", opts, file, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if stdout.Len() != 0 {
		t.Errorf("--output wrote to stdout too: %d bytes", stdout.Len())
	}
	st, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, want 0600", st.Mode().Perm())
	}
	if b, _ := os.ReadFile(file); !strings.Contains(string(b), "rootToken") {
		t.Error("the file holds no bundle")
	}
	if err := breakGlass(t.Context(), vaultOnly(), "homelab", opts, file, &stdout, &stderr); err == nil {
		t.Error("an existing file must be refused")
	}
}
