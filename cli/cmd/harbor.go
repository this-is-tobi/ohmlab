package cmd

import (
	"github.com/this-is-tobi/ohmlab/cli/cmd/harbor"
)

func init() {
	rootCmd.AddCommand(harbor.Cmd)
}
