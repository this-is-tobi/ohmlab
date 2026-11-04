package ui

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

// clock moves 1.2s at every call, so each step lasts 1.2s.
func clock() func() time.Time {
	t := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	n := 0
	return func() time.Time {
		defer func() { n++ }()
		return t.Add(time.Duration(n) * 1200 * time.Millisecond)
	}
}

// records parses one JSON object per line, without the time and level.
func records(t *testing.T, s string) []map[string]any {
	t.Helper()
	var rs []map[string]any
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		var r map[string]any
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatalf("not a JSON object: %q", sc.Text())
		}
		delete(r, "time")
		delete(r, "level")
		rs = append(rs, r)
	}
	return rs
}

func TestStepHuman(t *testing.T) {
	var out, errOut bytes.Buffer
	u := New(Options{Out: &out, Err: &errOut, Mode: Human, Now: clock()})
	if err := u.Step("Fetch").Done(nil); err != nil {
		t.Fatal(err)
	}
	if got, want := out.String(), "→ Fetch\n✓ Fetch (1.2s)\n"; got != want {
		t.Errorf("out %q, want %q", got, want)
	}
	out.Reset()
	boom := errors.New("boom")
	if err := u.Step("Fetch").Done(boom); err != boom {
		t.Errorf("Done returned %v, want the step's error", err)
	}
	if got, want := out.String(), "→ Fetch\n"; got != want {
		t.Errorf("out %q, want %q", got, want)
	}
	if got, want := errOut.String(), "✗ Fetch (1.2s): boom\n"; got != want {
		t.Errorf("err %q, want %q", got, want)
	}
}

func TestStepJSON(t *testing.T) {
	var out, errOut bytes.Buffer
	u := New(Options{Out: &out, Err: &errOut, Mode: JSON, Now: clock()})
	_ = u.Step("Fetch").Done(nil)
	_ = u.Step("Fetch").Done(errors.New("boom"))
	want := []map[string]any{
		{"msg": "step", "step": "Fetch", "status": "start"},
		{"msg": "step", "step": "Fetch", "status": "ok", "durationMs": 1200.0},
		{"msg": "step", "step": "Fetch", "status": "start"},
		{"msg": "step", "step": "Fetch", "status": "failed", "durationMs": 1200.0, "error": "boom"},
	}
	if got := records(t, out.String()); !reflect.DeepEqual(got, want) {
		t.Errorf("records %v, want %v", got, want)
	}
	if errOut.Len() != 0 {
		t.Errorf("JSON mode wrote %q to Err", errOut.String())
	}
}

func TestNoColorWithoutTerminal(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	if mode, color := Detect(w, false); mode != JSON || color {
		t.Errorf("Detect on a pipe: %v, %v; want JSON, false", mode, color)
	}

	if mode, color := detect(true, false); mode != Human || !color {
		t.Errorf("a terminal: %v, %v; want Human, true", mode, color)
	}
	if _, color := detect(true, true); color {
		t.Error("--no-color on a terminal: color on")
	}
	t.Setenv("NO_COLOR", "1")
	mode, color := detect(true, false)
	if mode != Human || color {
		t.Errorf("NO_COLOR on a terminal: %v, %v; want Human, false", mode, color)
	}
	var out bytes.Buffer
	u := New(Options{Out: &out, Err: &out, Mode: mode, Color: color, Now: clock()})
	_ = u.Step("Fetch").Done(nil)
	_ = u.Step("Fetch").Done(errors.New("boom"))
	u.Warn("careful")
	if strings.Contains(out.String(), "\x1b[") {
		t.Errorf("escape codes without color: %q", out.String())
	}
}

func TestConfirmDefaultsToNo(t *testing.T) {
	for in, want := range map[string]bool{"\n": false, "y\n": true, "yes\n": true, "n\n": false} {
		var errOut bytes.Buffer
		u := New(Options{Out: io.Discard, Err: &errOut, In: strings.NewReader(in), Mode: Human})
		got, err := u.Confirm("Replace context lab?")
		if err != nil || got != want {
			t.Errorf("answer %q: %v, %v; want %v", in, got, err, want)
		}
		if !strings.HasSuffix(errOut.String(), "[y/N] ") {
			t.Errorf("question %q does not end with [y/N] ", errOut.String())
		}
	}
}

func TestConfirmWithYesAsksNothing(t *testing.T) {
	var out, errOut bytes.Buffer
	u := New(Options{Out: &out, Err: &errOut, In: strings.NewReader(""), Mode: Human, Yes: true})
	if got, err := u.Confirm("Replace context lab?"); err != nil || !got {
		t.Errorf("got %v, %v; want true", got, err)
	}
	if out.Len()+errOut.Len() != 0 {
		t.Errorf("wrote %q and %q", out.String(), errOut.String())
	}
}

func TestConfirmWithoutTerminalIsAnError(t *testing.T) {
	u := New(Options{Out: io.Discard, Err: io.Discard, Mode: JSON})
	if _, err := u.Confirm("Replace context lab?"); err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Errorf("got %v, want an error naming --yes", err)
	}
}
