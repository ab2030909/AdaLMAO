// Package preflight inspects git state before pushing. It is strictly read-only on the working tree.
package preflight

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/adalmao/adalmao/internal/config"
	"github.com/adalmao/adalmao/internal/git"
)

// Result of a preflight run.
type Result struct {
	Text     string
	Problems int // issues that should be resolved before pushing
	Blocking int // issues serious enough to stop a push (in-progress merge/rebase, confirmed conflicts)
}

func lines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

func counts(dir, a, b string) (left, right int, ok bool) {
	out, err := git.Run(dir, "rev-list", "--left-right", "--count", a+"..."+b)
	if err != nil {
		return 0, 0, false
	}
	f := strings.Fields(out)
	if len(f) != 2 {
		return 0, 0, false
	}
	l, _ := strconv.Atoi(f[0])
	r, _ := strconv.Atoi(f[1])
	return l, r, true
}

func baseBranch(cfg *config.Config, dir string) string {
	if b := cfg.Git.BaseBranch; b != "" {
		return b
	}
	if out, err := git.Run(dir, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil && out != "" {
		return out
	}
	for _, c := range []string{"origin/main", "origin/master", "main", "master"} {
		if _, err := git.Run(dir, "rev-parse", "--verify", "--quiet", c); err == nil {
			return c
		}
	}
	return ""
}

// Run performs the preflight checks in dir.
func Run(cfg *config.Config, dir string) Result {
	var b strings.Builder
	r := Result{}
	if !git.Available() {
		return Result{Text: "git is not installed or not on PATH.\n", Problems: 1}
	}
	root := git.Root(dir)
	if root == "" {
		return Result{Text: "Not inside a git repository.\n", Problems: 1}
	}
	branch := git.Branch(root)
	fmt.Fprintf(&b, "😂 AdaLmao Preflight\n\nRepository: %s\nBranch:     %s\n", root, branch)
	if branch == "HEAD" {
		b.WriteString("⚠️  Detached HEAD — create a branch before pushing.\n")
		r.Problems++
	}
	var suggestions []string

	// in-progress operations
	for _, p := range []string{"MERGE_HEAD", "rebase-merge", "rebase-apply", "CHERRY_PICK_HEAD"} {
		if out, err := git.Run(root, "rev-parse", "--git-path", p); err == nil {
			path := out
			if !strings.HasPrefix(path, "/") && !strings.Contains(path, ":") {
				path = root + "/" + path
			}
			if _, err := os.Stat(path); err == nil {
				fmt.Fprintf(&b, "⚠️  A %s is in progress — finish or abort it first.\n", p)
				r.Problems++
				r.Blocking++
			}
		}
	}

	// working tree
	status, _ := git.Run(root, "status", "--porcelain")
	if status == "" {
		b.WriteString("[✓] Working tree clean\n")
	} else {
		var tracked, untracked int
		for _, l := range lines(status) {
			if strings.HasPrefix(l, "??") {
				untracked++
			} else {
				tracked++
			}
		}
		fmt.Fprintf(&b, "⚠️  Uncommitted changes: %d tracked, %d untracked (not part of any push)\n", tracked, untracked)
		suggestions = append(suggestions, "Commit or stash local changes before rebasing/merging.")
	}

	// upstream
	up, err := git.Run(root, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	if err != nil || up == "" {
		fmt.Fprintf(&b, "⚠️  No upstream tracking branch for %s (first push needs `git push -u`).\n", branch)
	} else {
		behind, ahead, ok := counts(root, up, "HEAD")
		if ok {
			fmt.Fprintf(&b, "Upstream:   %s (ahead %d, behind %d; based on last fetch)\n", up, ahead, behind)
			if behind > 0 {
				fmt.Fprintf(&b, "⚠️  Branch is behind %s by %d commit(s).\n", up, behind)
				r.Problems++
				suggestions = append(suggestions, "Review upstream changes, then rebase or merge deliberately.")
			}
		}
	}

	// base
	base := baseBranch(cfg, root)
	switch {
	case base == "":
		b.WriteString("Base branch: could not be determined (set git.base_branch in the config).\n")
	case base == branch || strings.HasSuffix(base, "/"+branch):
		fmt.Fprintf(&b, "Base branch: %s (you are on it)\n", base)
	default:
		behind, ahead, ok := counts(root, base, "HEAD")
		if ok {
			fmt.Fprintf(&b, "Base:       %s (you are ahead %d, behind %d)\n", base, ahead, behind)
		}
		if ok && behind > 0 {
			fmt.Fprintf(&b, "⚠️  Branch is behind %s.\n", base)
			r.Problems++
			baseConflicts(&b, &r, root, base)
			suggestions = append(suggestions, "Review changes before rebasing; AdaLmao never rebases for you.")
		} else if ok {
			b.WriteString("[✓] Up to date with base\n")
		}
		if out, err := git.Run(root, "cherry", base, "HEAD"); err == nil {
			eq := 0
			for _, l := range lines(out) {
				if strings.HasPrefix(l, "-") {
					eq++
				}
			}
			if eq > 0 {
				fmt.Fprintf(&b, "ℹ️  %d local commit(s) already have an equivalent in %s (a rebase would likely drop them).\n", eq, base)
			}
		}
	}

	if r.Problems == 0 {
		b.WriteString("\nNo blocking problems found. Looks pushable. (Remote state is as of your last fetch.)\n")
	}
	if len(suggestions) > 0 {
		b.WriteString("\nSuggested next steps:\n")
		for _, s := range suggestions {
			fmt.Fprintf(&b, "- %s\n", s)
		}
	}
	r.Text = b.String()
	return r
}

func baseConflicts(b *strings.Builder, r *Result, root, base string) {
	// Preferred: real merge simulation (git >= 2.38) — no working tree changes, "confirmed".
	out, err := git.RunT(20*time.Second, root, "merge-tree", "--write-tree", "--name-only", "--no-messages", "HEAD", base)
	if err == nil || out != "" {
		ls := lines(out)
		if len(ls) > 0 && len(ls[0]) >= 40 { // first line is the resulting tree id
			conf := ls[1:]
			if err == nil {
				b.WriteString("[✓] Simulated merge with base is clean (no conflicts)\n")
				return
			}
			if len(conf) > 0 {
				b.WriteString("\n🚨 Confirmed merge conflicts with " + base + " (via git merge-tree):\n")
				for _, f := range conf {
					fmt.Fprintf(b, "- %s\n", f)
				}
				b.WriteString("\n")
				r.Problems++
				r.Blocking++
				return
			}
		}
	}
	// Fallback: files changed on both sides since the merge-base = only POTENTIAL conflicts.
	mb, err := git.Run(root, "merge-base", "HEAD", base)
	if err != nil || mb == "" {
		b.WriteString("Could not compute merge-base; conflicts unknown.\n")
		return
	}
	ours, _ := git.Run(root, "diff", "--name-only", mb, "HEAD")
	theirs, _ := git.Run(root, "diff", "--name-only", mb, base)
	set := map[string]bool{}
	for _, f := range lines(theirs) {
		set[f] = true
	}
	var both []string
	for _, f := range lines(ours) {
		if set[f] {
			both = append(both, f)
		}
	}
	if len(both) == 0 {
		b.WriteString("No files changed on both sides — merge is unlikely to conflict (git merge-tree unavailable, heuristic only).\n")
		return
	}
	b.WriteString("\nPotentially conflicting files (changed on both sides; NOT confirmed):\n")
	for _, f := range both {
		fmt.Fprintf(b, "- %s\n", f)
	}
	b.WriteString("\n")
}
