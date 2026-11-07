// Package argocd is `ohmlab argocd`.
package argocd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	bs "github.com/this-is-tobi/ohmlab/cli/internal/bootstrap"
	"github.com/this-is-tobi/ohmlab/cli/internal/config"
)

// Cmd is `ohmlab argocd`.
var Cmd = &cobra.Command{
	Use:   "argocd",
	Short: "Argo CD helpers",
}

var passwordCmd = &cobra.Command{
	Use:   "password",
	Short: "Print the admin password Argo CD generated",
	Long: `Print the admin password Argo CD generated at the bootstrap, read from
<namespace>/argocd-initial-admin-secret, and nothing else, so it pipes. The
cluster is the workspace's context, as for ohmlab bootstrap. Argo CD keeps that
Secret until someone deletes it, which is due once the password is changed.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		return runPassword(cmd.Context(), bs.Connect, os.Stdout, cwd, os.Getenv, pwFlags)
	},
}

type passwordOptions struct {
	workspace             config.Flags
	kubeconfig, namespace string
}

var pwFlags passwordOptions

func init() {
	f := passwordCmd.Flags()
	f.StringVar(&pwFlags.workspace.Config, "config", "", "workspace file (default: the nearest "+config.DefaultPath+" up to the repository root)")
	f.StringVar(&pwFlags.workspace.ContextName, "context", "", "the kubeconfig context of the instance's cluster (default: $KUBE_CONTEXT_NAME, contextName in "+config.DefaultPath+", else ohmlab)")
	f.StringVar(&pwFlags.kubeconfig, "kubeconfig", "", "path to kubeconfig (default: $KUBECONFIG, then ~/.kube/config)")
	f.StringVar(&pwFlags.namespace, "namespace", bs.DefaultNamespace, "the namespace of the bootstrap release")
	Cmd.AddCommand(passwordCmd)
}

func runPassword(ctx context.Context, connect func(kubeconfig, context string) (bs.Cluster, string, error), out io.Writer, cwd string, getenv func(string) string, o passwordOptions) error {
	s, _, err := config.Resolve(cwd, o.workspace, getenv)
	if err != nil {
		return err
	}
	cluster, _, err := connect(o.kubeconfig, s.ContextName)
	if err != nil {
		return err
	}
	pw, err := cluster.InitialAdminPassword(ctx, o.namespace)
	if errors.Is(err, bs.ErrNoInitialPassword) {
		return fmt.Errorf("%w: it is deleted once the password is changed, or was never created because bootstrap set the password", err)
	}
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, pw)
	return err
}
