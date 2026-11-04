package audit

import (
	"github.com/spf13/cobra"
)

// Cmd is the root audit command.
var Cmd = &cobra.Command{
	Use:   "audit",
	Short: "Check the platform against its rules",
	Long: `Read-only checks of a running platform against the rules it is built to,
run from a workstation. Each exits non-zero when the rule is broken.`,
}
