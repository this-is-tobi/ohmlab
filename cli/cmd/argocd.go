package cmd

import (
	"github.com/this-is-tobi/ohmlab/cli/cmd/argocd"
)

func init() {
	rootCmd.AddCommand(argocd.Cmd)
}
