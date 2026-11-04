package internal_test

import (
	"os/exec"
	"strings"
	"testing"
)

// The targets share a process in some topologies, so the code of one must not
// be able to reach the credentials of another: the Harbor and SonarQube
// packages may not import anything of Vault's, and Vault's may not import
// theirs. The module path is stable (it is the module's, see go.mod).
const module = "github.com/this-is-tobi/ohmlab/cli/"

var vaultSide = []string{
	"internal/vault", "internal/seed", "internal/secretgen",
	"internal/controller/vaultseed",
}

var otherSide = []string{"internal/harbor", "internal/sonarqube"}

func deps(t *testing.T, pkg string) []string {
	t.Helper()
	out, err := exec.Command("go", "list", "-deps", "./"+strings.TrimPrefix(pkg, module)).CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps %s: %v\n%s", pkg, err, out)
	}
	return strings.Fields(string(out))
}

func imports(t *testing.T, from []string, forbidden []string) {
	t.Helper()
	for _, pkg := range from {
		for _, dep := range deps(t, "../"+pkg) {
			for _, bad := range forbidden {
				if dep == module+bad || strings.HasPrefix(dep, module+bad+"/") {
					t.Errorf("%s imports %s", pkg, bad)
				}
			}
		}
	}
}

func TestHarborAndSonarqubeNeverImportVault(t *testing.T) {
	imports(t, otherSide, vaultSide)
}

func TestVaultNeverImportsHarborOrSonarqube(t *testing.T) {
	imports(t, vaultSide, otherSide)
}

// The image's binary runs in the Jobs, next to their targets' credentials:
// no Kubernetes client library in it (its commands use internal/k8s).
func TestTheJobBinaryLinksNoKubernetesClient(t *testing.T) {
	notLinked(t, "../cmd/job", "k8s.io/client-go", "sigs.k8s.io/controller-runtime")
}

// The workstation CLI may use client-go (credential plugins, port-forward),
// never the controller stack.
func TestTheCLIDoesNotLinkTheControllerStack(t *testing.T) {
	notLinked(t, "../.", "sigs.k8s.io/controller-runtime")
}

func notLinked(t *testing.T, main string, banned ...string) {
	t.Helper()
	for _, dep := range deps(t, main) {
		for _, bad := range banned {
			if strings.HasPrefix(dep, bad) {
				t.Errorf("%s links %s", main, dep)
			}
		}
	}
}
