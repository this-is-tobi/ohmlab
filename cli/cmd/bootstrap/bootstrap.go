// Package bootstrap is `ohmlab bootstrap`.
package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"

	bs "github.com/this-is-tobi/ohmlab/cli/internal/bootstrap"
	"github.com/this-is-tobi/ohmlab/cli/internal/config"
	"github.com/this-is-tobi/ohmlab/cli/internal/instance"
	"github.com/this-is-tobi/ohmlab/cli/internal/runner"
	"github.com/this-is-tobi/ohmlab/cli/internal/ui"
)

// Cmd is `ohmlab bootstrap`.
var Cmd = &cobra.Command{
	Use:   "bootstrap [instance]",
	Short: "Install an instance's bootstrap chart on its cluster",
	Long: `Install the ohmlab chart of an instance on its cluster, as its self-managed
Application then runs it: the Gateway API CRDs (experimental channel) when they
are missing, Argo CD alone on an empty cluster, then the whole chart. Needs
helm (Helm 4).

The cluster is the workspace's context (--context, $KUBE_CONTEXT_NAME,
contextName in ohmlab.yaml, else ohmlab), never whichever one is current, and
it is confirmed first. The chart is the one the instance's ohmlab entry pins,
else its chartPath in the product checkout (--product), or --chart.

The admin password comes from --admin-password-file (- for standard input,
which then needs --yes) or $ARGOCD_ADMIN_PASSWORD. Without either, Argo CD
generates one, printed on a terminal only (ohmlab argocd password prints it
again). --dry-run prints the plan and the helm commands, and changes nothing.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// An interrupt cancels the run rather than killing the process, so
		// the password's values file is still removed.
		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		if len(args) == 1 {
			flags.workspace.Instance = args[0]
		}
		noColor, _ := cmd.Flags().GetBool("no-color")
		mode, color := ui.Detect(os.Stdout, noColor)
		u := ui.New(ui.Options{Mode: mode, Color: color, Yes: flags.yes})
		return run(ctx, runner.Exec{}, bs.Connect, u, cwd, os.Getenv, os.Stdin, flags)
	},
}

type options struct {
	workspace                       config.Flags
	kubeconfig, chart, passwordFile string
	dryRun, yes                     bool
}

var flags options

func init() {
	f := Cmd.Flags()
	config.AddFlags(f, &flags.workspace)
	// The instance is the argument, and no Ansible runs.
	for _, name := range []string{"instance", "inventory", "vault-password-file"} {
		_ = f.MarkHidden(name)
	}
	f.StringVar(&flags.workspace.ContextName, "context", "", "the kubeconfig context of the instance's cluster (default: $KUBE_CONTEXT_NAME, contextName in "+config.DefaultPath+", else ohmlab)")
	f.StringVar(&flags.kubeconfig, "kubeconfig", "", "path to kubeconfig (default: $KUBECONFIG, then ~/.kube/config)")
	f.StringVar(&flags.chart, "chart", "", "a chart directory to install instead of the one the instance's ohmlab entry names")
	f.StringVar(&flags.passwordFile, "admin-password-file", "", "a file holding the Argo CD admin password, - for standard input (default: $ARGOCD_ADMIN_PASSWORD, else Argo CD generates one)")
	f.BoolVar(&flags.dryRun, "dry-run", false, "print the plan and the helm commands, and change nothing")
	f.BoolVarP(&flags.yes, "yes", "y", false, "bootstrap without asking")
}

func run(ctx context.Context, r runner.Runner, connect func(kubeconfig, context string) (bs.Cluster, string, error), u *ui.UI, cwd string, getenv func(string) string, stdin io.Reader, o options) error {
	s, _, err := config.Resolve(cwd, o.workspace, getenv)
	if err != nil {
		return err
	}
	inst, err := instance.Pick(s.Instances, s.Instance)
	if err != nil {
		return err
	}
	values := filepath.Join(inst.Dir, "values", "core", "ohmlab.yaml")
	if _, err := os.Stat(values); errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("no %s: bootstrap installs the chart with the instance's ohmlab values", values)
	} else if err != nil {
		return err
	}
	entry, err := bs.ReadEntry(inst.Dir)
	if err != nil {
		return err
	}
	if !entry.Found {
		u.Warn("%s has no ohmlab app in core.yaml: its Argo CD will not manage itself, and its root manager reconciles every instance of the repository", inst.Name)
	}
	chart, err := bs.ResolveChart(entry, inst.Dir, s.Product, o.chart)
	if err != nil {
		return err
	}
	if o.passwordFile == "-" && !o.yes && !o.dryRun {
		return errors.New("the admin password comes from standard input, which leaves nothing there to answer the question: pass --yes")
	}
	password, source, err := bs.AdminPassword(o.passwordFile, getenv, stdin)
	if err != nil {
		return err
	}
	if err := bs.CheckHelm(ctx, r); err != nil {
		if !o.dryRun {
			return err
		}
		u.Warn("%v", err)
	}
	cluster, server, err := connect(o.kubeconfig, s.ContextName)
	if err != nil {
		return err
	}
	plan, err := bs.Decide(ctx, cluster)
	if err != nil {
		return err
	}
	opts := bs.Options{Chart: chart, Release: entry.Release(), Values: values,
		Kube: bs.Kube{Kubeconfig: o.kubeconfig, Context: s.ContextName}, GatewayAPIURL: bs.GatewayAPIURL(bs.GatewayAPIVersion)}

	u.Info("Instance %s (%s)", inst.Name, inst.Dir)
	u.Info("Context %s, server %s", s.ContextName, server)
	for _, line := range bs.Summary(opts, plan) {
		u.Info("%s", line)
	}
	if password != "" {
		u.Info("Admin password: from %s", source)
	} else {
		u.Info("Admin password: generated by Argo CD")
	}
	if o.dryRun {
		if plan.GatewayAPI {
			u.Info("Would fetch and apply %s", opts.GatewayAPIURL)
		}
		var values string
		if password != "" {
			values = "<a temporary file with the password's hash>"
		}
		for _, step := range bs.Commands(opts, plan, values) {
			u.Info("Would run: %s", step.Cmd)
		}
		return nil
	}

	ok, err := u.Confirm(fmt.Sprintf("Bootstrap %s on context %s (%s)?", inst.Name, s.ContextName, server))
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("not confirmed: nothing changed")
	}
	if err := bs.Run(ctx, r, cluster, u, opts, plan, password); err != nil {
		return err
	}

	ns := opts.Release.Namespace
	// kubectl prints a command for the bootstrapped cluster, whichever one is
	// current where it is pasted.
	kubectl := func(args ...string) string {
		var a []string
		if o.kubeconfig != "" {
			a = append(a, "--kubeconfig", o.kubeconfig)
		}
		a = append(a, "--context", s.ContextName, "-n", ns)
		return runner.Cmd{Name: "kubectl", Args: append(a, args...)}.String()
	}
	if password == "" {
		generated, err := cluster.InitialAdminPassword(ctx, ns)
		switch {
		case errors.Is(err, bs.ErrNoInitialPassword):
			u.Warn("%v: the admin password is the one set before (Argo CD's generated one is deleted once changed)", err)
		case err != nil:
			u.Warn("read the generated admin password: %v", err)
		case u.Human():
			u.Info("Argo CD: user admin, password %s", generated)
			u.Info("Store it, then delete the Secret once you have changed it: %s", kubectl("delete", "secret", "argocd-initial-admin-secret"))
		default:
			u.Info("Argo CD generated the admin password in %s/argocd-initial-admin-secret: print it with ohmlab argocd password", ns)
		}
	}
	u.Info("The root manager ApplicationSet now reconciles the instances of its repository: %s", kubectl("get", "applicationsets,applications"))
	return nil
}
