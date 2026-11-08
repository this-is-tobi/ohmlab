package sops

import (
	"context"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/this-is-tobi/ohmlab/cli/internal/runner"
	"github.com/this-is-tobi/ohmlab/cli/internal/runner/runnertest"
)

func write(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var n []string
	for _, e := range entries {
		n = append(n, e.Name())
	}
	return n
}

func TestFind(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{"a/x.enc.yaml", "b/c/y.enc.yaml", "a/.1.x.dec.yaml", "a/z.yaml"} {
		write(t, filepath.Join(root, p), "x")
	}
	got, err := Find(root, Encrypted)
	if want := []string{filepath.Join(root, "a/x.enc.yaml"), filepath.Join(root, "b/c/y.enc.yaml")}; err != nil || !slices.Equal(got, want) {
		t.Errorf("got %q, %v; want %q", got, err, want)
	}
	// What a killed run leaves is hidden: never taken for a file to encrypt.
	if got, err := Find(root, Decrypted); err != nil || len(got) != 0 {
		t.Errorf("got %q, %v; want none", got, err)
	}
	if _, err := Find(filepath.Join(root, "nope"), Encrypted); err == nil {
		t.Error("no error for a root that does not exist")
	}
}

func TestTarget(t *testing.T) {
	if got := Target("/a/x.enc.yaml"); got != "/a/x.dec.yaml" {
		t.Errorf("got %s", got)
	}
	if got := Target("/a/x.dec.yaml"); got != "/a/x.enc.yaml" {
		t.Errorf("got %s", got)
	}
}

func TestConvertDecrypt(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "x.enc.yaml")
	write(t, src, "sops: cipher\n")
	var tmp string
	f := &runnertest.Fake{Handle: func(c runner.Cmd) error {
		tmp = c.Stdout.(*os.File).Name()
		_, err := io.WriteString(c.Stdout, "plain: true\n")
		return err
	}}
	dst, err := Convert(context.Background(), f, true, src)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "x.dec.yaml"); dst != want || read(t, dst) != "plain: true\n" {
		t.Errorf("target %s: %q", dst, read(t, dst))
	}
	if st, err := os.Stat(dst); err != nil || st.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, %v; want 0600", st.Mode().Perm(), err)
	}
	if got := names(t, dir); !slices.Equal(got, []string{"x.dec.yaml", "x.enc.yaml"}) {
		t.Errorf("directory holds %q", got)
	}
	if len(f.Calls) != 1 || f.Calls[0].String() != "sops --decrypt "+src {
		t.Fatalf("calls %v", f.Calls)
	}
	// sops looks for .sops.yaml from its working directory up: the file's
	// own, whichever directory the command runs from.
	if f.Calls[0].Dir != dir {
		t.Errorf("sops runs in %q, want %q", f.Calls[0].Dir, dir)
	}
	// Hidden, and named like the target: .gitignore's *.dec.yaml covers the
	// plaintext a killed run would leave.
	if base := filepath.Base(tmp); !strings.HasPrefix(base, ".") {
		t.Errorf("temporary file %s is not hidden", base)
	} else if ok, _ := path.Match("*.dec.yaml", base); !ok {
		t.Errorf("temporary file %s does not match *.dec.yaml", base)
	}
}

func TestConvertFailureKeepsTheTarget(t *testing.T) {
	dir := t.TempDir()
	src, dst := filepath.Join(dir, "x.enc.yaml"), filepath.Join(dir, "x.dec.yaml")
	write(t, src, "sops: cipher\n")
	write(t, dst, "old")
	f := &runnertest.Fake{Handle: func(c runner.Cmd) error {
		_, _ = io.WriteString(c.Stdout, "partial")
		return &runner.ExitError{Name: "sops", Code: 128, Stderr: "no key"}
	}}
	if _, err := Convert(context.Background(), f, true, src); err == nil {
		t.Error("no error")
	}
	if got := read(t, dst); got != "old" {
		t.Errorf("target holds %q, want old", got)
	}
	if got := names(t, dir); !slices.Equal(got, []string{"x.dec.yaml", "x.enc.yaml"}) {
		t.Errorf("directory holds %q", got)
	}
}

// An emptied plaintext must never replace a good encrypted file.
func TestConvertEncryptRefusesAnEmptyFile(t *testing.T) {
	dir := t.TempDir()
	src, dst := filepath.Join(dir, "x.dec.yaml"), filepath.Join(dir, "x.enc.yaml")
	write(t, src, "")
	write(t, dst, "cipher")
	f := &runnertest.Fake{}
	if _, err := Convert(context.Background(), f, false, src); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Errorf("got %v, want an error naming empty", err)
	}
	if len(f.Calls) != 0 {
		t.Errorf("ran %v", f.Calls)
	}
	if got := read(t, dst); got != "cipher" {
		t.Errorf("target holds %q", got)
	}
}

func TestIgnored(t *testing.T) {
	file := filepath.Join(t.TempDir(), "x.dec.yaml")
	for _, c := range []struct {
		err     error
		want    bool
		wantErr bool
	}{
		{nil, true, false},
		{&runner.ExitError{Name: "git", Code: 1}, false, false},
		{&runner.ExitError{Name: "git", Code: 128, Stderr: "not a git repository"}, false, true},
	} {
		f := &runnertest.Fake{Handle: func(runner.Cmd) error { return c.err }}
		got, err := Ignored(context.Background(), f, file)
		if got != c.want || (err != nil) != c.wantErr {
			t.Errorf("git returning %v: %v, %v", c.err, got, err)
		}
		if want := "git -C " + filepath.Dir(file) + " check-ignore -q -- x.dec.yaml"; len(f.Calls) != 1 || f.Calls[0].String() != want {
			t.Errorf("calls %v, want %s", f.Calls, want)
		}
	}
}
