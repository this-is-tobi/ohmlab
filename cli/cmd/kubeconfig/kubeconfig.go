// Package kubeconfig is `ohmlab kubeconfig`.
package kubeconfig

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/this-is-tobi/ohmlab/cli/internal/ansible"
	"github.com/this-is-tobi/ohmlab/cli/internal/config"
	kc "github.com/this-is-tobi/ohmlab/cli/internal/kubeconfig"
	"github.com/this-is-tobi/ohmlab/cli/internal/runner"
	"github.com/this-is-tobi/ohmlab/cli/internal/ui"
)

// Cmd is `ohmlab kubeconfig`.
var Cmd = &cobra.Command{
	Use:   "kubeconfig",
	Short: "Fetch the cluster's kubeconfig and merge it as a named context",
	Long: `Fetch /etc/rancher/k3s/k3s.yaml from the first master with Ansible (become,
through the workspace's inventory and its vault), into a temporary directory
removed afterwards, and merge its cluster, user and context, all called
--name, into the file kubectl writes. The server is the first gateway host of
the inventory on 6443 (HAProxy fronts the API server), else the first master.

An existing file is backed up to <file>.bak first and keeps its current
context; a context of that name leading to another server is replaced only
after a yes. Needs ansible, and the product's ansible/ansible.cfg (--product).`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		// An interrupt, or a hangup when the terminal goes, cancels the run
		// rather than killing the process, so the fetched key's temporary
		// directory is still removed.
		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
		defer stop()
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		noColor, _ := cmd.Flags().GetBool("no-color")
		mode, color := ui.Detect(os.Stdout, noColor)
		u := ui.New(ui.Options{Mode: mode, Color: color, Yes: flags.yes})
		return run(ctx, runner.Exec{}, u, cwd, os.Getenv, flags)
	},
}

type options struct {
	workspace   config.Flags
	kubeconfig  string
	dryRun, yes bool
}

var flags options

func init() {
	f := Cmd.Flags()
	config.AddFlags(f, &flags.workspace)
	// Workspace flags this command has no use for.
	_ = f.MarkHidden("instance")
	_ = f.MarkHidden("instances")
	f.StringVar(&flags.workspace.ContextName, "name", "", "the cluster, user and context name (default: $KUBE_CONTEXT_NAME, contextName in "+config.DefaultPath+", else ohmlab)")
	f.StringVar(&flags.kubeconfig, "kubeconfig", "", "the file to merge into (default: the one kubectl writes, from $KUBECONFIG, else ~/.kube/config)")
	f.BoolVar(&flags.dryRun, "dry-run", false, "print what would run and change, and do nothing")
	f.BoolVarP(&flags.yes, "yes", "y", false, "replace a context of that name that leads to another server without asking")
}

func run(ctx context.Context, r runner.Runner, u *ui.UI, cwd string, getenv func(string) string, o options) error {
	s, _, err := config.Resolve(cwd, o.workspace, getenv)
	if err != nil {
		return err
	}
	opts := kc.Options{Product: s.Product, Inventory: s.Inventory, VaultPasswordFile: s.VaultPasswordFile,
		Name: s.ContextName, Target: o.kubeconfig, DryRun: o.dryRun}
	if opts.Target == "" {
		opts.Target = kc.DefaultTarget()
	}
	server, err := kc.ServerAddress(s.Inventory)
	if err != nil {
		return err
	}
	problem := ansible.Check(s.Product, s.VaultPasswordFile)
	if o.dryRun {
		if problem != nil {
			u.Warn("%v", problem)
		}
		u.Info("Would run: %s", kc.FetchCommand(opts, "<a temporary directory>"))
		u.Info("Would merge context %s, server https://%s, into %s", opts.Name, net.JoinHostPort(server, "6443"), opts.Target)
		return nil
	}
	if _, err := r.LookPath("ansible"); err != nil {
		return errors.New("ansible is not installed: it fetches the kubeconfig from the first master")
	}
	if problem != nil {
		return problem
	}

	step := u.Step("Fetch the kubeconfig from the first master")
	fetched, err := kc.Fetch(ctx, r, opts)
	if err := step.Done(err); err != nil {
		return err
	}
	step = u.Step(fmt.Sprintf("Merge context %s into %s", opts.Name, opts.Target))
	backup, err := kc.Merge(fetched, opts, server, u.Confirm)
	if err := step.Done(err); err != nil {
		return err
	}
	if backup != "" {
		u.Info("The previous file is at %s", backup)
	}
	u.Info("Switch to it with: kubectl config use-context %s", opts.Name)
	return nil
}
