package cmd

import (
	"github.com/this-is-tobi/ohmlab/cli/cmd/bootstrap"
)

func init() {
	rootCmd.AddCommand(bootstrap.Cmd)
}
