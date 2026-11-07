// Package ui writes what a command does: steps, notes and questions for a
// person on a terminal, JSON records for a pod, a pipe or a file.
package ui

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"golang.org/x/term"
)

// Mode is who reads the output.
type Mode int

const (
	// Human is a person on a terminal.
	Human Mode = iota
	// JSON is a program: one slog JSON record per line.
	JSON
)

// Options configures a UI. Nil writers, reader and clock default to the
// process's standard streams and time.Now.
type Options struct {
	Out, Err io.Writer
	In       io.Reader
	Mode     Mode
	Color    bool
	// Yes answers every question with yes, without asking.
	Yes bool
	Now func() time.Time
}

// UI is the output of one command.
type UI struct {
	o   Options
	in  *bufio.Reader
	log *slog.Logger
}

// New returns a UI writing as o says.
func New(o Options) *UI {
	if o.Out == nil {
		o.Out = os.Stdout
	}
	if o.Err == nil {
		o.Err = os.Stderr
	}
	if o.In == nil {
		o.In = os.Stdin
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return &UI{o: o, in: bufio.NewReader(o.In), log: slog.New(slog.NewJSONHandler(o.Out, nil))}
}

// Human reports whether a person reads the output, rather than a program.
func (u *UI) Human() bool { return u.o.Mode == Human }

// Detect chooses the mode from out: Human on a terminal, JSON otherwise.
// Color needs a terminal, no --no-color and an empty NO_COLOR.
func Detect(out *os.File, noColor bool) (mode Mode, color bool) {
	return detect(term.IsTerminal(int(out.Fd())), noColor)
}

func detect(terminal, noColor bool) (Mode, bool) {
	if !terminal {
		return JSON, false
	}
	return Human, !noColor && os.Getenv("NO_COLOR") == ""
}

// Step is one stage of a command, timed from Step to Done.
type Step struct {
	u     *UI
	title string
	start time.Time
}

// Step announces a stage.
func (u *UI) Step(title string) *Step {
	s := &Step{u: u, title: title, start: u.o.Now()}
	if u.o.Mode == JSON {
		u.log.Info("step", "step", title, "status", "start")
	} else {
		fmt.Fprintf(u.o.Out, "→ %s\n", title)
	}
	return s
}

// Done ends the stage, failed when err is not nil, and returns err.
func (s *Step) Done(err error) error {
	u, d := s.u, s.u.o.Now().Sub(s.start)
	switch {
	case u.o.Mode == JSON && err != nil:
		u.log.Error("step", "step", s.title, "status", "failed", "durationMs", d.Milliseconds(), "error", err.Error())
	case u.o.Mode == JSON:
		u.log.Info("step", "step", s.title, "status", "ok", "durationMs", d.Milliseconds())
	case err != nil:
		fmt.Fprintf(u.o.Err, "%s %s (%s): %v\n", paint(u.o.Color, red, "✗"), s.title, d.Round(100*time.Millisecond), err)
	default:
		fmt.Fprintf(u.o.Out, "%s %s (%s)\n", paint(u.o.Color, green, "✓"), s.title, d.Round(100*time.Millisecond))
	}
	return err
}

// Info writes a note.
func (u *UI) Info(format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	if u.o.Mode == JSON {
		u.log.Info(msg)
		return
	}
	fmt.Fprintln(u.o.Out, msg)
}

// Warn writes a warning.
func (u *UI) Warn(format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	if u.o.Mode == JSON {
		u.log.Warn(msg)
		return
	}
	fmt.Fprintf(u.o.Err, "%s%s\n", paint(u.o.Color, yellow, "warning: "), msg)
}

// Confirm asks a yes/no question, No by default. Yes answers it without
// asking; with no terminal to ask on, it is an error rather than a guess.
func (u *UI) Confirm(question string) (bool, error) {
	if u.o.Yes {
		return true, nil
	}
	if u.o.Mode != Human {
		return false, fmt.Errorf("%s: no terminal to ask on, pass --yes to answer yes", question)
	}
	fmt.Fprintf(u.o.Err, "%s [y/N] ", question)
	line, err := u.in.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	}
	return false, nil
}

const (
	red    = "31"
	green  = "32"
	yellow = "33"
)

func paint(color bool, code, s string) string {
	if !color {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}
