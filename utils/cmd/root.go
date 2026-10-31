package cmd

import (
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var logLevel string

var rootCmd = &cobra.Command{
	Use:   "ohmlab",
	Short: "ohmlab platform management toolkit",
	Long: `ohmlab is a CLI toolkit for managing an ohmlab platform's infrastructure.

It provides commands for:
  - Vault secret initialization and rotation
  - Harbor post-install OIDC configuration
  - SonarQube admin group setup

Designed to run as a Kubernetes Job or locally for platform management.
The ohmlab controller runs the same seeding engine in the cluster.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		initLogger()
	},
	SilenceUsage: true,
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "log level (debug, info, warn, error)")
}

func initLogger() {
	var level slog.Level
	switch strings.ToLower(logLevel) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(handler))
}
