package cmd

import (
	"github.com/this-is-tobi/ohmlab/cli/cmd/provision"
)

func init() {
	rootCmd.AddCommand(provision.Cmd)
}
