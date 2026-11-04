package cmd

import (
	"github.com/this-is-tobi/ohmlab/cli/cmd/audit"
)

func init() {
	rootCmd.AddCommand(audit.Cmd)
}
