package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/this-is-tobi/ohmlab/utils/internal/check"
	"github.com/this-is-tobi/ohmlab/utils/internal/k8s"
)

var checkKubeconfig string

var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Run a read-only platform health sweep",
	Long: `Run a read-only health sweep against the cluster and report anything
that needs attention:

  nodes          NotReady nodes
  argocd-apps    Applications not Synced/Healthy
  vault-secrets  VaultStaticSecrets failing to sync (VSO)
  vault-ha       sealed/uninitialized Vault pods, raft leadership anomalies
  pods           crashlooping / stuck / failed pods
  kyverno        policy violations per namespace

Uses the in-cluster service account when available, otherwise
$KUBECONFIG / ~/.kube/config (static certs or token; no exec plugins).
Exits non-zero when problems are found — usable from cron or CI.`,
	RunE: runCheck,
}

func init() {
	checkCmd.Flags().StringVar(&checkKubeconfig, "kubeconfig", "", "path to kubeconfig (default: in-cluster, then $KUBECONFIG, then ~/.kube/config)")
	rootCmd.AddCommand(checkCmd)
}

func runCheck(cmd *cobra.Command, args []string) error {
	var (
		client *k8s.Client
		err    error
	)
	if checkKubeconfig != "" {
		client, err = k8s.NewKubeconfigClient(checkKubeconfig)
	} else {
		client, err = k8s.NewClient()
	}
	if err != nil {
		return fmt.Errorf("build kubernetes client: %w", err)
	}

	res := check.Run(client)

	for _, s := range res.Sections {
		mark := "✓"
		if !s.OK {
			mark = "✗"
		}
		fmt.Printf("%s %-14s %s\n", mark, s.Name, s.Summary)
	}
	if len(res.Findings) > 0 {
		fmt.Println()
		for _, f := range res.Findings {
			fmt.Printf("  [%s] %s\n", f.Section, f.Detail)
		}
		os.Exit(1)
	}
	return nil
}
