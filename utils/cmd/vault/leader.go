package vault

import (
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"
	vaultclient "github.com/this-is-tobi/ohmlab/utils/internal/vault"
)

var leaderCmd = &cobra.Command{
	Use:   "leader",
	Short: "Discover the active Vault leader node address",
	Long: `Discover and print the address of the active Vault leader node.

This command queries /v1/sys/leader (no authentication required) to determine
which Vault node is currently the active leader in an HA cluster.

Useful for targeting requests to the leader node directly, avoiding
rate-limiting (429) responses from standby nodes.

Example:
  ohmlab vault leader --vault-addr=https://vault.default.svc.cluster.local:8200
  # Output: https://vault-0.vault.default.svc.cluster.local:8200
`,
	RunE: runLeader,
}

func runLeader(cmd *cobra.Command, args []string) error {
	vaultAddr := cmd.Flag("vault-addr").Value.String()
	skipTLSVerify := cmd.Flag("skip-tls-verify").Value.String() == "true"
	caCert := cmd.Flag("ca-cert").Value.String()

	if vaultAddr == "" {
		slog.Error("vault address not provided", "flag", "--vault-addr", "env_var", "VAULT_ADDR")
		return fmt.Errorf("--vault-addr is required")
	}

	slog.Debug("discovering Vault leader", "vault_addr", vaultAddr, "skip_tls_verify", skipTLSVerify, "ca_cert", caCert)

	// Create client (no auth needed for leader discovery)
	slog.Debug("creating vault client for leader discovery")
	client, err := vaultclient.NewClient(vaultclient.ClientConfig{
		Address:       vaultAddr,
		CACertPath:    caCert,
		SkipTLSVerify: skipTLSVerify,
	})
	if err != nil {
		slog.Error("failed to create vault client", "error", err)
		return fmt.Errorf("create vault client: %w", err)
	}

	// Discover leader
	slog.Debug("querying /v1/sys/leader endpoint")
	leaderAddr, err := client.GetLeaderAddress()
	if err != nil {
		slog.Error("failed to discover vault leader", "error", err)
		return fmt.Errorf("discover leader: %w", err)
	}

	// Print to stdout (can be captured by shell script)
	fmt.Println(leaderAddr)

	slog.Info("vault leader discovered successfully", "leader_address", leaderAddr)
	return nil
}

func init() {
	Cmd.AddCommand(leaderCmd)

	leaderCmd.Flags().String("vault-addr", "", "Vault server address (required)")
	leaderCmd.Flags().String("ca-cert", "", "Path to CA certificate for TLS verification")
	leaderCmd.Flags().Bool("skip-tls-verify", false, "Skip TLS certificate verification (insecure, dev only)")
}
