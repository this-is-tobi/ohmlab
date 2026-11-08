package ansible

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestEnv(t *testing.T) {
	if got, want := Env("/p", "/s/pw"), []string{"ANSIBLE_CONFIG=/p/ansible/ansible.cfg", "ANSIBLE_VAULT_PASSWORD_FILE=/s/pw"}; !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := Env("/p", ""), []string{"ANSIBLE_CONFIG=/p/ansible/ansible.cfg"}; !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCheck(t *testing.T) {
	product, notAFile := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(product, "ansible"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Config(product), []byte("[defaults]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(Config(notAFile), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, product, password string
		// want are substrings of the error; none means no error.
		want []string
	}{
		{"both", product, "/s/pw", nil},
		{"no ansible.cfg", t.TempDir(), "/s/pw", []string{"pass --product"}},
		{"ansible.cfg is a directory", notAFile, "/s/pw", []string{"pass --product"}},
		{"no password file", product, "", []string{"--vault-password-file", "ANSIBLE_VAULT_PASSWORD_FILE", "vaultPasswordFile"}},
	} {
		err := Check(c.product, c.password)
		if len(c.want) == 0 {
			if err != nil {
				t.Errorf("%s: %v", c.name, err)
			}
			continue
		}
		if err == nil {
			t.Errorf("%s: no error", c.name)
			continue
		}
		for _, w := range c.want {
			if !strings.Contains(err.Error(), w) {
				t.Errorf("%s: %q lacks %q", c.name, err, w)
			}
		}
	}
}
