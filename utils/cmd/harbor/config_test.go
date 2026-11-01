package harbor

import (
	"strings"
	"testing"
)

// The Keycloak domain and realm come from flags or env only: no chart
// creates a Keycloak Secret to read them from, and Harbor, a tenant app,
// signs in through the instance's realm, which no shared Secret carries.
func TestKeycloakSettingsHaveNoSecretFallback(t *testing.T) {
	for _, name := range []string{"keycloak-secret", "keycloak-namespace"} {
		if configCmd.Flags().Lookup(name) != nil {
			t.Errorf("flag --%s still exists", name)
		}
	}
	if !strings.Contains(configCmd.Long, "KEYCLOAK_REALM") {
		t.Error("the help does not name KEYCLOAK_REALM")
	}
	for _, env := range []string{"KEYCLOAK_SECRET_NAME", "KEYCLOAK_NAMESPACE"} {
		if strings.Contains(configCmd.Long, env) {
			t.Errorf("the help still names %s", env)
		}
	}
}
