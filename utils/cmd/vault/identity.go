package vault

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/spf13/cobra"
	vaultclient "github.com/this-is-tobi/ohmlab/utils/internal/vault"
)

var identityCmd = &cobra.Command{
	Use:   "identity",
	Short: "Configure Vault identity groups and aliases from JSON config",
	Long: `Configure Vault identity groups and group-aliases using a declarative JSON configuration.

Creates identity groups with attached policies and group-aliases that map external
identity claims (e.g., OIDC group claims from Keycloak) to internal Vault groups.
This enables OIDC users with specific group memberships to receive policy-based access.

Config format (JSON):
  {
    "groups": [
      {
        "name": "homelab-admins",
        "policies": ["admin", "reader"]
      }
    ],
    "groupAliases": [
      {
        "name": "/admin",
        "group": "homelab-admins",
        "mountAccessor": "oidc_xxxxx"
      }
    ]
  }

The "name" in groupAliases should match the external claim value (e.g., Keycloak group path).
The "group" field references the group name defined in the groups section.
The "mountAccessor" is the Vault auth mount accessor. If empty, it will be auto-discovered
by searching for the first auth mount of type 'oidc' (retrieve manually via: vault auth list -format=json).`,
	RunE: runIdentity,
}

var (
	identityConfigFile string
	identityDryRun     bool
)

func init() {
	Cmd.AddCommand(identityCmd)

	f := identityCmd.Flags()
	f.StringVar(&vaultAddr, "vault-addr", envOrDefault("VAULT_ADDR", ""), "Vault server address")
	f.StringVar(&vaultCACert, "ca-cert", envOrDefault("VAULT_CACERT", ""), "path to Vault CA certificate")
	f.BoolVar(&vaultSkipTLS, "skip-tls-verify", os.Getenv("VAULT_SKIP_VERIFY") == "true", "skip TLS verification")
	f.StringVar(&vaultAuthMethod, "auth-method", envOrDefault("VAULT_AUTH_METHOD", "kubernetes"), "auth method: kubernetes, jwt, approle, token")
	f.StringVar(&vaultAuthMount, "auth-mount", envOrDefault("VAULT_AUTH_MOUNT", ""), "auth mount path (defaults to method name)")
	f.StringVar(&vaultRole, "role", envOrDefault("VAULT_ROLE", "vault-post-config"), "Vault auth role name")
	f.StringVar(&vaultToken, "token", "", "Vault token (for token auth, or use VAULT_TOKEN env)")
	f.StringVar(&vaultRoleID, "role-id", envOrDefault("VAULT_ROLE_ID", ""), "AppRole role ID")
	f.StringVar(&vaultSecretID, "secret-id", envOrDefault("VAULT_SECRET_ID", ""), "AppRole secret ID")
	f.StringVar(&identityConfigFile, "config", envOrDefault("IDENTITY_FILE", ""), "path to identity config JSON file")
	f.BoolVar(&identityDryRun, "dry-run", false, "print what would be done without writing to Vault")

	_ = identityCmd.MarkFlagRequired("config")
}

func runIdentity(cmd *cobra.Command, args []string) error {
	start := time.Now()
	slog.Info("vault identity configuration started", "config_file", identityConfigFile)

	// Validate Vault address
	if vaultAddr == "" {
		slog.Error("vault address not set", "env_var", "VAULT_ADDR", "flag", "--vault-addr")
		return fmt.Errorf("--vault-addr is required or set VAULT_ADDR env var")
	}
	slog.Debug("vault configuration", "address", vaultAddr, "auth_method", vaultAuthMethod, "auth_mount", vaultAuthMount)

	// Read config file
	slog.Debug("reading identity config file", "path", identityConfigFile)
	configData, err := os.ReadFile(identityConfigFile)
	if err != nil {
		slog.Error("failed to read identity config file", "path", identityConfigFile, "error", err)
		return fmt.Errorf("read identity config file %s: %w", identityConfigFile, err)
	}
	slog.Debug("identity config file read", "size_bytes", len(configData))

	var config struct {
		Groups       []Group      `json:"groups"`
		GroupAliases []GroupAlias `json:"groupAliases"`
	}
	if err := json.Unmarshal(configData, &config); err != nil {
		slog.Error("failed to parse identity config", "error", err)
		return fmt.Errorf("parse identity config: %w", err)
	}

	slog.Info("parsed identity config", "groups_count", len(config.Groups), "aliases_count", len(config.GroupAliases))

	if len(config.Groups) == 0 && len(config.GroupAliases) == 0 {
		slog.Info("identity config is empty, nothing to do")
		return nil
	}

	// Log config details
	for i, g := range config.Groups {
		slog.Debug("group in config", "index", i, "name", g.Name, "policies", g.Policies)
	}
	for i, a := range config.GroupAliases {
		slog.Debug("group-alias in config", "index", i, "name", a.Name, "group", a.Group, "mount_accessor", a.MountAccessor)
	}

	if identityDryRun {
		slog.Warn("DRY RUN MODE: no changes will be made to Vault")
		slog.Info("dry run: would create", "groups", len(config.Groups), "aliases", len(config.GroupAliases))
		for _, g := range config.Groups {
			slog.Info("dry run: create group", "name", g.Name, "policies", g.Policies)
		}
		for _, a := range config.GroupAliases {
			slog.Info("dry run: create group-alias", "name", a.Name, "group", a.Group, "mount_accessor", a.MountAccessor)
		}
		return nil
	}

	// Create Vault client
	slog.Debug("creating vault client", "address", vaultAddr, "skip_tls_verify", vaultSkipTLS, "ca_cert_path", vaultCACert)
	client, err := vaultclient.NewClient(vaultclient.ClientConfig{
		Address:       vaultAddr,
		CACertPath:    vaultCACert,
		SkipTLSVerify: vaultSkipTLS,
		Timeout:       30 * time.Second,
	})
	if err != nil {
		slog.Error("failed to create vault client", "error", err)
		return fmt.Errorf("create vault client: %w", err)
	}
	slog.Debug("vault client created successfully")

	// Check Vault health
	slog.Debug("checking vault health")
	if err := client.Health(); err != nil {
		slog.Error("vault health check failed", "error", err)
		return fmt.Errorf("vault health check: %w", err)
	}
	slog.Debug("vault health check passed")

	// Authenticate
	slog.Debug("authenticating to vault", "method", vaultAuthMethod, "mount", vaultAuthMount, "role", vaultRole)
	authParams := map[string]string{
		"role":      vaultRole,
		"token":     vaultToken,
		"role_id":   vaultRoleID,
		"secret_id": vaultSecretID,
	}
	if err := client.Authenticate(vaultclient.AuthMethod(vaultAuthMethod), vaultAuthMount, authParams); err != nil {
		slog.Error("authentication failed", "method", vaultAuthMethod, "mount", vaultAuthMount, "error", err)
		return fmt.Errorf("authenticate to vault: %w", err)
	}
	slog.Info("authenticated to vault successfully", "method", vaultAuthMethod)

	// Track group IDs for aliases
	groupIDs := make(map[string]string)
	createdGroups := 0
	createdAliases := 0

	// Create groups
	slog.Info("creating identity groups", "count", len(config.Groups))
	for i, group := range config.Groups {
		slog.Debug("creating group", "index", i, "name", group.Name, "policies", group.Policies)
		groupID, err := client.IdentityCreateGroup(group.Name, group.Type, group.Policies)
		if err != nil {
			slog.Error("failed to create group", "name", group.Name, "error", err)
			return fmt.Errorf("create group %s: %w", group.Name, err)
		}
		groupIDs[group.Name] = groupID
		createdGroups++
		slog.Info("created group successfully", "name", group.Name, "id", groupID)
	}

	// Create group-aliases
	slog.Info("creating group-aliases", "count", len(config.GroupAliases))
	for i, alias := range config.GroupAliases {
		slog.Debug("processing group-alias", "index", i, "name", alias.Name, "group", alias.Group)

		groupID, ok := groupIDs[alias.Group]
		if !ok {
			slog.Error("group not found for alias", "alias_name", alias.Name, "group_name", alias.Group)
			return fmt.Errorf("group %s referenced by alias %s not found in config", alias.Group, alias.Name)
		}
		slog.Debug("resolved group for alias", "alias_name", alias.Name, "group_id", groupID)

		// Auto-discover mount accessor if not provided
		mountAccessor := alias.MountAccessor
		if mountAccessor == "" {
			slog.Info("mount accessor not provided, auto-discovering", "alias_name", alias.Name)
			// Try to detect auth type from alias name (e.g., "/admin" suggests OIDC group claim)
			// Default to "oidc" if no explicit mount accessor provided
			discovered, err := client.GetAuthMountAccessor("oidc", "")
			if err != nil {
				slog.Error("failed to auto-discover mount accessor", "alias_name", alias.Name, "error", err)
				return fmt.Errorf("auto-discover mount accessor for alias %s: %w", alias.Name, err)
			}
			mountAccessor = discovered
			slog.Info("auto-discovered mount accessor", "alias_name", alias.Name, "mount_accessor", mountAccessor)
		} else {
			slog.Debug("using provided mount accessor", "alias_name", alias.Name, "mount_accessor", mountAccessor)
		}

		slog.Debug("creating group-alias", "name", alias.Name, "group_id", groupID, "mount_accessor", mountAccessor)
		if err := client.IdentityCreateGroupAlias(alias.Name, groupID, mountAccessor); err != nil {
			slog.Error("failed to create group-alias", "name", alias.Name, "group", alias.Group, "error", err)
			return fmt.Errorf("create group-alias %s: %w", alias.Name, err)
		}
		createdAliases++
		slog.Info("created group-alias successfully", "name", alias.Name, "group_id", groupID)
	}

	duration := time.Since(start).Seconds()
	slog.Info("identity configuration completed successfully",
		"groups_created", createdGroups,
		"aliases_created", createdAliases,
		"duration_seconds", fmt.Sprintf("%.2f", duration))
	return nil
}

// Group represents an identity group configuration.
type Group struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"` // "internal" (default) or "external" (required for group-aliases)
	Policies []string `json:"policies"`
}

// GroupAlias represents a group-alias configuration that maps external identities
// (e.g., OIDC group claims) to internal Vault groups.
type GroupAlias struct {
	Name          string `json:"name"`
	Group         string `json:"group"`
	MountAccessor string `json:"mountAccessor"`
}
