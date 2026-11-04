package cmd

import (
	"github.com/this-is-tobi/ohmlab/cli/cmd/kubeconfig"
)

func init() {
	rootCmd.AddCommand(kubeconfig.Cmd)
}
