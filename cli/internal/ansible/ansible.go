// Package ansible is how the CLI runs Ansible: with the product's
// configuration, which finds the roles, and the workspace's vault password.
package ansible

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/this-is-tobi/ohmlab/cli/internal/config"
)

// Config is the product's ansible.cfg.
func Config(product string) string {
	return filepath.Join(product, "ansible", "ansible.cfg")
}

// Check reports what would make Ansible fail with nothing pointing here. It
// skips an ANSIBLE_CONFIG that does not exist and takes another
// configuration. And the product's configuration names a .vault_password in
// the product checkout, which Ansible requires when the workspace gives none.
func Check(product, vaultPasswordFile string) error {
	if st, err := os.Stat(Config(product)); err != nil || !st.Mode().IsRegular() {
		return fmt.Errorf("no ansible/ansible.cfg in %s: pass --product with a checkout of ohmlab, or set product in %s", product, config.DefaultPath)
	}
	if vaultPasswordFile == "" {
		return errors.New("no Ansible vault password file: pass --vault-password-file, set $ANSIBLE_VAULT_PASSWORD_FILE or vaultPasswordFile in " + config.DefaultPath + ", or create ansible/.vault_password in the workspace")
	}
	return nil
}

// Env points Ansible at the product's configuration and the vault password
// file. ANSIBLE_VAULT_PASSWORD_FILE replaces the file the configuration
// names; --vault-password-file would add to it, and Ansible would warn that
// one is missing.
func Env(product, vaultPasswordFile string) []string {
	env := []string{"ANSIBLE_CONFIG=" + Config(product)}
	if vaultPasswordFile != "" {
		env = append(env, "ANSIBLE_VAULT_PASSWORD_FILE="+vaultPasswordFile)
	}
	return env
}
