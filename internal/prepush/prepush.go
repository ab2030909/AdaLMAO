// Package prepush manages an optional, AdaLmao-owned git pre-push hook that runs preflight checks.
// It never overwrites a hook it does not own, never rebases, merges or pushes.
package prepush

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/adalmao/adalmao/internal/config"
	"github.com/adalmao/adalmao/internal/git"
	"github.com/adalmao/adalmao/internal/preflight"
)

const marker = "# adalmao-prepush (managed by AdaLmao; remove with `adalmao prepush remove`)"

func hookPath(root string) (string, error) {
	out, err := git.Run(root, "rev-parse", "--git-path", "hooks/pre-push")
	if err != nil || out == "" {
		return "", fmt.Errorf("could not locate the git hooks directory")
	}
	if !filepath.IsAbs(out) {
		out = filepath.Join(root, out)
	}
	return out, nil
}

func owned(p string) (exists, ours bool) {
	b, err := os.ReadFile(p)
	if err != nil {
		return false, false
	}
	return true, strings.Contains(string(b), marker)
}

// Script returns the hook script text for the given executable path.
func Script(exe string) string {
	q := "'" + strings.ReplaceAll(filepath.ToSlash(exe), "'", `'\''`) + "'"
	return "#!/bin/sh\n" + marker + "\n" +
		"# Skip once with: ADALMAO_SKIP_PREPUSH=1 git push   (or git push --no-verify)\n" +
		"[ \"$ADALMAO_SKIP_PREPUSH\" = \"1\" ] && exit 0\n" +
		"# Fail open if the binary has moved, so a missing file never blocks your push.\n" +
		"if [ -f " + q + " ]; then " + q + " prepush run \"$@\" || exit $?; fi\n" +
		"exit 0\n"
}

// Install writes the hook into the repository containing dir.
func Install(dir, exe string) (string, error) {
	root := git.Root(dir)
	if root == "" {
		return "", fmt.Errorf("not inside a git repository")
	}
	p, err := hookPath(root)
	if err != nil {
		return "", err
	}
	if exists, ours := owned(p); exists && !ours {
		return "", fmt.Errorf("%s already exists and is not managed by AdaLmao; not overwriting it. Add this line to it yourself if you want the check: %s prepush run \"$@\"", p, filepath.ToSlash(exe))
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(p, []byte(Script(exe)), 0o755); err != nil {
		return "", err
	}
	return p, nil
}

// Remove deletes the hook only if AdaLmao installed it.
func Remove(dir string) (string, error) {
	root := git.Root(dir)
	if root == "" {
		return "", fmt.Errorf("not inside a git repository")
	}
	p, err := hookPath(root)
	if err != nil {
		return "", err
	}
	exists, ours := owned(p)
	switch {
	case !exists:
		return "", fmt.Errorf("no pre-push hook installed")
	case !ours:
		return "", fmt.Errorf("%s is not managed by AdaLmao; leaving it alone", p)
	}
	return p, os.Remove(p)
}

// Status describes the hook state for dir's repository.
func Status(dir string) string {
	root := git.Root(dir)
	if root == "" {
		return "not inside a git repository"
	}
	p, err := hookPath(root)
	if err != nil {
		return err.Error()
	}
	exists, ours := owned(p)
	switch {
	case ours:
		return "installed: " + p
	case exists:
		return "a different pre-push hook exists (not managed by AdaLmao): " + p
	}
	return "not installed"
}

// Run is invoked by the git hook. It returns the process exit code: non-zero aborts the push.
// Only in-progress merge/rebase and confirmed conflicts with the base abort; everything else warns.
func Run(dir string) (string, int) {
	cfg := config.MustLoad()
	if !cfg.Git.PreflightEnabled {
		return "", 0
	}
	r := preflight.Run(cfg, dir)
	if r.Blocking > 0 {
		return r.Text + "\nAdaLmao pre-push: push aborted. Resolve the above, or bypass once with `git push --no-verify`.\n", 1
	}
	if r.Problems > 0 {
		return r.Text + "\nAdaLmao pre-push: warnings only; continuing with the push.\n", 0
	}
	return "", 0
}
