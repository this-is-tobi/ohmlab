package sonarqube

import (
	"github.com/spf13/cobra"
)

// Cmd is the root sonarqube command
var Cmd = &cobra.Command{
	Use:   "sonarqube",
	Short: "SonarQube code quality management commands",
	Long: `Commands for configuring and managing SonarQube.

SonarQube is a continuous inspection platform for code quality.
These commands provide post-install configuration for admin group setup
and security policy integration.`,
}

func init() {
	// Commands are registered in their respective files via init()
}
