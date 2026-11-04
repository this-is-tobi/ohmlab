// Package config reads the workspace file, ohmlab.yaml at the root of an
// instance repository, and resolves the settings of the workstation
// commands: flags, then environment, then the file, then defaults.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spf13/pflag"
	"gopkg.in/yaml.v3"
)

// DefaultPath is the name of the workspace file.
const DefaultPath = "ohmlab.yaml"

// File is the workspace file. Relative paths in it are relative to it.
type File struct {
	// Product is a checkout of the product, for what is not published yet
	// (the Ansible roles).
	Product string `yaml:"product"`
	// Instances holds the instance folders.
	Instances string `yaml:"instances"`
	// Instance is the one to act on, when there are several.
	Instance          string `yaml:"instance"`
	Inventory         string `yaml:"inventory"`
	VaultPasswordFile string `yaml:"vaultPasswordFile"`
	// ContextName is the kubeconfig context of the instance's cluster.
	ContextName string `yaml:"contextName"`
	Audit       Audit  `yaml:"audit"`
}

// Audit configures the audit commands.
type Audit struct {
	TLS AuditTLS `yaml:"tls"`
}

// AuditTLS configures `ohmlab audit tls`.
type AuditTLS struct {
	Exceptions []TLSException `yaml:"exceptions"`
}

// TLSException lets the ports of a Service stay plaintext, for a reason.
type TLSException struct {
	Namespace string  `yaml:"namespace" json:"namespace"`
	Service   string  `yaml:"service" json:"service"`
	Ports     []int32 `yaml:"ports,omitempty" json:"ports,omitempty"` // empty: every port
	Reason    string  `yaml:"reason" json:"reason"`
}

// Load reads and checks the workspace file at path. Unknown keys are errors,
// so a typo cannot silently drop an exception. A missing file is an error
// that wraps fs.ErrNotExist.
func Load(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f File
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	for i, e := range f.Audit.TLS.Exceptions {
		if e.Namespace == "" || e.Service == "" || e.Reason == "" {
			return nil, fmt.Errorf("%s: audit.tls.exceptions[%d] (%s/%s) needs a namespace, a service and a reason", path, i, e.Namespace, e.Service)
		}
	}
	return &f, nil
}

// Find returns the nearest workspace file in dir or a parent, up to the root
// of the repository dir is in, or in dir alone outside a repository. None is
// an error wrapping fs.ErrNotExist.
func Find(dir string) (string, error) {
	dir = filepath.Clean(dir)
	top := repositoryRoot(dir)
	for d := dir; ; d = filepath.Dir(d) {
		if p := filepath.Join(d, DefaultPath); isFile(p) {
			return p, nil
		}
		if top == "" || d == top || d == filepath.Dir(d) {
			break
		}
	}
	return "", fmt.Errorf("no %s in %s or a parent up to the repository root: %w", DefaultPath, dir, fs.ErrNotExist)
}

// repositoryRoot is the nearest directory holding .git (a directory, or a
// file in a worktree), or "".
func repositoryRoot(dir string) string {
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			return d
		}
		if d == filepath.Dir(d) {
			return ""
		}
	}
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

// Flags are the workspace settings given on the command line; empty is not set.
type Flags struct {
	Config, Product, Instances, Instance, Inventory, VaultPasswordFile, ContextName string
}

// AddFlags registers the workspace flags. A command that needs ContextName
// adds its own flag for it, named for what it is there.
func AddFlags(fs *pflag.FlagSet, f *Flags) {
	fs.StringVar(&f.Config, "config", "", "workspace file (default: the nearest "+DefaultPath+" up to the repository root)")
	fs.StringVar(&f.Product, "product", "", "a checkout of the product (default: product in "+DefaultPath+", else the workspace)")
	fs.StringVar(&f.Instances, "instances", "", "the instance folders (default: instances in "+DefaultPath+", else argo-cd/instances)")
	fs.StringVar(&f.Instance, "instance", "", "the instance to act on (default: instance in "+DefaultPath+", else the only one)")
	fs.StringVar(&f.Inventory, "inventory", "", "Ansible inventory (default: $ANSIBLE_INVENTORY, inventory in "+DefaultPath+", else ansible/inventory)")
	fs.StringVar(&f.VaultPasswordFile, "vault-password-file", "", "Ansible vault password file (default: $ANSIBLE_VAULT_PASSWORD_FILE, vaultPasswordFile in "+DefaultPath+", else ansible/.vault_password when it exists)")
}

// Settings are the resolved workspace settings, paths absolute when their
// source was.
type Settings struct {
	// Workspace is the directory of the workspace file, else the repository
	// root, else the current directory: where the defaults are.
	Workspace, Product, Instances, Instance, Inventory, VaultPasswordFile, ContextName string
}

// Resolve settles each setting from flags, then the environment (the names
// run.sh read), then the workspace file, then defaults. Relative paths
// resolve against cwd for flags and the environment, against the file's
// directory for the file. The file is flags.Config, else the one Find gives
// from cwd; only a file flags.Config names has to exist. With no file, the
// returned File is empty.
func Resolve(cwd string, flags Flags, getenv func(string) string) (Settings, *File, error) {
	file, dir := &File{}, ""
	path := flags.Config
	if path != "" {
		path = abs(cwd, path)
	} else if p, err := Find(cwd); err == nil {
		path = p
	} else if !errors.Is(err, fs.ErrNotExist) {
		return Settings{}, nil, err
	}
	if path != "" {
		f, err := Load(path)
		if err != nil {
			return Settings{}, nil, err
		}
		file, dir = f, filepath.Dir(path)
	}

	s := Settings{Workspace: dir}
	if s.Workspace == "" {
		s.Workspace = repositoryRoot(cwd)
	}
	if s.Workspace == "" {
		s.Workspace = cwd
	}
	// pick returns the first source that is set, as a path when isPath.
	pick := func(isPath bool, flag, envName, fromFile, def string) string {
		switch {
		case flag != "":
			return pathIf(isPath, cwd, flag)
		case envName != "" && getenv(envName) != "":
			return pathIf(isPath, cwd, getenv(envName))
		case fromFile != "":
			return pathIf(isPath, dir, fromFile)
		}
		return def
	}
	s.Product = pick(true, flags.Product, "", file.Product, s.Workspace)
	s.Instances = pick(true, flags.Instances, "", file.Instances, filepath.Join(s.Workspace, "argo-cd", "instances"))
	s.Instance = pick(false, flags.Instance, "", file.Instance, "")
	s.Inventory = pick(true, flags.Inventory, "ANSIBLE_INVENTORY", file.Inventory, filepath.Join(s.Workspace, "ansible", "inventory"))
	s.VaultPasswordFile = pick(true, flags.VaultPasswordFile, "ANSIBLE_VAULT_PASSWORD_FILE", file.VaultPasswordFile, vaultPasswordFile(s.Workspace))
	s.ContextName = pick(false, flags.ContextName, "KUBE_CONTEXT_NAME", file.ContextName, "ohmlab")
	return s, file, nil
}

// vaultPasswordFile is the workspace's ansible/.vault_password when it
// exists. The product's ansible.cfg names that file too, but resolves it in
// the product checkout, which holds no instance's password.
func vaultPasswordFile(workspace string) string {
	p := filepath.Join(workspace, "ansible", ".vault_password")
	if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
		return p
	}
	return ""
}

func pathIf(isPath bool, base, p string) string {
	if !isPath {
		return p
	}
	return abs(base, p)
}

func abs(base, p string) string {
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(base, p)
}
