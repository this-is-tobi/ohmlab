// Package kubeconfig fetches a k3s cluster's kubeconfig from its first master
// through Ansible and merges it into the user's as a named context.
package kubeconfig

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/this-is-tobi/ohmlab/cli/internal/runner"
)

// k3sPath is where k3s writes the cluster-admin kubeconfig on a server node.
const k3sPath = "/etc/rancher/k3s/k3s.yaml"

// Options are what the kubeconfig command works with.
type Options struct {
	Product, Inventory, VaultPasswordFile string
	// Name is the cluster, user and context name in the merged kubeconfig.
	Name string
	// Target is the kubeconfig file merged into.
	Target string
	DryRun bool
}

// FetchCommand is the Ansible run that copies k3s.yaml from the first master
// into dest. k3s writes it for root only, hence --become; the inventory's
// vault, when there is one, holds the become password.
func FetchCommand(o Options, dest string) runner.Cmd {
	args := []string{"masters[0]", "-i", o.Inventory, "--become", "-m", "ansible.builtin.fetch",
		"-a", fmt.Sprintf("src=%s dest=%s flat=true", k3sPath, filepath.Join(dest, "k3s.yaml"))}
	if vault := filepath.Join(o.Inventory, "group_vars", "vault.yml"); isFile(vault) {
		args = append(args, "-e", "@"+vault)
	}
	if o.VaultPasswordFile != "" {
		args = append(args, "--vault-password-file", o.VaultPasswordFile)
	}
	return runner.Cmd{Name: "ansible", Args: args, Env: []string{"ANSIBLE_CONFIG=" + filepath.Join(o.Product, "ansible", "ansible.cfg")}}
}

// Fetch copies k3s.yaml into a private temporary directory, reads it and
// removes the directory, on failure too: the merged kubeconfig stays the only
// copy of the cluster-admin key.
func Fetch(ctx context.Context, r runner.Runner, o Options) (*clientcmdapi.Config, error) {
	dir, err := os.MkdirTemp("", "ohmlab-kubeconfig-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	var out bytes.Buffer
	c := FetchCommand(o, dir)
	c.Stdout = &out
	if err := r.Run(ctx, c); err != nil {
		// Ansible reports a failed task on stdout.
		return nil, fmt.Errorf("fetch %s from the first master: %w%s", k3sPath, err, indented(out.String()))
	}
	data, err := os.ReadFile(filepath.Join(dir, "k3s.yaml"))
	if err != nil {
		return nil, fmt.Errorf("fetch %s from the first master: %w", k3sPath, err)
	}
	cfg, err := clientcmd.Load(data)
	if err != nil {
		return nil, fmt.Errorf("the fetched %s: %w", k3sPath, err)
	}
	if len(cfg.Clusters) != 1 || len(cfg.AuthInfos) != 1 || len(cfg.Contexts) != 1 {
		return nil, fmt.Errorf("the fetched %s has %d clusters, %d users and %d contexts, want one of each", k3sPath, len(cfg.Clusters), len(cfg.AuthInfos), len(cfg.Contexts))
	}
	return cfg, nil
}

// indented is the last lines of a tool's output, indented under an error.
func indented(out string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return ""
	}
	if len(lines) > 20 {
		lines = lines[len(lines)-20:]
	}
	return "\n  " + strings.Join(lines, "\n  ")
}

// Merge writes the fetched cluster and user, and a context joining them, all
// called o.Name, into o.Target, the server set to https://<server>:6443.
// A context or cluster of that name leading to another server is replaced
// only if confirm says yes. An existing target is backed up to <target>.bak
// first, and keeps its current context; a new one gets o.Name. The target is
// left readable by its owner only. It returns the backup's path, or "".
func Merge(fetched *clientcmdapi.Config, o Options, server string, confirm func(string) (bool, error)) (backup string, err error) {
	if o.Name == "" || o.Target == "" {
		return "", errors.New("a context name and a target file are required")
	}
	if len(fetched.Clusters) != 1 || len(fetched.AuthInfos) != 1 {
		return "", fmt.Errorf("the fetched kubeconfig has %d clusters and %d users, want one of each", len(fetched.Clusters), len(fetched.AuthInfos))
	}
	var cluster *clientcmdapi.Cluster
	var user *clientcmdapi.AuthInfo
	for _, c := range fetched.Clusters {
		cluster = c.DeepCopy()
	}
	for _, u := range fetched.AuthInfos {
		user = u.DeepCopy()
	}
	cluster.Server = "https://" + net.JoinHostPort(server, "6443")
	cluster.LocationOfOrigin, user.LocationOfOrigin = "", ""

	cfg, err := clientcmd.LoadFromFile(o.Target)
	isNew := errors.Is(err, fs.ErrNotExist)
	switch {
	case isNew:
		cfg = clientcmdapi.NewConfig()
	case err != nil:
		return "", err
	}
	if old := serverOf(cfg, o.Name); old != "" && old != cluster.Server {
		ok, err := confirm(fmt.Sprintf("Context %s in %s leads to %s: replace it with %s?", o.Name, o.Target, old, cluster.Server))
		if err != nil {
			return "", err
		}
		if !ok {
			return "", fmt.Errorf("context %s left as it is, at %s", o.Name, old)
		}
	}
	cfg.Clusters[o.Name] = cluster
	cfg.AuthInfos[o.Name] = user
	cfg.Contexts[o.Name] = &clientcmdapi.Context{Cluster: o.Name, AuthInfo: o.Name}
	if isNew {
		cfg.CurrentContext = o.Name
	} else {
		backup = o.Target + ".bak"
		if err := copyPrivate(o.Target, backup); err != nil {
			return "", err
		}
	}
	if err := clientcmd.WriteToFile(*cfg, o.Target); err != nil {
		return "", err
	}
	return backup, os.Chmod(o.Target, 0o600)
}

// serverOf is the server the context name leads to, else the one of the
// cluster of that name, else "".
func serverOf(cfg *clientcmdapi.Config, name string) string {
	if x := cfg.Contexts[name]; x != nil {
		if c := cfg.Clusters[x.Cluster]; c != nil {
			return c.Server
		}
	}
	if c := cfg.Clusters[name]; c != nil {
		return c.Server
	}
	return ""
}

// copyPrivate copies src to dst, readable by its owner only.
func copyPrivate(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.WriteFile(dst, data, 0o600); err != nil {
		return err
	}
	return os.Chmod(dst, 0o600)
}

// DefaultTarget is the file kubectl writes: the first existing file of
// $KUBECONFIG, else its last, else ~/.kube/config.
func DefaultTarget() string {
	return clientcmd.NewDefaultPathOptions().GetDefaultFilename()
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}
