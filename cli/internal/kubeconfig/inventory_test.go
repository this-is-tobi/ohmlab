package kubeconfig

import (
	"os"
	"path/filepath"
	"testing"
)

// inventory writes hosts.yml (and group_vars/vault.yml when vault) into a
// new inventory directory.
func inventory(t *testing.T, hosts string, vault bool) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hosts.yml"), []byte(hosts), 0o600); err != nil {
		t.Fatal(err)
	}
	if vault {
		if err := os.MkdirAll(filepath.Join(dir, "group_vars"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "group_vars", "vault.yml"), []byte("x: y\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestServerAddressPrefersTheGateway(t *testing.T) {
	if got, err := ServerAddress("testdata/inventory"); err != nil || got != "192.168.1.100" {
		t.Errorf("got %q, %v; want the gateway 192.168.1.100", got, err)
	}
}

func TestServerAddressWithoutGatewayTakesTheFirstMaster(t *testing.T) {
	inv := inventory(t, `all:
  children:
    k3s:
      children:
        workers:
          hosts:
            w1: {ansible_host: 10.0.0.9}
        masters:
          hosts:
            m2: {ansible_host: 10.0.0.2}
            m1: {ansible_host: 10.0.0.1}
`, false)
	if got, err := ServerAddress(inv); err != nil || got != "10.0.0.2" {
		t.Errorf("got %q, %v; want the first master 10.0.0.2", got, err)
	}
}

func TestServerAddressUsesTheHostName(t *testing.T) {
	inv := inventory(t, "masters:\n  hosts:\n    node1.lan:\n", false)
	if got, err := ServerAddress(inv); err != nil || got != "node1.lan" {
		t.Errorf("got %q, %v; want the host name", got, err)
	}
	if _, err := ServerAddress(inventory(t, "workers:\n  hosts:\n    w1:\n", false)); err == nil {
		t.Error("an inventory with no gateway and no master gave an address")
	}
}
