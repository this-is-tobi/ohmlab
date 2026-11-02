package cmd

import (
	"github.com/this-is-tobi/ohmlab/cli/cmd/sonarqube"
)

func init() {
	rootCmd.AddCommand(sonarqube.Cmd)
}
