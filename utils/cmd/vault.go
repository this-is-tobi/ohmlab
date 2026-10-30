package cmd

import (
	"github.com/this-is-tobi/ohmlab/utils/cmd/vault"
)

func init() {
	rootCmd.AddCommand(vault.Cmd)
}
