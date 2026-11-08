package cmd

import (
	"github.com/this-is-tobi/ohmlab/cli/cmd/sops"
)

func init() {
	rootCmd.AddCommand(sops.Cmd)
}
