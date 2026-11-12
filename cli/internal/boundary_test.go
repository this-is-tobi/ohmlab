package internal_test

import (
	"os/exec"
	"strings"
	"testing"
)

// The targets share a process in some topologies, so the code of one must not
// be able to reach the credentials of another: the other targets' packages may
// not import anything of Vault's or the storage target's, and Vault's may not
// import theirs. The storage target may use Vault's client, since it writes
// the claims' records; Vault's code may not reach a store's root keys. The
// module path is stable (it is the module's, see go.mod).
const module = "github.com/this-is-tobi/ohmlab/cli/"

var vaultSide = []string{
	"internal/vault", "internal/seed", "internal/secretgen",
	"internal/controller/vaultseed",
}

var storageSide = []string{"internal/rustfs", "internal/controller/storage"}

var otherSide = []string{"internal/harbor", "internal/sonarqube", "internal/controller/certificates"}

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

func TestOtherTargetsNeverImportVault(t *testing.T) {
	imports(t, otherSide, vaultSide)
}

func TestOtherTargetsNeverImportStorage(t *testing.T) {
	imports(t, otherSide, storageSide)
}

func TestVaultNeverImportsTheOtherTargets(t *testing.T) {
	imports(t, vaultSide, append(append([]string{}, otherSide...), storageSide...))
}

// The CLI, in the release archives and the image alike, may use client-go
// (credential plugins, port-forward); the controller-runtime graph stays in
// the controller's binary.
func TestTheCLIDoesNotLinkTheControllerStack(t *testing.T) {
	for _, dep := range deps(t, "../.") {
		if strings.HasPrefix(dep, "sigs.k8s.io/controller-runtime") {
			t.Errorf("the ohmlab CLI links %s", dep)
		}
	}
}
