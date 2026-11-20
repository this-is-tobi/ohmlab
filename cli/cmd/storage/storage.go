// Package storage holds the commands about the platform's object storage.
package storage

import (
	"github.com/spf13/cobra"
)

// Cmd is the root storage command.
var Cmd = &cobra.Command{
	Use:   "storage",
	Short: "Object storage: what a rebuild from the cloud replica needs",
	Long: `Commands about the platform's object storage (docs/10-object-storage.md),
run from a workstation.`,
}
