// Package sops converts the Sops files of a workspace with the sops tool,
// never truncating a good file: sops writes to a temporary file, renamed over
// the target only once sops succeeded.
package sops

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/this-is-tobi/ohmlab/cli/internal/runner"
)

const (
	// Encrypted is the suffix of the files committed.
	Encrypted = ".enc.yaml"
	// Decrypted is the suffix of their plaintext, which git must ignore.
	Decrypted = ".dec.yaml"
)

// Find returns the files under root whose name ends in suffix, in lexical
// order. Hidden ones are left out: a temporary file a killed run left behind
// is one.
func Find(root, suffix string) ([]string, error) {
	var found []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() && strings.HasSuffix(d.Name(), suffix) && !strings.HasPrefix(d.Name(), ".") {
			found = append(found, p)
		}
		return nil
	})
	return found, err
}

// Target is the file src converts to: x.enc.yaml and x.dec.yaml, both ways.
func Target(src string) string {
	if base, ok := strings.CutSuffix(src, Encrypted); ok {
		return base + Decrypted
	}
	return strings.TrimSuffix(src, Decrypted) + Encrypted
}

// Convert decrypts or encrypts src into its target and returns the target.
// An empty file is never encrypted: it would replace a good encrypted one.
func Convert(ctx context.Context, r runner.Runner, decrypt bool, src string) (string, error) {
	dst, mode := Target(src), "--decrypt"
	if !decrypt {
		mode = "--encrypt"
		st, err := os.Stat(src)
		if err != nil {
			return "", err
		}
		if st.Size() == 0 {
			return "", fmt.Errorf("%s: empty file, not encrypted", src)
		}
	}
	// Hidden, and named like the target: a *.dec.yaml rule in .gitignore
	// covers the plaintext a killed run leaves, and Find skips it.
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".*."+filepath.Base(dst))
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	// sops looks for .sops.yaml from its working directory up: the file's
	// own, so another repository's rules never apply.
	err = r.Run(ctx, runner.Cmd{Name: "sops", Args: []string{mode, src}, Dir: filepath.Dir(src), Stdout: tmp})
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", fmt.Errorf("%s: %w", src, err)
	}
	return dst, os.Rename(tmp.Name(), dst)
}

// Ignored reports whether git ignores file. Outside a repository it is an
// error.
func Ignored(ctx context.Context, r runner.Runner, file string) (bool, error) {
	err := r.Run(ctx, runner.Cmd{Name: "git", Args: []string{"-C", filepath.Dir(file), "check-ignore", "-q", "--", filepath.Base(file)}})
	var exit *runner.ExitError
	switch {
	case err == nil:
		return true, nil
	case errors.As(err, &exit) && exit.Code == 1:
		return false, nil
	}
	return false, err
}
