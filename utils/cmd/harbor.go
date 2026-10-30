package cmd

import (
	"github.com/this-is-tobi/ohmlab/utils/cmd/harbor"
)

func init() {
	rootCmd.AddCommand(harbor.Cmd)
}
