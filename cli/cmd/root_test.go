package cmd

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"testing"
)

// Jobs and the controller read these logs: off a terminal they stay slog JSON.
func TestLoggerIsJSONWithoutTerminal(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, prev := os.Stdout, slog.Default()
	os.Stdout = w
	defer func() { os.Stdout = stdout; slog.SetDefault(prev) }()

	initLogger()
	slog.Info("x", "k", "v")
	w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	var rec map[string]any
	if err := json.Unmarshal(out, &rec); err != nil {
		t.Fatalf("not JSON: %q", out)
	}
	if rec["msg"] != "x" || rec["k"] != "v" || rec["level"] != "INFO" {
		t.Errorf("record %v, want msg x, k v, level INFO", rec)
	}
}

func TestNoColorFlagExists(t *testing.T) {
	if rootCmd.PersistentFlags().Lookup("no-color") == nil {
		t.Error("no persistent --no-color flag")
	}
}
