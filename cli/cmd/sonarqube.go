package cmd

import (
	"github.com/this-is-tobi/ohmlab/utils/cmd/sonarqube"
)

func init() {
	rootCmd.AddCommand(sonarqube.Cmd)
}
