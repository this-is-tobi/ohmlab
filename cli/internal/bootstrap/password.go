package bootstrap

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"
)

// passwordEnv is where run.sh read the admin password from.
const passwordEnv = "ARGOCD_ADMIN_PASSWORD"

// passwordCost is run.sh's (htpasswd -C 10).
const passwordCost = 10

// AdminPassword returns the Argo CD admin password to set, and where it came
// from: file ("-" is standard input), else $ARGOCD_ADMIN_PASSWORD. With
// neither, both are empty: Argo CD generates a password. One trailing newline
// is dropped, as a file written with echo ends with one.
func AdminPassword(file string, getenv func(string) string, stdin io.Reader) (password, source string, err error) {
	var raw string
	switch {
	case file == "-":
		b, err := io.ReadAll(stdin)
		if err != nil {
			return "", "", fmt.Errorf("read the admin password from standard input: %w", err)
		}
		raw, source = string(b), "standard input"
	case file != "":
		b, err := os.ReadFile(file)
		if err != nil {
			return "", "", fmt.Errorf("read the admin password: %w", err)
		}
		raw, source = string(b), "file "+file
	case getenv(passwordEnv) != "":
		raw, source = getenv(passwordEnv), "$"+passwordEnv
	default:
		return "", "", nil
	}
	password = strings.TrimSuffix(raw, "\n")
	if password != raw {
		password = strings.TrimSuffix(password, "\r")
	}
	if password == "" {
		return "", "", fmt.Errorf("the admin password from %s is empty", source)
	}
	return password, source, nil
}

// AdminHash is an admin password as argocd-secret holds it: its bcrypt hash,
// and when it was set (empty: the chart sets now).
type AdminHash struct{ Hash, Mtime string }

// HashAdminPassword hashes a given admin password.
func HashAdminPassword(password string) (AdminHash, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), passwordCost)
	if err != nil {
		return AdminHash{}, fmt.Errorf("hash the admin password: %w", err)
	}
	return AdminHash{Hash: string(hash)}, nil
}

// Accepts reports whether password is the one h hashes.
func (h AdminHash) Accepts(password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(h.Hash), []byte(password)) == nil
}

// WriteAdminPasswordValues writes into dir the Helm values that set the admin
// password's hash, and its mtime when known, in a new file only its owner can
// read, and returns its path. The hash goes through a file because a --set
// value shows in the process list for the whole helm run.
func WriteAdminPasswordValues(dir string, h AdminHash) (string, error) {
	secret := map[string]any{"argocdServerAdminPassword": h.Hash}
	if h.Mtime != "" {
		secret["argocdServerAdminPasswordMtime"] = h.Mtime
	}
	data, err := yaml.Marshal(map[string]any{"argo-cd": map[string]any{"configs": map[string]any{"secret": secret}}})
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "admin-password.yaml")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return "", err
	}
	return path, f.Close()
}
