package bootstrap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"
)

func env(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

func TestAdminPasswordSources(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "pw")
	write(t, file, "from-file\n")
	withEnv := env(map[string]string{"ARGOCD_ADMIN_PASSWORD": "from-env"})
	for _, tc := range []struct {
		name, file, stdin, want, source string
		getenv                          func(string) string
	}{
		{name: "the file beats the environment", file: file, getenv: withEnv, want: "from-file", source: "file " + file},
		{name: "- reads standard input", file: "-", stdin: "from-stdin\r\n", getenv: withEnv, want: "from-stdin", source: "standard input"},
		{name: "the environment alone", getenv: withEnv, want: "from-env", source: "$ARGOCD_ADMIN_PASSWORD"},
		{name: "neither", getenv: env(nil)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pw, source, err := AdminPassword(tc.file, tc.getenv, strings.NewReader(tc.stdin))
			if err != nil || pw != tc.want || source != tc.source {
				t.Errorf("got %q from %q, %v; want %q from %q", pw, source, err, tc.want, tc.source)
			}
		})
	}
	empty := filepath.Join(dir, "empty")
	write(t, empty, "\n")
	if _, _, err := AdminPassword(empty, env(nil), strings.NewReader("")); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Errorf("got %v, want an error saying the password is empty", err)
	}
}

func TestAdminPasswordValuesHoldAVerifiableHash(t *testing.T) {
	for _, pw := range []string{"s3cret", `with "quotes", $dollars and: colons`} {
		dir := t.TempDir()
		path, err := WriteAdminPasswordValues(dir, pw)
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join(dir, "admin-password.yaml"); path != want {
			t.Errorf("path %s, want %s", path, want)
		}
		st, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != 0o600 {
			t.Errorf("mode %v, want 0600", st.Mode().Perm())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), pw) {
			t.Errorf("the values file holds the password %q", pw)
		}
		var v struct {
			ArgoCD struct {
				Configs struct {
					Secret struct {
						Hash string `yaml:"argocdServerAdminPassword"`
					} `yaml:"secret"`
				} `yaml:"configs"`
			} `yaml:"argo-cd"`
		}
		if err := yaml.Unmarshal(data, &v); err != nil {
			t.Fatal(err)
		}
		h := v.ArgoCD.Configs.Secret.Hash
		if !strings.HasPrefix(h, "$2a$10$") || bcrypt.CompareHashAndPassword([]byte(h), []byte(pw)) != nil {
			t.Errorf("hash %q is not a cost 10 bcrypt of %q", h, pw)
		}
	}
}
