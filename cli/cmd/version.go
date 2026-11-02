package cmd

import (
	"fmt"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// Set via ldflags at build time.
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		goVersion := "unknown"
		if info, ok := debug.ReadBuildInfo(); ok {
			goVersion = info.GoVersion
		}
		fmt.Printf("ohmlab %s\n  commit:  %s\n  built:   %s\n  go:      %s\n", version, commit, date, goVersion)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
