// Package provision is `ohmlab provision`.
package provision

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/this-is-tobi/ohmlab/cli/internal/ansible"
	"github.com/this-is-tobi/ohmlab/cli/internal/config"
	"github.com/this-is-tobi/ohmlab/cli/internal/provision"
	"github.com/this-is-tobi/ohmlab/cli/internal/runner"
	"github.com/this-is-tobi/ohmlab/cli/internal/ui"
)

// Cmd is `ohmlab provision`.
var Cmd = &cobra.Command{
	Use:   "provision [-- ansible-playbook arguments]",
	Short: "Run the product's playbook on the instance's hosts",
	Long: `Run the product's playbook (ansible/install.yml of --product, or --playbook)
on the hosts of the workspace's inventory, with the product's
ansible/ansible.cfg and the workspace's vault password file. --tags picks the
plays: all by default, which leaves out those that need care, the ones tagged
never. --update installs the playbook's collections first. Arguments after --
go to ansible-playbook as they are, such as -- --limit <host> or -- --check.

One play runs on this workstation: k3s-users, which creates the inventory's
adminUsers on the cluster. It gets a kubeconfig holding only the workspace's
context (--context, $KUBE_CONTEXT_NAME, contextName in ohmlab.yaml, else
ohmlab), never whichever one is current, in a temporary directory removed
afterwards.

The run is confirmed first. k3s-destroy (provision destroy) asks for the word
yes, and so does never, which selects it too, wherever the tags come from:
--tags, arguments after --, or $ANSIBLE_RUN_TAGS. --yes answers both, and
--dry-run prints the commands and runs nothing.
Needs ansible-playbook, and ansible-galaxy with --update.`,
	Args: cobra.ArbitraryArgs,
	RunE: runE,
}

type options struct {
	workspace            config.Flags
	kubeconfig, playbook string
	tags                 []string
	update, dryRun, yes  bool
	noColor              bool
}

var flags options

func init() {
	pf := Cmd.PersistentFlags()
	config.AddFlags(pf, &flags.workspace)
	// The inventory says where the playbook runs, not an instance.
	for _, name := range []string{"instance", "instances"} {
		_ = pf.MarkHidden(name)
	}
	pf.StringVar(&flags.workspace.ContextName, "context", "", "the kubeconfig context of the instance's cluster, the only one the k3s-users play reaches (default: $KUBE_CONTEXT_NAME, contextName in "+config.DefaultPath+", else ohmlab)")
	pf.StringVar(&flags.kubeconfig, "kubeconfig", "", "the kubeconfig the context comes from (default: $KUBECONFIG, then ~/.kube/config)")
	pf.StringVar(&flags.playbook, "playbook", "", "a playbook to run instead of the product's ansible/install.yml")
	pf.BoolVar(&flags.update, "update", false, "install or upgrade the playbook's Ansible collections first")
	pf.BoolVar(&flags.dryRun, "dry-run", false, "print the commands, and run nothing")
	pf.BoolVarP(&flags.yes, "yes", "y", false, "run without asking")
	Cmd.Flags().StringSliceVar(&flags.tags, "tags", []string{"all"}, "the playbook's tags to run, comma-separated")

	Cmd.AddCommand(
		shortcut("destroy", provision.DestroyTag, "Remove k3s, its data and /var/lib/rancher from every node",
			`Run the playbook's k3s-destroy plays on every node of the inventory: k3s,
its data and /var/lib/rancher are removed. It asks for the word yes (--yes
answers it).`),
		shortcut("upgrade-os", provision.UpgradeOSTag, "Upgrade the packages of every host, one at a time",
			`Run the playbook's os-upgrade play: the packages of every host of the
inventory are upgraded, one host at a time, rebooting those that need it.`),
	)
}

// shortcut is a provision run of one tag that needs care.
func shortcut(name, tag, short, long string) *cobra.Command {
	return &cobra.Command{
		Use:         name + " [-- ansible-playbook arguments]",
		Short:       short,
		Long:        long,
		Args:        cobra.ArbitraryArgs,
		Annotations: map[string]string{"tags": tag},
		RunE:        runE,
	}
}

func runE(cmd *cobra.Command, args []string) error {
	extra, err := splitArgs(args, cmd.ArgsLenAtDash())
	if err != nil {
		return err
	}
	tags := flags.tags
	if tag, ok := cmd.Annotations["tags"]; ok {
		tags = []string{tag}
	}
	// An interrupt, or a hangup when the terminal goes, cancels the run
	// rather than killing the process, so the kubeconfig's temporary
	// directory is still removed.
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	noColor, _ := cmd.Flags().GetBool("no-color")
	mode, color := ui.Detect(os.Stdout, noColor)
	o := flags
	o.noColor = !color
	return run(ctx, runner.Exec{}, ui.New(ui.Options{Mode: mode, Color: color, Yes: o.yes}), os.Stdout, os.Stderr, cwd, os.Getenv, o, tags, extra)
}

// splitArgs keeps the arguments after --, for ansible-playbook: one before
// it is a mistyped command or flag.
func splitArgs(args []string, dash int) ([]string, error) {
	if dash == -1 {
		dash = len(args)
	}
	if dash > 0 {
		return nil, fmt.Errorf("unknown argument %q: ansible-playbook arguments go after --", args[0])
	}
	return args, nil
}

func run(ctx context.Context, r runner.Runner, u *ui.UI, out, errOut io.Writer, cwd string, getenv func(string) string, o options, tags, extra []string) error {
	s, _, err := config.Resolve(cwd, o.workspace, getenv)
	if err != nil {
		return err
	}
	playbook := provision.DefaultPlaybook(s.Product)
	if o.playbook != "" {
		playbook = o.playbook
		if !filepath.IsAbs(playbook) {
			playbook = filepath.Join(cwd, playbook)
		}
	}
	opts := provision.Options{Product: s.Product, Playbook: playbook, Inventory: s.Inventory, VaultPasswordFile: s.VaultPasswordFile,
		Tags: tags, Extra: extra, NoColor: o.noColor}

	problems := []error{ansible.Check(s.Product, s.VaultPasswordFile)}
	if st, err := os.Stat(playbook); err != nil || !st.Mode().IsRegular() {
		problems = append(problems, fmt.Errorf("no playbook at %s: pass --playbook, or --product with a checkout of ohmlab", playbook))
	}
	// The playbook reads the inventory's group_vars next to its hosts.
	if st, err := os.Stat(s.Inventory); err != nil || !st.IsDir() {
		problems = append(problems, fmt.Errorf("the inventory %s is not a directory laid out like the product's ansible/inventory-example: pass --inventory, set $ANSIBLE_INVENTORY or inventory in %s", s.Inventory, config.DefaultPath))
	}
	for _, p := range problems {
		switch {
		case p == nil:
		case o.dryRun:
			u.Warn("%v", p)
		default:
			return p
		}
	}
	if !o.dryRun {
		tools := []string{"ansible-playbook"}
		if o.update {
			tools = append(tools, "ansible-galaxy")
		}
		for _, tool := range tools {
			if _, err := r.LookPath(tool); err != nil {
				return fmt.Errorf("%s is not installed: provision runs the product's playbook with it", tool)
			}
		}
	}

	kube, err := provision.LoadContext(o.kubeconfig, s.ContextName)
	if err != nil {
		return err
	}
	joined := strings.Join(tags, ",")
	// Ansible adds $ANSIBLE_RUN_TAGS to --tags.
	selected := tags
	if env := getenv("ANSIBLE_RUN_TAGS"); env != "" {
		selected = slices.Concat(tags, strings.Split(env, ","))
	}
	u.Info("Inventory %s", s.Inventory)
	u.Info("Playbook %s, tags %s", playbook, joined)
	switch {
	case kube.Found:
		u.Info("Context %s (%s) for the k3s-users play", kube.Name, kube.Server)
	case provision.SelectsUsersPlay(selected):
		u.Warn("no context %s in the kubeconfig: the k3s-users play gets none and reaches no cluster, and fails if the inventory declares adminUsers: fetch the context with ohmlab kubeconfig, then run ohmlab provision --tags k3s-users", kube.Name)
	}
	if o.dryRun {
		if o.update {
			u.Info("Would run: %s", provision.CollectionsCommand(opts))
		}
		opts.Kubeconfig = "<a temporary kubeconfig holding no context>"
		if kube.Found {
			opts.Kubeconfig = fmt.Sprintf("<a temporary kubeconfig holding context %s>", kube.Name)
		}
		u.Info("Would run: %s", provision.PlaybookCommand(opts))
		return nil
	}

	var ok bool
	if provision.Destructive(selected, extra) {
		ok, err = u.ConfirmWord(fmt.Sprintf("These tags can run %s (never selects it too), which removes k3s, its data and /var/lib/rancher from every node of %s. Type yes to go on", provision.DestroyTag, s.Inventory), "yes")
	} else {
		ok, err = u.Confirm(fmt.Sprintf("Run %s with tags %s on the hosts of %s?", filepath.Base(playbook), joined, s.Inventory))
	}
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("not confirmed: nothing ran")
	}

	dir, err := os.MkdirTemp("", "ohmlab-provision-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	if opts.Kubeconfig, err = kube.Write(dir); err != nil {
		return err
	}

	// Off a terminal, stdout carries one JSON record per line.
	stdout := out
	if !u.Human() {
		stdout = errOut
	}
	type stage struct {
		title string
		cmd   runner.Cmd
	}
	var steps []stage
	if o.update {
		steps = append(steps, stage{"Update the Ansible collections", provision.CollectionsCommand(opts)})
	}
	steps = append(steps, stage{fmt.Sprintf("Run %s (tags %s)", filepath.Base(playbook), joined), provision.PlaybookCommand(opts)})
	for _, st := range steps {
		c := st.cmd
		c.Stdout, c.Stderr = stdout, errOut
		step := u.Step(st.title)
		if err := step.Done(failure(ctx, r.Run(ctx, c))); err != nil {
			return err
		}
	}
	return nil
}

// failure words a tool's failure without its stderr, which is on screen
// already.
func failure(ctx context.Context, err error) error {
	var exit *runner.ExitError
	switch {
	case !errors.As(err, &exit):
		return err
	case ctx.Err() != nil:
		return fmt.Errorf("%s was interrupted", exit.Name)
	}
	return fmt.Errorf("%s exited %d", exit.Name, exit.Code)
}
