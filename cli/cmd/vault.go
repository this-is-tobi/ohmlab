package cmd

import (
	"github.com/this-is-tobi/ohmlab/cli/cmd/vault"
)

func init() {
	rootCmd.AddCommand(vault.Cmd)
}
