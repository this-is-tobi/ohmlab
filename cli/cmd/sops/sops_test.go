package sops

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/this-is-tobi/ohmlab/cli/internal/runner"
	"github.com/this-is-tobi/ohmlab/cli/internal/runner/runnertest"
	"github.com/this-is-tobi/ohmlab/cli/internal/ui"
)

// setup makes a workspace (ohmlab.yaml) holding the given files under it.
func setup(t *testing.T, files map[string]string) string {
	t.Helper()
	ws := t.TempDir()
	files["ohmlab.yaml"] = ""
	for p, content := range files {
		p = filepath.Join(ws, p)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(ws, "argo-cd"), 0o755); err != nil {
		t.Fatal(err)
	}
	return ws
}

// fakeSops writes what sops would, fails for the sources named in fail, and
// says every file is git-ignored.
func fakeSops(fail ...string) *runnertest.Fake {
	return &runnertest.Fake{Handle: func(c runner.Cmd) error {
		if c.Name != "sops" {
			return nil
		}
		src := c.Args[len(c.Args)-1]
		for _, f := range fail {
			if strings.HasSuffix(src, f) {
				return &runner.ExitError{Name: "sops", Code: 128, Stderr: "no key"}
			}
		}
		_, err := io.WriteString(c.Stdout, "converted from "+filepath.Base(src)+"\n")
		return err
	}}
}

func term(out *bytes.Buffer) *ui.UI {
	return ui.New(ui.Options{Out: out, Err: out, In: strings.NewReader(""), Mode: ui.Human})
}

func sopsCalls(f *runnertest.Fake) int {
	n := 0
	for _, c := range f.Calls {
		if c.Name == "sops" {
			n++
		}
	}
	return n
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func TestDecryptAll(t *testing.T) {
	ws := setup(t, map[string]string{"argo-cd/a/x.enc.yaml": "c", "argo-cd/b/y.enc.yaml": "c"})
	var out bytes.Buffer
	f := fakeSops()
	if err := run(context.Background(), f, term(&out), ws, os.Getenv, options{}, true); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	for _, name := range []string{"a/x", "b/y"} {
		if b, err := os.ReadFile(filepath.Join(ws, "argo-cd", name+".dec.yaml")); err != nil || string(b) != "converted from "+filepath.Base(name)+".enc.yaml\n" {
			t.Errorf("%s.dec.yaml: %q, %v", name, b, err)
		}
		if want := "argo-cd/" + name + ".enc.yaml -> argo-cd/" + name + ".dec.yaml"; !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
}

func TestDecryptCountsFailures(t *testing.T) {
	ws := setup(t, map[string]string{"argo-cd/x.enc.yaml": "c", "argo-cd/y.enc.yaml": "c"})
	err := run(context.Background(), fakeSops("x.enc.yaml"), term(&bytes.Buffer{}), ws, os.Getenv, options{}, true)
	if err == nil || err.Error() != "1 file(s) failed to decrypt" {
		t.Errorf("got %v", err)
	}
	if exists(filepath.Join(ws, "argo-cd", "x.dec.yaml")) || !exists(filepath.Join(ws, "argo-cd", "y.dec.yaml")) {
		t.Error("want y decrypted and x not")
	}
}

func TestEncryptSkipsEmpty(t *testing.T) {
	ws := setup(t, map[string]string{"argo-cd/x.dec.yaml": "", "argo-cd/y.dec.yaml": "plain"})
	f := fakeSops()
	err := run(context.Background(), f, term(&bytes.Buffer{}), ws, os.Getenv, options{}, false)
	if err == nil || err.Error() != "1 file(s) failed to encrypt" {
		t.Errorf("got %v", err)
	}
	if sopsCalls(f) != 1 || exists(filepath.Join(ws, "argo-cd", "x.enc.yaml")) || !exists(filepath.Join(ws, "argo-cd", "y.enc.yaml")) {
		t.Errorf("want y encrypted alone, calls %v", f.Calls)
	}
}

func TestDryRun(t *testing.T) {
	ws := setup(t, map[string]string{"argo-cd/x.enc.yaml": "c"})
	var out bytes.Buffer
	f := fakeSops()
	if err := run(context.Background(), f, term(&out), ws, os.Getenv, options{dryRun: true}, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Would decrypt argo-cd/x.enc.yaml -> argo-cd/x.dec.yaml") {
		t.Errorf("output:\n%s", out.String())
	}
	if len(f.Calls) != 0 || exists(filepath.Join(ws, "argo-cd", "x.dec.yaml")) {
		t.Errorf("changed something: calls %v", f.Calls)
	}
}

func TestWarnsWhenNotIgnored(t *testing.T) {
	ws := setup(t, map[string]string{"argo-cd/x.enc.yaml": "c"})
	var out bytes.Buffer
	f := fakeSops()
	sops := f.Handle
	f.Handle = func(c runner.Cmd) error {
		if c.Name == "git" {
			return &runner.ExitError{Name: "git", Code: 1}
		}
		return sops(c)
	}
	if err := run(context.Background(), f, term(&out), ws, os.Getenv, options{}, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "argo-cd/x.dec.yaml is not git-ignored") {
		t.Errorf("no warning:\n%s", out.String())
	}
}

func TestNoSops(t *testing.T) {
	ws := setup(t, map[string]string{"argo-cd/x.enc.yaml": "c"})
	f := &runnertest.Fake{Missing: []string{"sops"}}
	if err := run(context.Background(), f, term(&bytes.Buffer{}), ws, os.Getenv, options{}, true); err == nil || !strings.Contains(err.Error(), "sops is not installed") {
		t.Errorf("got %v", err)
	}
	if len(f.Calls) != 0 {
		t.Errorf("ran %v", f.Calls)
	}
}

func TestNothingToDo(t *testing.T) {
	ws := setup(t, map[string]string{})
	var out bytes.Buffer
	f := fakeSops()
	if err := run(context.Background(), f, term(&out), ws, os.Getenv, options{}, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "No *.enc.yaml under") || len(f.Calls) != 0 {
		t.Errorf("output %q, calls %v", out.String(), f.Calls)
	}
}

func TestDir(t *testing.T) {
	ws := setup(t, map[string]string{"argo-cd/x.enc.yaml": "c", "other/y.enc.yaml": "c"})
	if err := run(context.Background(), fakeSops(), term(&bytes.Buffer{}), ws, os.Getenv, options{dir: "other"}, true); err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(ws, "argo-cd", "x.dec.yaml")) || !exists(filepath.Join(ws, "other", "y.dec.yaml")) {
		t.Error("want other/ converted and argo-cd/ not")
	}
}

// helperArgs carries the command line to the child process of
// TestInterruptLeavesNoCopy.
const helperArgs = "OHMLAB_SOPS_TEST_ARGS"

// Ctrl-C, or a hangup when the terminal goes, while sops runs leaves no
// temporary copy of the plaintext.
func TestInterruptLeavesNoCopy(t *testing.T) {
	if args := os.Getenv(helperArgs); args != "" {
		Cmd.SetArgs(strings.Split(args, "\n"))
		_ = Cmd.Execute()
		os.Exit(0)
	}
	for _, sig := range []os.Signal{os.Interrupt, syscall.SIGHUP} {
		ws := setup(t, map[string]string{"argo-cd/x.enc.yaml": "c"})
		bin, started := t.TempDir(), filepath.Join(t.TempDir(), "started")
		// A fake sops: writes part of the plaintext, says it started, then hangs.
		if err := os.WriteFile(filepath.Join(bin, "sops"), []byte("#!/bin/sh\nprintf partial\ntouch \"$FAKE_STARTED\"\nexec sleep 30\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		child := exec.Command(os.Args[0], "-test.run=^TestInterruptLeavesNoCopy$")
		child.Dir = ws
		child.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "FAKE_STARTED="+started, helperArgs+"=decrypt")
		if err := child.Start(); err != nil {
			t.Fatal(err)
		}
		defer child.Process.Kill()

		for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
			if _, err := os.Stat(started); err == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("%v: the fake sops never started", sig)
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
		entries, err := os.ReadDir(filepath.Join(ws, "argo-cd"))
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 {
			var names []string
			for _, e := range entries {
				names = append(names, e.Name())
			}
			t.Errorf("%v: argo-cd holds %q, want x.enc.yaml alone", sig, names)
		}
	}
}

// An interrupt stops the run at the file it is on.
func TestInterruptStops(t *testing.T) {
	ws := setup(t, map[string]string{"argo-cd/x.enc.yaml": "c", "argo-cd/y.enc.yaml": "c"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := &runnertest.Fake{Handle: func(c runner.Cmd) error {
		if c.Name != "sops" {
			return nil
		}
		cancel()
		return errors.New("interrupted")
	}}
	if err := run(ctx, f, term(&bytes.Buffer{}), ws, os.Getenv, options{}, true); err == nil {
		t.Error("no error")
	}
	if sopsCalls(f) != 1 {
		t.Errorf("sops ran %d times after the interrupt, want 1", sopsCalls(f))
	}
}
