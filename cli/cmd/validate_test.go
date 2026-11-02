package cmd

import (
	"strings"
	"testing"
)

const good = `
apiVersion: ohmlab.fr/v1alpha1
kind: VaultSeed
metadata: {name: keycloak, namespace: vault-operator-system}
spec:
  path: homelab/platforms/production/core/keycloak
  data:
    admin: {password: "<random:24>", hashed: "<bcrypt:admin.password>"}
---
apiVersion: v1
kind: ConfigMap
metadata: {name: ignored}
---
apiVersion: ohmlab.fr/v1alpha1
kind: VaultSeed
metadata: {name: grafana, namespace: vault-operator-system}
spec:
  path: homelab/platforms/production/core/grafana
  data:
    secret: "<ref:homelab/platforms/production/core/keycloak#admin.password>"
`

func validate(t *testing.T, in string) ([]string, int) {
	t.Helper()
	problems, n, err := validateInputs([]string{"-"}, strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	return problems, n
}

func TestValidateAcceptsAWholeRenderedChart(t *testing.T) {
	if problems, n := validate(t, good); len(problems) != 0 || n != 2 {
		t.Errorf("problems=%v n=%d", problems, n)
	}
}

func TestValidateReportsEachProblemByObjectAndKey(t *testing.T) {
	in := `
apiVersion: ohmlab.fr/v1alpha1
kind: VaultSeed
metadata: {name: bad, namespace: ns}
spec:
  path: homelab/platforms/production/core/bad
  data: {k: "<hex:7>"}
`
	problems, _ := validate(t, in)
	if len(problems) != 1 || !strings.Contains(problems[0], "VaultSeed ns/bad") || !strings.Contains(problems[0], "k: <hex:7>") {
		t.Errorf("problems = %v", problems)
	}
}

func TestValidateChecksTheSetAsAWhole(t *testing.T) {
	obj := func(name, path, data string) string {
		return "apiVersion: ohmlab.fr/v1alpha1\nkind: VaultSeed\nmetadata: {name: " + name + ", namespace: ns}\nspec:\n  path: " + path + "\n  data: " + data + "\n---\n"
	}
	tests := []struct {
		name, in, want string
	}{
		{"same path", obj("a", "homelab/p/c/x", `{k: v}`) + obj("b", "homelab/p/c/x", `{k: v}`), "already declared"},
		{"same name", obj("a", "homelab/p/c/x", `{k: v}`) + obj("a", "homelab/p/c/y", `{k: v}`), "name is already used"},
		{"cycle", obj("a", "homelab/p/c/a", `{k: "<ref:homelab/p/c/b#k>"}`) + obj("b", "homelab/p/c/b", `{k: "<ref:homelab/p/c/a#k>"}`), "references form a cycle"},
		{"self reference", obj("a", "homelab/p/c/a", `{k: "<ref:homelab/p/c/a#other>"}`), "references form a cycle"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			problems, _ := validate(t, tt.in)
			if len(problems) == 0 || !strings.Contains(strings.Join(problems, "\n"), tt.want) {
				t.Errorf("problems = %v, want %q", problems, tt.want)
			}
		})
	}
}

func TestValidateRefusesUnknownFieldsAndKinds(t *testing.T) {
	in := `
apiVersion: ohmlab.fr/v1alpha1
kind: VaultSeed
metadata: {name: a, namespace: ns}
spec:
  path: homelab/p/c/x
  data: {k: v}
  typo: true
---
apiVersion: ohmlab.fr/v1alpha1
kind: HarborConfiguration
metadata: {name: h}
`
	problems, _ := validate(t, in)
	joined := strings.Join(problems, "\n")
	if !strings.Contains(joined, "unknown field") || !strings.Contains(joined, "HarborConfiguration is not a kind") {
		t.Errorf("problems = %v", problems)
	}
}

func TestValidateNeverPrintsAValue(t *testing.T) {
	in := `
apiVersion: ohmlab.fr/v1alpha1
kind: VaultSeed
metadata: {name: a, namespace: ns}
spec:
  path: homelab/p/c/x
  data: {plain: "TOP-SECRET-VALUE", bad: "<random:0>"}
`
	problems, _ := validate(t, in)
	if len(problems) == 0 || strings.Contains(strings.Join(problems, "\n"), "TOP-SECRET-VALUE") {
		t.Errorf("problems = %v", problems)
	}
}

func TestValidateRefusesADeletionPolicyTheCRDWouldRefuse(t *testing.T) {
	in := `
apiVersion: ohmlab.fr/v1alpha1
kind: VaultSeed
metadata: {name: typo, namespace: ns}
spec:
  path: homelab/platforms/production/core/typo
  deletionPolicy: delete
  data: {a: "<random:8>"}
`
	problems, _ := validate(t, in)
	if len(problems) != 1 || !strings.Contains(problems[0], "deletionPolicy") || !strings.Contains(problems[0], `"delete"`) {
		t.Errorf("want one problem naming the field and the value: %v", problems)
	}
	ok := strings.Replace(in, "deletionPolicy: delete", "deletionPolicy: Delete", 1)
	if problems, _ := validate(t, ok); len(problems) != 0 {
		t.Errorf("Delete is valid: %v", problems)
	}
}
