package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/this-is-tobi/ohmlab/utils/internal/buildinfo"
)

func runArgs(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := run(context.Background(), args, &out, &errb)
	return code, out.String(), errb.String()
}

func TestVersion(t *testing.T) {
	buildinfo.Version, buildinfo.Commit = "1.2.3", "abc"
	code, out, _ := runArgs(t, "--version")
	if code != 0 || !strings.Contains(out, "ohmlab-controller 1.2.3 (commit abc") {
		t.Errorf("code=%d out=%q", code, out)
	}
}

func TestAnUnknownFlagIsRefused(t *testing.T) {
	if code, _, _ := runArgs(t, "--nope"); code != 2 {
		t.Errorf("code = %d", code)
	}
}

func TestConfigurationProblemsStopTheProcessWithAMessage(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.yaml")
	_ = os.WriteFile(good, []byte("leaseNamespace: ohmlab-system\nvault:\n  watchNamespaces: [a]\n  backend:\n    address: https://v:8200\n    auth: {role: r}\n"), 0o600)
	typo := filepath.Join(dir, "typo.yaml")
	_ = os.WriteFile(typo, []byte("vualt: {}\n"), 0o600)

	tests := []struct {
		name string
		args []string
		want string
	}{
		{"no such file", []string{"--config", filepath.Join(dir, "missing.yaml"), "--target", "vault"}, "read config"},
		{"a typo", []string{"--config", typo, "--target", "vault"}, "parse config"},
		{"no target", []string{"--config", good}, "no target"},
		{"unknown target", []string{"--config", good, "--target", "harbor"}, "unknown target"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, _, errOut := runArgs(t, tt.args...)
			if code != 1 || !strings.Contains(errOut, tt.want) {
				t.Errorf("code=%d stderr=%q, want %q", code, errOut, tt.want)
			}
		})
	}
}

func TestSplitList(t *testing.T) {
	got := splitList(" vault, ,harbor ")
	if len(got) != 2 || got[0] != "vault" || got[1] != "harbor" {
		t.Errorf("got %v", got)
	}
}
