// Package provision runs the product's playbook on an instance's hosts, the
// way `ohmlab provision` does: the command lines, the tags that need care,
// and the kubeconfig of the one play that runs on the workstation.
package provision

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/this-is-tobi/ohmlab/cli/internal/ansible"
	"github.com/this-is-tobi/ohmlab/cli/internal/runner"
)

const (
	// DestroyTag removes k3s, its data and /var/lib/rancher from every node.
	DestroyTag = "k3s-destroy"
	// UpgradeOSTag upgrades the packages of every host, one at a time.
	UpgradeOSTag = "os-upgrade"
)

// playbookGrace is how long an interrupted ansible-playbook has to stop the
// tasks it runs.
const playbookGrace = 30 * time.Second

// Options are what a provision run works with.
type Options struct {
	Product, Playbook, Inventory, VaultPasswordFile string
	// Kubeconfig is the file the k3s-users play reaches the cluster through.
	Kubeconfig string
	Tags       []string
	// Extra goes to ansible-playbook as it is, after the CLI's own arguments.
	Extra   []string
	NoColor bool
}

// DefaultPlaybook is the product's playbook.
func DefaultPlaybook(product string) string {
	return filepath.Join(product, "ansible", "install.yml")
}

// PlaybookCommand is the ansible-playbook run.
func PlaybookCommand(o Options) runner.Cmd {
	args := []string{o.Playbook, "-i", o.Inventory, "--tags", strings.Join(o.Tags, ","), "-e", extraVars(o.Kubeconfig)}
	return runner.Cmd{Name: "ansible-playbook", Args: append(args, o.Extra...), Env: env(o), Grace: playbookGrace}
}

// CollectionsCommand installs or upgrades the collections the playbook needs.
func CollectionsCommand(o Options) runner.Cmd {
	return runner.Cmd{Name: "ansible-galaxy", Args: []string{"collection", "install", "-r", filepath.Join(o.Product, "ansible", "collections", "requirements.yml"), "--upgrade"}, Env: env(o)}
}

func env(o Options) []string {
	e := ansible.Env(o.Product, o.VaultPasswordFile)
	if o.NoColor {
		e = append(e, "ANSIBLE_NOCOLOR=1")
	}
	return e
}

// extraVars is the play's kubeconfig as an -e value: JSON, since Ansible
// splits key=value pairs on spaces.
func extraVars(kubeconfig string) string {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(map[string]string{"K8S_AUTH_KUBECONFIG": kubeconfig})
	return strings.TrimSuffix(b.String(), "\n")
}

// Destructive reports whether a run can remove k3s from the nodes: any
// argument naming k3s-destroy, or never, which selects every play tagged
// never, k3s-destroy's included, however it is spelled.
func Destructive(tags, extra []string) bool {
	return slices.ContainsFunc(slices.Concat(tags, extra), func(a string) bool {
		return strings.Contains(a, DestroyTag) || strings.Contains(a, "never")
	})
}

// SelectsUsersPlay reports whether the tags run the k3s-users play, the one
// that reaches the cluster from the workstation.
func SelectsUsersPlay(tags []string) bool {
	return slices.ContainsFunc(tags, func(t string) bool {
		switch strings.TrimSpace(t) {
		case "all", "tagged", "k3s", "k3s-deploy", "k3s-users":
			return true
		}
		return false
	})
}
