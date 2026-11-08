// Package sops is `ohmlab sops`.
package sops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/this-is-tobi/ohmlab/cli/internal/config"
	"github.com/this-is-tobi/ohmlab/cli/internal/runner"
	"github.com/this-is-tobi/ohmlab/cli/internal/sops"
	"github.com/this-is-tobi/ohmlab/cli/internal/ui"
)

// Cmd is `ohmlab sops`.
var Cmd = &cobra.Command{
	Use:   "sops",
	Short: "Decrypt or encrypt the workspace's Sops files",
	Long: `For instances that commit Sops-encrypted manifests (optional): decrypt every
*.enc.yaml under the workspace's argo-cd directory (--dir for another) to
*.dec.yaml, which .gitignore must cover, or encrypt them back. sops reads the
keys and .sops.yaml as usual. sops writes to a temporary file, which replaces
the target only once it succeeded, and an empty *.dec.yaml is never encrypted.
--dry-run lists the files and changes nothing. Needs sops.`,
}

type options struct {
	workspace config.Flags
	dir       string
	dryRun    bool
}

var flags options

func init() {
	pf := Cmd.PersistentFlags()
	config.AddFlags(pf, &flags.workspace)
	// Only the workspace file says where the files are.
	for _, name := range []string{"product", "instances", "instance", "inventory", "vault-password-file"} {
		_ = pf.MarkHidden(name)
	}
	pf.StringVar(&flags.dir, "dir", "", "the directory holding the files (default: argo-cd in the workspace)")
	pf.BoolVar(&flags.dryRun, "dry-run", false, "list the files, and change nothing")
	Cmd.AddCommand(
		&cobra.Command{Use: "decrypt", Short: "Decrypt every *.enc.yaml to *.dec.yaml", Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error { return runE(cmd, true) }},
		&cobra.Command{Use: "encrypt", Short: "Encrypt every *.dec.yaml to *.enc.yaml", Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error { return runE(cmd, false) }},
	)
}

func runE(cmd *cobra.Command, decrypt bool) error {
	// An interrupt, or a hangup when the terminal goes, cancels the run
	// rather than killing the process, so the file being converted leaves no
	// temporary copy.
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	noColor, _ := cmd.Flags().GetBool("no-color")
	mode, color := ui.Detect(os.Stdout, noColor)
	return run(ctx, runner.Exec{}, ui.New(ui.Options{Mode: mode, Color: color}), cwd, os.Getenv, flags, decrypt)
}

func run(ctx context.Context, r runner.Runner, u *ui.UI, cwd string, getenv func(string) string, o options, decrypt bool) error {
	s, _, err := config.Resolve(cwd, o.workspace, getenv)
	if err != nil {
		return err
	}
	root := filepath.Join(s.Workspace, "argo-cd")
	if o.dir != "" {
		root = o.dir
		if !filepath.IsAbs(root) {
			root = filepath.Join(cwd, root)
		}
	}
	verb, from := "encrypt", sops.Decrypted
	if decrypt {
		verb, from = "decrypt", sops.Encrypted
	}
	if !o.dryRun {
		if _, err := r.LookPath("sops"); err != nil {
			return errors.New("sops is not installed: it converts the files with the keys it finds")
		}
	}
	files, err := sops.Find(root, from)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		u.Info("No *%s under %s", from, root)
		return nil
	}
	rel := func(p string) string {
		if r, err := filepath.Rel(s.Workspace, p); err == nil {
			return r
		}
		return p
	}
	if o.dryRun {
		for _, f := range files {
			u.Info("Would %s %s -> %s", verb, rel(f), rel(sops.Target(f)))
		}
		return nil
	}

	failed := 0
	// plain are the plaintext files of the run, which git must not commit.
	var plain []string
	if !decrypt {
		plain = files
	}
	for _, f := range files {
		dst, err := sops.Convert(ctx, r, decrypt, f)
		if err != nil {
			failed++
			u.Warn("%v", err)
			if ctx.Err() != nil {
				break
			}
			continue
		}
		u.Info("%s -> %s", rel(f), rel(dst))
		if decrypt {
			plain = append(plain, dst)
		}
	}
	// Outside a repository, or without git, nothing can be committed.
	if _, err := r.LookPath("git"); err == nil && ctx.Err() == nil {
		for _, p := range plain {
			if ignored, err := sops.Ignored(ctx, r, p); err == nil && !ignored {
				u.Warn("%s is not git-ignored: add *%s to .gitignore, or it can be committed in plaintext", rel(p), sops.Decrypted)
			}
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d file(s) failed to %s", failed, verb)
	}
	return nil
}
