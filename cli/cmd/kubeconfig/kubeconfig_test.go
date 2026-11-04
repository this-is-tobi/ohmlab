package kubeconfig

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"k8s.io/client-go/tools/clientcmd"

	"github.com/this-is-tobi/ohmlab/cli/internal/config"
	"github.com/this-is-tobi/ohmlab/cli/internal/runner"
	"github.com/this-is-tobi/ohmlab/cli/internal/runner/runnertest"
	"github.com/this-is-tobi/ohmlab/cli/internal/ui"
)

const k3s = `apiVersion: v1
kind: Config
clusters:
- name: default
  cluster: {server: "https://127.0.0.1:6443", certificate-authority-data: Y2E=}
users:
- name: default
  user: {token: fake}
contexts:
- name: default
  context: {cluster: default, user: default}
current-context: default
`

// setup makes a product checkout (its ansible.cfg) and an inventory with a
// gateway, and points $KUBECONFIG at a file in a temporary directory, so no
// test can reach the real ~/.kube/config.
func setup(t *testing.T) (product, inventory, target string) {
	t.Helper()
	dir := t.TempDir()
	product, inventory = filepath.Join(dir, "ohmlab"), filepath.Join(dir, "inventory")
	for _, d := range []string{filepath.Join(product, "ansible"), inventory} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(product, "ansible", "ansible.cfg"), []byte("[defaults]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	hosts := "gateway:\n  hosts:\n    gw:\n      ansible_host: 192.168.1.100\nmasters:\n  hosts:\n    m1:\n"
	if err := os.WriteFile(filepath.Join(inventory, "hosts.yml"), []byte(hosts), 0o600); err != nil {
		t.Fatal(err)
	}
	target = filepath.Join(dir, "kube", "config")
	t.Setenv("KUBECONFIG", target)
	t.Setenv("KUBE_CONTEXT_NAME", "")
	t.Setenv("ANSIBLE_INVENTORY", "")
	t.Setenv("ANSIBLE_VAULT_PASSWORD_FILE", "")
	return product, inventory, target
}

func term(out *bytes.Buffer) *ui.UI {
	return ui.New(ui.Options{Out: out, Err: out, In: strings.NewReader(""), Mode: ui.Human})
}

func TestKubeconfigDryRunRunsNothing(t *testing.T) {
	product, inventory, target := setup(t)
	var out bytes.Buffer
	f := &runnertest.Fake{}
	o := options{workspace: config.Flags{Product: product, Inventory: inventory, ContextName: "lab"}, dryRun: true}
	if err := run(context.Background(), f, term(&out), t.TempDir(), os.Getenv, o); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ANSIBLE_CONFIG=" + product + "/ansible/ansible.cfg ansible 'masters[0]' -i " + inventory, "https://192.168.1.100:6443", target, "lab"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
	if len(f.Calls) != 0 {
		t.Errorf("ran %v", f.Calls)
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("wrote %s", target)
	}
}

func TestKubeconfigMergesAndPrintsTheContext(t *testing.T) {
	product, inventory, target := setup(t)
	var out bytes.Buffer
	f := &runnertest.Fake{Handle: func(c runner.Cmd) error {
		for _, kv := range strings.Fields(c.Args[slices.Index(c.Args, "-a")+1]) {
			if dest, ok := strings.CutPrefix(kv, "dest="); ok {
				return os.WriteFile(dest, []byte(k3s), 0o600)
			}
		}
		return errors.New("no dest")
	}}
	o := options{workspace: config.Flags{Product: product, Inventory: inventory, ContextName: "lab"}}
	if err := run(context.Background(), f, term(&out), t.TempDir(), os.Getenv, o); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if len(f.Calls) != 1 || f.Calls[0].Name != "ansible" {
		t.Errorf("calls %v", f.Calls)
	}
	if got := strings.TrimSpace(out.String()); !strings.HasSuffix(got, "kubectl config use-context lab") {
		t.Errorf("output does not end with the use-context hint:\n%s", got)
	}
	cfg, err := clientcmd.LoadFromFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if x := cfg.Contexts["lab"]; x == nil || cfg.Clusters["lab"].Server != "https://192.168.1.100:6443" {
		t.Errorf("target %+v", cfg)
	}
}

func TestKubeconfigNeedsAnsible(t *testing.T) {
	_, inventory, _ := setup(t)
	var out bytes.Buffer
	f := &runnertest.Fake{Missing: []string{"ansible"}}
	o := options{workspace: config.Flags{Product: "/p", Inventory: inventory}}
	err := run(context.Background(), f, term(&out), t.TempDir(), os.Getenv, o)
	if err == nil || !strings.Contains(err.Error(), "ansible is not installed") {
		t.Errorf("got %v, want ansible is not installed", err)
	}
	if len(f.Calls) != 0 {
		t.Errorf("ran %v", f.Calls)
	}
}

// Without the product's ansible.cfg, Ansible would fall back to another
// config silently and fail on the vault: say what to pass instead.
func TestKubeconfigNeedsTheProduct(t *testing.T) {
	_, inventory, _ := setup(t)
	f := &runnertest.Fake{}
	o := options{workspace: config.Flags{Product: t.TempDir(), Inventory: inventory}}
	err := run(context.Background(), f, term(&bytes.Buffer{}), t.TempDir(), os.Getenv, o)
	if err == nil || !strings.Contains(err.Error(), "--product") {
		t.Errorf("got %v, want an error naming --product", err)
	}
	if len(f.Calls) != 0 {
		t.Errorf("ran %v", f.Calls)
	}
}

// helperArgs carries the command line to the child process of
// TestInterruptLeavesNoCopy.
const helperArgs = "OHMLAB_KUBECONFIG_TEST_ARGS"

// Ctrl-C while Ansible runs still removes the fetched key's directory.
func TestInterruptLeavesNoCopy(t *testing.T) {
	if args := os.Getenv(helperArgs); args != "" {
		Cmd.SetArgs(strings.Split(args, "\n"))
		_ = Cmd.Execute()
		os.Exit(0)
	}
	product, inventory, target := setup(t)
	bin, tmp := t.TempDir(), t.TempDir()
	// A fake ansible: writes the fetched file where it is told, then hangs.
	fake := "#!/bin/sh\nfor a; do case \"$a\" in *dest=*) d=${a#*dest=}; printf x > \"${d%% *}\";; esac; done\nexec sleep 30\n"
	if err := os.WriteFile(filepath.Join(bin, "ansible"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestInterruptLeavesNoCopy$")
	child.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "TMPDIR="+tmp,
		helperArgs+"="+strings.Join([]string{"--product", product, "--inventory", inventory, "--kubeconfig", target, "--name", "lab"}, "\n"))
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer child.Process.Kill()

	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if found, _ := filepath.Glob(filepath.Join(tmp, "ohmlab-kubeconfig-*", "k3s.yaml")); len(found) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the fake ansible never wrote the file")
		}
	}
	if err := child.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- child.Wait() }()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("the command did not stop on the interrupt")
	}
	if left, _ := filepath.Glob(filepath.Join(tmp, "ohmlab-kubeconfig-*")); len(left) > 0 {
		t.Errorf("left behind: %v", left)
	}
}
