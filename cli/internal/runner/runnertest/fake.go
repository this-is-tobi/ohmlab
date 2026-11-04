// Package runnertest is a runner.Runner for tests: it records every command
// and lets the test decide what each one does.
package runnertest

import (
	"context"
	"os/exec"
	"slices"

	"github.com/this-is-tobi/ohmlab/cli/internal/runner"
)

// Fake records the commands it is given.
type Fake struct {
	// Handle does what the command would; nil succeeds.
	Handle func(runner.Cmd) error
	Calls  []runner.Cmd
	// Missing are the tools LookPath does not find.
	Missing []string
}

func (f *Fake) Run(_ context.Context, c runner.Cmd) error {
	f.Calls = append(f.Calls, c)
	if f.Handle == nil {
		return nil
	}
	return f.Handle(c)
}

func (f *Fake) LookPath(name string) (string, error) {
	if slices.Contains(f.Missing, name) {
		return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
	}
	return "/usr/bin/" + name, nil
}
