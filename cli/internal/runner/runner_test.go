package runner_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/this-is-tobi/ohmlab/cli/internal/runner"
	"github.com/this-is-tobi/ohmlab/cli/internal/runner/runnertest"
)

func TestCmdString(t *testing.T) {
	c := runner.Cmd{
		Name: "ansible",
		Args: []string{"masters[0]", "-a", "src=/a dest=/b flat=true"},
		Env:  []string{"ANSIBLE_CONFIG=/p/ansible.cfg"},
	}
	if got, want := c.String(), `ANSIBLE_CONFIG=/p/ansible.cfg ansible 'masters[0]' -a 'src=/a dest=/b flat=true'`; got != want {
		t.Errorf("got %s\nwant %s", got, want)
	}
	if got, want := (runner.Cmd{Name: "echo", Args: []string{"it's", ""}}).String(), `echo 'it'"'"'s' ''`; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestExecRunsAndPassesEnv(t *testing.T) {
	var out bytes.Buffer
	err := runner.Exec{}.Run(context.Background(), runner.Cmd{Name: "sh", Args: []string{"-c", `printf %s "$X"`}, Env: []string{"X=1"}, Stdout: &out})
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != "1" {
		t.Errorf("stdout %q, want 1", out.String())
	}
}

func TestExecExitError(t *testing.T) {
	var stderr bytes.Buffer
	err := runner.Exec{}.Run(context.Background(), runner.Cmd{Name: "sh", Args: []string{"-c", "echo no >&2; exit 3"}, Stderr: &stderr})
	var exit *runner.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("got %v, want an *ExitError", err)
	}
	if exit.Code != 3 || exit.Stderr != "no\n" || exit.Name != "sh" {
		t.Errorf("got %+v, want sh, code 3, stderr no", exit)
	}
	if got, want := exit.Error(), "sh exited 3: no"; got != want {
		t.Errorf("Error() %q, want %q", got, want)
	}
	if stderr.String() != "no\n" {
		t.Errorf("the caller's Stderr got %q", stderr.String())
	}
}

func TestFakeRecordsCalls(t *testing.T) {
	boom := errors.New("boom")
	f := &runnertest.Fake{
		Handle: func(c runner.Cmd) error {
			if c.Name == "helm" {
				return boom
			}
			return nil
		},
		Missing: []string{"helm"},
	}
	var r runner.Runner = f
	if err := r.Run(context.Background(), runner.Cmd{Name: "ansible", Args: []string{"all"}}); err != nil {
		t.Fatal(err)
	}
	if err := r.Run(context.Background(), runner.Cmd{Name: "helm"}); err != boom {
		t.Errorf("got %v, want the handler's error", err)
	}
	if len(f.Calls) != 2 || f.Calls[0].String() != "ansible all" || f.Calls[1].Name != "helm" {
		t.Errorf("calls %v", f.Calls)
	}
	if _, err := r.LookPath("helm"); err == nil {
		t.Error("LookPath found a missing tool")
	}
	if _, err := r.LookPath("ansible"); err != nil {
		t.Errorf("LookPath(ansible): %v", err)
	}
}

// ssh's ControlPersist master outlives ansible holding its output: the run
// must end when the tool does.
func TestExecDoesNotWaitForAChildHoldingTheOutput(t *testing.T) {
	var out bytes.Buffer
	start := time.Now()
	err := runner.Exec{}.Run(context.Background(), runner.Cmd{Name: "sh", Args: []string{"-c", "sleep 20 & echo done"}, Stdout: &out})
	if err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took > 10*time.Second {
		t.Errorf("took %v, waiting for the child", took)
	}
	if out.String() != "done\n" {
		t.Errorf("stdout %q", out.String())
	}
}

func TestExecInterruptsAToolGivenGrace(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "stopped")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runner.Exec{}.Run(ctx, runner.Cmd{Name: "sh", Args: []string{"-c",
			`trap 'kill $!; echo stopped > "$0"; exit 3' INT TERM; sleep 30 & wait`, marker}, Grace: 5 * time.Second})
	}()
	time.Sleep(300 * time.Millisecond) // the shell installs its trap
	cancel()
	var err error
	select {
	case err = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after the cancel")
	}
	if b, rerr := os.ReadFile(marker); rerr != nil || strings.TrimSpace(string(b)) != "stopped" {
		t.Errorf("the tool was not let stop by itself: %v %q", rerr, b)
	}
	if err == nil || !strings.Contains(err.Error(), "sh was interrupted") {
		t.Errorf("got %v, want an error saying sh was interrupted", err)
	}
}
