package vault

import (
	"github.com/spf13/cobra"
)

// Cmd is the root vault command
var Cmd = &cobra.Command{
	Use:   "vault",
	Short: "Vault secret management commands",
	Long: `Commands for managing secrets in HashiCorp Vault.

Supports multiple authentication methods for multi-cluster deployments:
  - kubernetes: validates SA token via K8s API (same-cluster only)
  - jwt: validates SA token via JWKS (cross-cluster, recommended)
  - approle: uses role_id + secret_id credentials
  - token: direct Vault token (development/manual use)

JWT vs AppRole comparison for multi-cluster:
  JWT is recommended because it uses existing K8s SA tokens (no additional
  credentials to manage), validates via public JWKS endpoint (no cross-cluster
  network dependency to the K8s API), and rotates automatically with SA tokens.
  AppRole requires distributing and rotating secret_id credentials per namespace,
  adding operational complexity without security benefit over JWT.`,
}

func init() {
	// Subcommands are registered by their respective files
}
