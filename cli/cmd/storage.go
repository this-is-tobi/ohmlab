package cmd

import (
	"github.com/this-is-tobi/ohmlab/cli/cmd/storage"
)

func init() {
	rootCmd.AddCommand(storage.Cmd)
}
