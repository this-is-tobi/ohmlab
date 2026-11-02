package harbor

import (
	"github.com/spf13/cobra"
)

// Cmd is the root harbor command
var Cmd = &cobra.Command{
	Use:   "harbor",
	Short: "Harbor container registry management commands",
	Long: `Commands for configuring and managing Harbor container registry.

Harbor is an open source trusted cloud native registry.
These commands provide post-install configuration for OIDC authentication
and scanning integration.`,
}

func init() {
	// Commands are registered in their respective files via init()
}
