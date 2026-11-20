package storage

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"

	"github.com/this-is-tobi/ohmlab/cli/internal/breakglass"
	"github.com/this-is-tobi/ohmlab/cli/internal/kubeclient"
)

var breakGlassFlags struct {
	kubeconfig, context, output, k3sTokenFile string
	noK3s                                     bool
}

var breakGlassCmd = &cobra.Command{
	Use:   "break-glass",
	Short: "Print what a rebuild needs and the cluster cannot keep for itself",
	Long: `Print the break-glass bundle of a cluster, for the owner's password manager:
Vault's unseal keys and root token (a Vault snapshot opens only with them), each
cloud replica's key, crypt passwords and settings (without the passwords the
replica cannot be read), and the k3s server token (an etcd snapshot restores
only with it). It never goes to a store, encrypted or not.

The bundle holds secrets: --output writes it to a new file only its owner reads.

Needs to read the Secret vault-operator-system/vault-unseal-keys, and the
ConfigMaps labelled ohmlab.fr/replica=true with the two Secrets each names.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if breakGlassFlags.noK3s && breakGlassFlags.k3sTokenFile != "" {
			return fmt.Errorf("--k3s-token-file and --no-k3s exclude each other")
		}
		cfg, contextName, err := kubeclient.Load(breakGlassFlags.kubeconfig, breakGlassFlags.context)
		if err != nil {
			return err
		}
		cs, err := kubernetes.NewForConfig(cfg)
		if err != nil {
			return err
		}
		o := breakglass.Options{K3sTokenFile: breakGlassFlags.k3sTokenFile, NoK3s: breakGlassFlags.noK3s}
		return breakGlass(cmd.Context(), cs, contextName, o, breakGlassFlags.output, os.Stdout, os.Stderr)
	},
}

func init() {
	f := breakGlassCmd.Flags()
	f.StringVar(&breakGlassFlags.kubeconfig, "kubeconfig", "", "path to kubeconfig (default: $KUBECONFIG, then ~/.kube/config)")
	f.StringVar(&breakGlassFlags.context, "context", "", "kubeconfig context (default: the current one)")
	f.StringVarP(&breakGlassFlags.output, "output", "o", "", "write the bundle to this new file, mode 0600, instead of stdout")
	f.StringVar(&breakGlassFlags.k3sTokenFile, "k3s-token-file", "", "the k3s server token (Ansible writes inventory/group_vars/.k3sToken)")
	f.BoolVar(&breakGlassFlags.noK3s, "no-k3s", false, "leave the k3s token out: the cluster is not self-hosted k3s")
	Cmd.AddCommand(breakGlassCmd)
}

// breakGlass collects the bundle and writes it to output, a new file, or to stdout.
func breakGlass(ctx context.Context, cs kubernetes.Interface, cluster string, o breakglass.Options, output string, stdout, stderr io.Writer) error {
	b, err := breakglass.Collect(ctx, cs, cluster, o)
	if err != nil {
		return err
	}
	if len(b.Replicas) == 0 {
		fmt.Fprintln(stderr, "warning: no replica: no ConfigMap labelled "+breakglass.ReplicaLabel+"=true, so the bundle holds no cloud key or crypt password")
	}
	if output == "" {
		return breakglass.Write(b, stdout)
	}
	f, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("--output: %w", err)
	}
	if err := breakglass.Write(b, f); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	fmt.Fprintf(stderr, "wrote %s: keep it offline, then delete it\n", output)
	return nil
}
