// Package attest handles optional, explicit AI-assistance attribution trailers.
// It never signs commits, never adds Signed-off-by, and never changes anything unless asked.
package attest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/adalmao/adalmao/internal/config"
	"github.com/adalmao/adalmao/internal/git"
)

const hookMarker = "# adalmao-attest-hook"

// Report describes the current attribution policy.
func Report(cfg *config.Config, cwd string) string {
	var s strings.Builder
	s.WriteString("😂 AdaLmao Attest\n\n")
	state := "disabled"
	if cfg.Attest.Enabled {
		state = "enabled"
	}
	fmt.Fprintf(&s, "Attribution trailer: %q (%s)\n", cfg.Attest.Trailer, state)
	s.WriteString("AdaLmao never adds Signed-off-by (DCO), never signs commits, and never edits commits on its own.\n\n")
	s.WriteString("Use it manually in a commit message:\n")
	fmt.Fprintf(&s, "  git commit -m \"your message\" -m \"%s\"\n", cfg.Attest.Trailer)
	s.WriteString("\nOptional: `adalmao attest install-hook` installs a prepare-commit-msg hook in this repo that appends\n")
	s.WriteString("the trailer (visible and editable in your commit editor). `adalmao attest remove-hook` removes it.\n")
	if root := git.Root(cwd); root != "" {
		if hookPath(root) != "" {
			if b, err := os.ReadFile(hookPath(root)); err == nil && strings.Contains(string(b), hookMarker) {
				s.WriteString("\nThis repo: attribution git hook is INSTALLED.\n")
			} else {
				s.WriteString("\nThis repo: attribution git hook is not installed.\n")
			}
		}
	}
	return s.String()
}

func hookPath(root string) string {
	out, err := git.Run(root, "rev-parse", "--git-path", "hooks/prepare-commit-msg")
	if err != nil || out == "" {
		return ""
	}
	if !filepath.IsAbs(out) {
		out = filepath.Join(root, out)
	}
	return out
}

// InstallHook adds a prepare-commit-msg hook that appends the trailer. Requires attest.enabled.
func InstallHook(cfg *config.Config, cwd string) (string, error) {
	if !cfg.Attest.Enabled {
		return "", fmt.Errorf("attribution is disabled; set attest.enabled=true in %s first (explicit opt-in)", config.Path())
	}
	if strings.ContainsAny(cfg.Attest.Trailer, "\"'`$\\\n") || strings.TrimSpace(cfg.Attest.Trailer) == "" {
		return "", fmt.Errorf("attest.trailer contains unsafe characters or is empty")
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(cfg.Attest.Trailer)), "signed-off-by") {
		return "", fmt.Errorf("refusing to configure a Signed-off-by trailer: DCO sign-off must be your own decision")
	}
	root := git.Root(cwd)
	if root == "" {
		return "", fmt.Errorf("not inside a git repository")
	}
	p := hookPath(root)
	if p == "" {
		return "", fmt.Errorf("could not locate the git hooks directory")
	}
	if b, err := os.ReadFile(p); err == nil && !strings.Contains(string(b), hookMarker) {
		return "", fmt.Errorf("%s already exists and is not managed by AdaLmao; not overwriting", p)
	}
	script := "#!/bin/sh\n" + hookMarker + "\n" +
		"# Appends an AI-assistance trailer. Edit or delete the line in your commit message if you disagree.\n" +
		"grep -qF \"" + cfg.Attest.Trailer + "\" \"$1\" || printf '\\n%s\\n' \"" + cfg.Attest.Trailer + "\" >> \"$1\"\n"
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		return "", err
	}
	return p, nil
}

// RemoveHook removes the hook only if AdaLmao installed it.
func RemoveHook(cwd string) (string, error) {
	root := git.Root(cwd)
	if root == "" {
		return "", fmt.Errorf("not inside a git repository")
	}
	p := hookPath(root)
	b, err := os.ReadFile(p)
	if err != nil {
		return "", fmt.Errorf("no prepare-commit-msg hook found")
	}
	if !strings.Contains(string(b), hookMarker) {
		return "", fmt.Errorf("%s is not managed by AdaLmao; leaving it alone", p)
	}
	return p, os.Remove(p)
}
