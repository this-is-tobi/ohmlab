package vault

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"
	vaultclient "github.com/this-is-tobi/ohmlab/utils/internal/vault"
)

var secretCmd = &cobra.Command{
	Use:   "secret",
	Short: "Initialize secrets in Vault from a JSON config",
	Long: `Initialize secrets in Vault using a declarative JSON configuration.

Reads a JSON file containing secret entries with template placeholders,
generates values, and writes them to Vault using deep-merge semantics
(existing values are preserved, only missing keys are added).

Template placeholders:
  <random:N>          Cryptographic random alphanumeric string of length N
  <hex:N>             Cryptographic random hex string of length N (N must be even)
  <uuid>              Random UUID v4
  <age:secret>        Age X25519 secret key (one per entry)
  <age:public>        Age X25519 public key (paired with secret)
  <ref:path#key.path> Reference a value from another Vault secret

Authentication methods (--auth-method):
  kubernetes  SA token validated by K8s API (same-cluster only)
  jwt         SA token validated via JWKS (recommended for multi-cluster)
  approle     role_id + secret_id credentials
  token       Direct Vault token

Config format (JSON array):
  [
    {
      "path": "mount/secret/path",
      "data": {
        "admin": {"username": "admin", "password": "<random:24>"},
        "keycloak": {"clientSecret": "<ref:mount/other/path#keycloak.clientSecret>"}
      }
    }
  ]`,
	RunE: runSecret,
}

var (
	vaultAddr       string
	vaultCACert     string
	vaultSkipTLS    bool
	vaultAuthMethod string
	vaultAuthMount  string
	vaultRole       string
	vaultToken      string
	vaultRoleID     string
	vaultSecretID   string
	initConfigFile  string
	initDryRun      bool
	initForceRotate bool
)

func init() {
	Cmd.AddCommand(secretCmd)

	f := secretCmd.Flags()
	f.StringVar(&vaultAddr, "vault-addr", envOrDefault("VAULT_ADDR", ""), "Vault server address")
	f.StringVar(&vaultCACert, "ca-cert", envOrDefault("VAULT_CACERT", ""), "path to Vault CA certificate")
	f.BoolVar(&vaultSkipTLS, "skip-tls-verify", os.Getenv("VAULT_SKIP_VERIFY") == "true", "skip TLS verification")
	f.StringVar(&vaultAuthMethod, "auth-method", envOrDefault("VAULT_AUTH_METHOD", "kubernetes"), "auth method: kubernetes, jwt, approle, token")
	f.StringVar(&vaultAuthMount, "auth-mount", envOrDefault("VAULT_AUTH_MOUNT", ""), "auth mount path (defaults to method name)")
	f.StringVar(&vaultRole, "role", envOrDefault("VAULT_ROLE", "vault-post-config"), "Vault auth role name")
	f.StringVar(&vaultToken, "token", "", "Vault token (for token auth, or use VAULT_TOKEN env)")
	f.StringVar(&vaultRoleID, "role-id", envOrDefault("VAULT_ROLE_ID", ""), "AppRole role ID")
	f.StringVar(&vaultSecretID, "secret-id", envOrDefault("VAULT_SECRET_ID", ""), "AppRole secret ID")
	f.StringVar(&initConfigFile, "config", envOrDefault("SECRETS_FILE", ""), "path to secrets config JSON file")
	f.BoolVar(&initDryRun, "dry-run", false, "print what would be done without writing to Vault")
	f.BoolVar(&initForceRotate, "force-rotate", false, "overwrite existing values (rotation mode)")

	_ = secretCmd.MarkFlagRequired("config")
}

func runSecret(cmd *cobra.Command, args []string) error {
	slog.Info("vault secret initialization starting",
		"vault_addr", vaultAddr,
		"auth_method", vaultAuthMethod,
		"dry_run", initDryRun,
		"force_rotate", initForceRotate,
	)

	if vaultAddr == "" {
		return fmt.Errorf("--vault-addr or VAULT_ADDR is required")
	}

	// Load config
	entries, err := vaultclient.LoadConfig(initConfigFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	slog.Info("loaded secret entries", "count", len(entries))

	// Create Vault client
	client, err := vaultclient.NewClient(vaultclient.ClientConfig{
		Address:       vaultAddr,
		CACertPath:    vaultCACert,
		SkipTLSVerify: vaultSkipTLS,
	})
	if err != nil {
		return fmt.Errorf("create vault client: %w", err)
	}

	// Authenticate
	authParams := buildAuthParams()
	if err := client.Authenticate(vaultclient.AuthMethod(vaultAuthMethod), vaultAuthMount, authParams); err != nil {
		return fmt.Errorf("vault auth (%s): %w", vaultAuthMethod, err)
	}

	// Run initialization
	results := vaultclient.RunInit(client, entries, vaultclient.InitOptions{
		DryRun:      initDryRun,
		ForceRotate: initForceRotate,
		ConfigFile:  initConfigFile,
	})

	// Report summary
	var created, updated, unchanged, failed int
	for _, r := range results {
		switch {
		case r.Error != nil:
			failed++
		case r.Action == "created":
			created++
		case r.Action == "updated" || r.Action == "rotated":
			updated++
		default:
			unchanged++
		}
	}

	slog.Info("initialization complete",
		"total", len(results),
		"created", created,
		"updated", updated,
		"unchanged", unchanged,
		"failed", failed,
	)

	if failed > 0 {
		return fmt.Errorf("%d secret(s) failed to initialize", failed)
	}

	return nil
}

func buildAuthParams() map[string]string {
	params := map[string]string{
		"role": vaultRole,
	}
	switch vaultclient.AuthMethod(vaultAuthMethod) {
	case vaultclient.AuthAppRole:
		params["role_id"] = vaultRoleID
		params["secret_id"] = vaultSecretID
	case vaultclient.AuthToken:
		params["token"] = vaultToken
	}
	return params
}
