// Package runner runs the external tools the CLI drives (ansible, helm,
// sops) behind an interface tests replace, so they assert the exact command
// lines.
package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Cmd is one run of an external tool. Nil Stdout and Stderr discard the
// output, as os/exec does.
type Cmd struct {
	Name string
	Args []string
	// Env holds KEY=value pairs added to the process's environment.
	Env    []string
	Dir    string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// String is the command line as a shell reads it, the environment first:
// what --dry-run prints.
func (c Cmd) String() string {
	parts := make([]string, 0, len(c.Env)+1+len(c.Args))
	for _, e := range c.Env {
		k, v, _ := strings.Cut(e, "=")
		parts = append(parts, k+"="+quote(v))
	}
	parts = append(parts, quote(c.Name))
	for _, a := range c.Args {
		parts = append(parts, quote(a))
	}
	return strings.Join(parts, " ")
}

// quote returns s bare when a shell would read it unchanged, else in single
// quotes.
func quote(s string) string {
	if s == "" {
		return "''"
	}
	if strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("@%_+=:,./-", r))
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

// Runner runs external tools.
type Runner interface {
	Run(ctx context.Context, c Cmd) error
	LookPath(name string) (string, error)
}

// Exec runs the tools with os/exec.
type Exec struct{}

// stderrTail is how much of a failed tool's stderr ExitError keeps.
const stderrTail = 2 << 10

// waitDelay bounds the wait for a tool's output once it has exited, or has
// been killed on cancel: a child it leaves behind (ssh's ControlPersist
// master) may hold the output open long after.
const waitDelay = 2 * time.Second

// Run runs c and waits for it. A tool that exits non-zero returns an
// *ExitError with the end of its stderr.
func (Exec) Run(ctx context.Context, c Cmd) error {
	cmd := exec.CommandContext(ctx, c.Name, c.Args...)
	cmd.Dir, cmd.Stdin, cmd.Stdout = c.Dir, c.Stdin, c.Stdout
	if len(c.Env) > 0 {
		cmd.Env = append(os.Environ(), c.Env...)
	}
	tail := &tailWriter{max: stderrTail}
	cmd.Stderr = tail
	if c.Stderr != nil {
		cmd.Stderr = io.MultiWriter(c.Stderr, tail)
	}
	cmd.WaitDelay = waitDelay
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case errors.As(err, &exit):
		return &ExitError{Name: c.Name, Code: exit.ExitCode(), Stderr: string(tail.b)}
	case errors.Is(err, exec.ErrWaitDelay):
		// The tool succeeded; only a child of its kept the output open.
		return nil
	}
	return err
}

// LookPath finds a tool in PATH.
func (Exec) LookPath(name string) (string, error) {
	return exec.LookPath(name)
}

// ExitError is a tool that exited non-zero.
type ExitError struct {
	Name string
	Code int
	// Stderr is the end of the tool's stderr.
	Stderr string
}

func (e *ExitError) Error() string {
	msg := fmt.Sprintf("%s exited %d", e.Name, e.Code)
	if s := strings.TrimSpace(e.Stderr); s != "" {
		msg += ": " + s
	}
	return msg
}

// tailWriter keeps the last max bytes written to it.
type tailWriter struct {
	max int
	b   []byte
}

func (t *tailWriter) Write(p []byte) (int, error) {
	t.b = append(t.b, p...)
	if over := len(t.b) - t.max; over > 0 {
		t.b = append(t.b[:0], t.b[over:]...)
	}
	return len(p), nil
}
