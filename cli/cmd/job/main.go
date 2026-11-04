// Command ohmlab of the image the Jobs run in the cluster: the commands that
// work there, without those of the workstation CLI (cli/main.go) that need a
// Kubernetes client library, which stays out of this binary.
package main

import (
	"os"

	"github.com/this-is-tobi/ohmlab/cli/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
