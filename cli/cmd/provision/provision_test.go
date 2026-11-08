package provision

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/this-is-tobi/ohmlab/cli/internal/runner"
	"github.com/this-is-tobi/ohmlab/cli/internal/runner/runnertest"
	"github.com/this-is-tobi/ohmlab/cli/internal/ui"
)

// kubeconfig is a workstation's: a work cluster, current, and the lab one.
const kubeconfig = `apiVersion: v1
kind: Config
clusters:
- name: work
  cluster: {server: "https://work.example:443"}
- name: lab
  cluster: {server: "https://10.0.0.1:6443"}
users:
- name: work
  user: {token: work}
- name: lab
  user: {token: lab}
contexts:
- name: work
  context: {cluster: work, user: work}
- name: lab
  context: {cluster: lab, user: lab}
current-context: work
`

// setup makes a workspace (ohmlab.yaml, an inventory, a vault password
// file), a product checkout and a kubeconfig, points the environment at them
// alone, and gives the test a TMPDIR of its own. It returns the workspace and
// the product.
func setup(t *testing.T) (ws, product string) {
	t.Helper()
	dir := t.TempDir()
	ws, product = filepath.Join(dir, "lab"), filepath.Join(dir, "ohmlab")
	for path, content := range map[string]string{
		filepath.Join(ws, "ohmlab.yaml"):                                     "product: ../ohmlab\n",
		filepath.Join(ws, "ansible", "inventory", "hosts.yml"):               "masters:\n  hosts:\n    m1:\n",
		filepath.Join(ws, "ansible", ".vault_password"):                      "x\n",
		filepath.Join(product, "ansible", "ansible.cfg"):                     "[defaults]\n",
		filepath.Join(product, "ansible", "install.yml"):                     "---\n",
		filepath.Join(product, "ansible", "collections", "requirements.yml"): "---\n",
		filepath.Join(dir, "kube", "config"):                                 kubeconfig,
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("KUBECONFIG", filepath.Join(dir, "kube", "config"))
	t.Setenv("KUBE_CONTEXT_NAME", "lab")
	t.Setenv("ANSIBLE_INVENTORY", "")
	t.Setenv("ANSIBLE_VAULT_PASSWORD_FILE", "")
	t.Setenv("TMPDIR", t.TempDir())
	return ws, product
}

func term(out *bytes.Buffer, in string, yes bool) *ui.UI {
	return ui.New(ui.Options{Out: out, Err: out, In: strings.NewReader(in), Mode: ui.Human, Yes: yes})
}

// kubeconfigOf is the kubeconfig the -e of an ansible-playbook run names.
func kubeconfigOf(t *testing.T, c runner.Cmd) string {
	t.Helper()
	i := slices.Index(c.Args, "-e")
	var vars map[string]string
	if i < 0 || i+1 == len(c.Args) || json.Unmarshal([]byte(c.Args[i+1]), &vars) != nil {
		t.Fatalf("no -e JSON in %q", c.Args)
	}
	return vars["K8S_AUTH_KUBECONFIG"]
}

func tmpIsEmpty(t *testing.T) {
	t.Helper()
	if left, _ := os.ReadDir(os.Getenv("TMPDIR")); len(left) > 0 {
		t.Errorf("left in TMPDIR: %v", left)
	}
}

func gone(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("%s is still there", path)
	}
}

var all = []string{"all"}

func TestProvisionRunsThePlaybook(t *testing.T) {
	ws, product := setup(t)
	var out bytes.Buffer
	f := &runnertest.Fake{}
	if err := run(context.Background(), f, term(&out, "", true), &out, &out, ws, os.Getenv, options{yes: true}, all, nil); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if len(f.Calls) != 1 {
		t.Fatalf("calls %v", f.Calls)
	}
	kc := kubeconfigOf(t, f.Calls[0])
	want := "ANSIBLE_CONFIG=" + product + "/ansible/ansible.cfg ANSIBLE_VAULT_PASSWORD_FILE=" + ws + "/ansible/.vault_password ansible-playbook " + product + "/ansible/install.yml -i " + ws + `/ansible/inventory --tags all -e '{"K8S_AUTH_KUBECONFIG":"` + kc + `"}'`
	if got := f.Calls[0].String(); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if filepath.Base(kc) != "kubeconfig" || !strings.HasPrefix(filepath.Base(filepath.Dir(kc)), "ohmlab-provision-") {
		t.Errorf("kubeconfig %s", kc)
	}
}

// The k3s-users play acts on the current context of the kubeconfig it gets:
// it gets the workspace's alone, never the user's current one.
func TestProvisionPinsTheWorkspaceContext(t *testing.T) {
	ws, _ := setup(t)
	var path string
	var seen *clientcmdapi.Config
	f := &runnertest.Fake{Handle: func(c runner.Cmd) error {
		path = kubeconfigOf(t, c)
		cfg, err := clientcmd.LoadFromFile(path)
		seen = cfg
		return err
	}}
	if err := run(context.Background(), f, term(&bytes.Buffer{}, "", true), io.Discard, io.Discard, ws, os.Getenv, options{yes: true}, all, nil); err != nil {
		t.Fatal(err)
	}
	if seen == nil || seen.CurrentContext != "lab" || len(seen.Contexts) != 1 || seen.Clusters[seen.Contexts["lab"].Cluster].Server != "https://10.0.0.1:6443" {
		t.Errorf("the play got %+v", seen)
	}
	gone(t, filepath.Dir(path))
}

func TestProvisionRemovesTheKubeconfig(t *testing.T) {
	ws, _ := setup(t)
	var path string
	f := &runnertest.Fake{Handle: func(c runner.Cmd) error {
		path = kubeconfigOf(t, c)
		return &runner.ExitError{Name: "ansible-playbook", Code: 2, Stderr: "boom"}
	}}
	err := run(context.Background(), f, term(&bytes.Buffer{}, "", true), io.Discard, io.Discard, ws, os.Getenv, options{yes: true}, all, nil)
	if err == nil || err.Error() != "ansible-playbook exited 2" {
		t.Errorf("got %v, want ansible-playbook exited 2", err)
	}
	gone(t, filepath.Dir(path))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f = &runnertest.Fake{Handle: func(c runner.Cmd) error {
		path = kubeconfigOf(t, c)
		cancel()
		return ctx.Err()
	}}
	if err := run(ctx, f, term(&bytes.Buffer{}, "", true), io.Discard, io.Discard, ws, os.Getenv, options{yes: true}, all, nil); err == nil {
		t.Error("no error after an interrupt")
	}
	gone(t, filepath.Dir(path))
}

func TestProvisionUpdate(t *testing.T) {
	ws, product := setup(t)
	f := &runnertest.Fake{}
	if err := run(context.Background(), f, term(&bytes.Buffer{}, "", true), io.Discard, io.Discard, ws, os.Getenv, options{yes: true, update: true}, all, nil); err != nil {
		t.Fatal(err)
	}
	if len(f.Calls) != 2 || f.Calls[1].Name != "ansible-playbook" {
		t.Fatalf("calls %v", f.Calls)
	}
	want := "ANSIBLE_CONFIG=" + product + "/ansible/ansible.cfg ANSIBLE_VAULT_PASSWORD_FILE=" + ws + "/ansible/.vault_password ansible-galaxy collection install -r " + product + "/ansible/collections/requirements.yml --upgrade"
	if got := f.Calls[0].String(); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestProvisionDestroyNeedsTheWord(t *testing.T) {
	for _, c := range []struct {
		tags, extra []string
		// runTags is $ANSIBLE_RUN_TAGS, which Ansible adds to --tags.
		runTags string
	}{
		{[]string{"k3s-destroy"}, nil, ""},
		{[]string{"gateway", "k3s-destroy"}, nil, ""},
		{all, []string{"--tags=k3s-destroy"}, ""},
		{[]string{"all", "never"}, nil, ""},
		{[]string{"gateway"}, nil, "k3s-destroy"},
	} {
		ws, _ := setup(t)
		t.Setenv("ANSIBLE_RUN_TAGS", c.runTags)
		var out bytes.Buffer
		f := &runnertest.Fake{}
		err := run(context.Background(), f, term(&out, "y\n", false), &out, &out, ws, os.Getenv, options{}, c.tags, c.extra)
		if err == nil || err.Error() != "not confirmed: nothing ran" || len(f.Calls) != 0 {
			t.Errorf("%q %q, answer y: %v, calls %v", c.tags, c.extra, err, f.Calls)
		}
		if !strings.Contains(out.String(), "Type yes to go on") {
			t.Errorf("%q %q: no question for the word:\n%s", c.tags, c.extra, out.String())
		}
		tmpIsEmpty(t)

		if err := run(context.Background(), f, term(&out, "yes\n", false), &out, &out, ws, os.Getenv, options{}, c.tags, c.extra); err != nil || len(f.Calls) != 1 {
			t.Fatalf("%q %q, answer yes: %v, calls %v", c.tags, c.extra, err, f.Calls)
		}
		if !strings.Contains(f.Calls[0].String(), "--tags "+strings.Join(c.tags, ",")) {
			t.Errorf("ran %s", f.Calls[0])
		}
	}
}

func TestProvisionAsks(t *testing.T) {
	ws, _ := setup(t)
	var out bytes.Buffer
	f := &runnertest.Fake{}
	err := run(context.Background(), f, term(&out, "\n", false), &out, &out, ws, os.Getenv, options{}, all, nil)
	if err == nil || err.Error() != "not confirmed: nothing ran" || len(f.Calls) != 0 {
		t.Errorf("got %v, calls %v", err, f.Calls)
	}
	if !strings.Contains(out.String(), "Run install.yml with tags all on the hosts of "+ws+"/ansible/inventory?") {
		t.Errorf("question:\n%s", out.String())
	}
	tmpIsEmpty(t)
}

func TestProvisionWithoutATerminal(t *testing.T) {
	ws, _ := setup(t)
	f := &runnertest.Fake{}
	u := ui.New(ui.Options{Out: io.Discard, Err: io.Discard, Mode: ui.JSON})
	if err := run(context.Background(), f, u, io.Discard, io.Discard, ws, os.Getenv, options{}, all, nil); err == nil || !strings.Contains(err.Error(), "pass --yes") || len(f.Calls) != 0 {
		t.Errorf("got %v, calls %v", err, f.Calls)
	}
}

func TestProvisionDryRun(t *testing.T) {
	ws, product := setup(t)
	var out bytes.Buffer
	f := &runnertest.Fake{}
	if err := run(context.Background(), f, term(&out, "", false), &out, &out, ws, os.Getenv, options{dryRun: true, update: true}, all, nil); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Inventory " + ws + "/ansible/inventory",
		"Playbook " + product + "/ansible/install.yml, tags all",
		"Context lab (https://10.0.0.1:6443) for the k3s-users play",
		"Would run: ANSIBLE_CONFIG=" + product + "/ansible/ansible.cfg ANSIBLE_VAULT_PASSWORD_FILE=" + ws + "/ansible/.vault_password ansible-galaxy",
		`--tags all -e '{"K8S_AUTH_KUBECONFIG":"<a temporary kubeconfig holding context lab>"}'`,
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
	if len(f.Calls) != 0 {
		t.Errorf("ran %v", f.Calls)
	}
	tmpIsEmpty(t)

	if err := os.Remove(filepath.Join(product, "ansible", "ansible.cfg")); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := run(context.Background(), f, term(&out, "", false), &out, &out, ws, os.Getenv, options{dryRun: true}, all, nil); err != nil {
		t.Fatalf("dry run without the product's ansible.cfg: %v", err)
	}
	if !strings.Contains(out.String(), "warning: no ansible/ansible.cfg") {
		t.Errorf("no warning:\n%s", out.String())
	}
}

func TestProvisionMissingContext(t *testing.T) {
	ws, _ := setup(t)
	t.Setenv("KUBE_CONTEXT_NAME", "nope")
	var seen *clientcmdapi.Config
	f := &runnertest.Fake{Handle: func(c runner.Cmd) error {
		cfg, err := clientcmd.LoadFromFile(kubeconfigOf(t, c))
		seen = cfg
		return err
	}}
	var out bytes.Buffer
	if err := run(context.Background(), f, term(&out, "", true), &out, &out, ws, os.Getenv, options{yes: true}, all, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "no context nope in the kubeconfig") || !strings.Contains(out.String(), "ohmlab kubeconfig") {
		t.Errorf("no warning:\n%s", out.String())
	}
	if seen == nil || seen.CurrentContext != "" || len(seen.Contexts) != 0 {
		t.Errorf("the play got %+v", seen)
	}

	out.Reset()
	if err := run(context.Background(), f, term(&out, "", true), &out, &out, ws, os.Getenv, options{yes: true}, []string{"gateway"}, nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "no context") {
		t.Errorf("a warning for a run without the k3s-users play:\n%s", out.String())
	}
}

func TestProvisionProblems(t *testing.T) {
	for _, c := range []struct {
		name, want string
		breaks     func(ws, product string) error
		missing    []string
	}{
		{"no ansible.cfg", "pass --product", func(_, product string) error {
			return os.Remove(filepath.Join(product, "ansible", "ansible.cfg"))
		}, nil},
		{"no vault password file", "--vault-password-file", func(ws, _ string) error {
			return os.Remove(filepath.Join(ws, "ansible", ".vault_password"))
		}, nil},
		{"no inventory", "is not a directory", func(ws, _ string) error {
			return os.RemoveAll(filepath.Join(ws, "ansible", "inventory"))
		}, nil},
		{"no ansible-playbook", "ansible-playbook is not installed", func(string, string) error { return nil }, []string{"ansible-playbook"}},
	} {
		ws, product := setup(t)
		if err := c.breaks(ws, product); err != nil {
			t.Fatal(err)
		}
		f := &runnertest.Fake{Missing: c.missing}
		err := run(context.Background(), f, term(&bytes.Buffer{}, "", true), io.Discard, io.Discard, ws, os.Getenv, options{yes: true}, all, nil)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: got %v, want %q", c.name, err, c.want)
		}
		if len(f.Calls) != 0 {
			t.Errorf("%s: ran %v", c.name, f.Calls)
		}
	}
}

// Off a terminal the output is one JSON record per line: Ansible's own
// output goes to stderr, without colors.
func TestProvisionJSONOutput(t *testing.T) {
	ws, _ := setup(t)
	var out, errOut bytes.Buffer
	f := &runnertest.Fake{}
	u := ui.New(ui.Options{Out: &out, Err: &errOut, Mode: ui.JSON, Yes: true})
	if err := run(context.Background(), f, u, &out, &errOut, ws, os.Getenv, options{yes: true, noColor: true}, all, nil); err != nil {
		t.Fatal(err)
	}
	if len(f.Calls) != 1 || f.Calls[0].Stdout != io.Writer(&errOut) || f.Calls[0].Stderr != io.Writer(&errOut) {
		t.Fatalf("calls %+v", f.Calls)
	}
	if !slices.Contains(f.Calls[0].Env, "ANSIBLE_NOCOLOR=1") {
		t.Errorf("env %q", f.Calls[0].Env)
	}
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if !json.Valid([]byte(line)) {
			t.Errorf("not JSON: %s", line)
		}
	}
}

// helperArgs carries the command line to the child process of
// TestInterruptLeavesNoCopy.
const helperArgs = "OHMLAB_PROVISION_TEST_ARGS"

// Ctrl-C, or a hangup when the terminal goes, while the playbook runs still
// removes the kubeconfig copy.
func TestInterruptLeavesNoCopy(t *testing.T) {
	if args := os.Getenv(helperArgs); args != "" {
		Cmd.SetArgs(strings.Split(args, "\n"))
		_ = Cmd.Execute()
		os.Exit(0)
	}
	for _, sig := range []os.Signal{os.Interrupt, syscall.SIGHUP} {
		ws, _ := setup(t)
		bin, started := t.TempDir(), filepath.Join(t.TempDir(), "started")
		// A fake ansible-playbook: says it started, then hangs.
		if err := os.WriteFile(filepath.Join(bin, "ansible-playbook"), []byte("#!/bin/sh\ntouch \"$FAKE_STARTED\"\nexec sleep 30\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		child := exec.Command(os.Args[0], "-test.run=^TestInterruptLeavesNoCopy$")
		child.Dir = ws
		child.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "FAKE_STARTED="+started,
			helperArgs+"="+strings.Join([]string{"--yes", "--tags", "gateway"}, "\n"))
		if err := child.Start(); err != nil {
			t.Fatal(err)
		}
		defer child.Process.Kill()

		for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
			if _, err := os.Stat(started); err == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("%v: the fake ansible-playbook never started", sig)
			}
		}
		if err := child.Process.Signal(sig); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- child.Wait() }()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			t.Fatalf("%v: the command did not stop", sig)
		}
		if left, _ := filepath.Glob(filepath.Join(os.Getenv("TMPDIR"), "ohmlab-provision-*")); len(left) > 0 {
			t.Errorf("%v: left behind: %v", sig, left)
		}
	}
}

func TestSplitArgs(t *testing.T) {
	if _, err := splitArgs([]string{"destory"}, -1); err == nil || !strings.Contains(err.Error(), `unknown argument "destory"`) {
		t.Errorf("got %v, want unknown argument", err)
	}
	if extra, err := splitArgs([]string{"--limit", "w3"}, 0); err != nil || !slices.Equal(extra, []string{"--limit", "w3"}) {
		t.Errorf("got %q, %v", extra, err)
	}
	if extra, err := splitArgs(nil, -1); err != nil || extra != nil {
		t.Errorf("got %q, %v", extra, err)
	}
}

func TestShortcuts(t *testing.T) {
	for name, tag := range map[string]string{"destroy": "k3s-destroy", "upgrade-os": "os-upgrade"} {
		c, _, err := Cmd.Find([]string{name})
		if err != nil || c.Name() != name || c.Annotations["tags"] != tag {
			t.Errorf("%s: %v, %v, annotations %v", name, c.Name(), err, c.Annotations)
			continue
		}
		if c.Flags().Lookup("tags") != nil || c.InheritedFlags().Lookup("tags") != nil {
			t.Errorf("%s takes --tags", name)
		}
	}
	if Cmd.Flags().Lookup("tags") == nil {
		t.Error("provision has no --tags")
	}
}
