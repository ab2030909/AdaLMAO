package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/adalmao/adalmao/internal/loop"
	"github.com/adalmao/adalmao/internal/prepush"
	"github.com/adalmao/adalmao/internal/update"
	"github.com/adalmao/adalmao/internal/verify"
)

func newFlags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

func exePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	return exe, nil
}

// ---- verify ----

func cmdVerify(args []string) int {
	fs := newFlags("verify")
	live := fs.Bool("live", false, "run one real headless AdaL turn in a throwaway repo")
	model := fs.String("model", "", "AdaL model key for --live (default: AdaL's default)")
	if err := fs.Parse(args); err != nil {
		return errf("usage: adalmao verify [--live] [--model <key>]")
	}
	var r *verify.Report
	if *live {
		r = verify.Live(*model)
	} else {
		r = verify.Offline()
	}
	fmt.Fprint(stdout, r.String())
	if r.Failed > 0 {
		fmt.Fprintf(stdout, "\n%d check(s) FAILED.\n", r.Failed)
		return 1
	}
	if *live {
		fmt.Fprintln(stdout, "\nAll live checks passed: AdaL invoked AdaLmao and the destructive command was blocked.")
	} else {
		fmt.Fprintln(stdout, "\nAll launch checks passed. Run `adalmao verify --live` to confirm AdaL itself calls the hooks.")
	}
	return 0
}

// ---- prepush ----

func cmdPrepush(args []string) int {
	if len(args) == 0 {
		args = []string{"status"}
	}
	switch args[0] {
	case "install":
		exe, err := exePath()
		if err != nil {
			return errf("%v", err)
		}
		p, err := prepush.Install(cwd(), exe)
		if err != nil {
			return errf("%v", err)
		}
		fmt.Fprintf(stdout, "[✓] Installed pre-push hook: %s\nIt only aborts a push on an in-progress merge/rebase or confirmed conflicts with the base branch; everything else warns.\nBypass once: git push --no-verify  (or ADALMAO_SKIP_PREPUSH=1)\n", p)
	case "remove":
		p, err := prepush.Remove(cwd())
		if err != nil {
			return errf("%v", err)
		}
		fmt.Fprintf(stdout, "[✓] Removed %s\n", p)
	case "status":
		fmt.Fprintln(stdout, "pre-push hook:", prepush.Status(cwd()))
	case "run": // called by git
		text, code := prepush.Run(cwd())
		if text != "" {
			fmt.Fprint(stderr, text)
		}
		return code
	default:
		return errf("usage: adalmao prepush install|remove|status")
	}
	return 0
}

// ---- update ----

func cmdUpdate(args []string) int {
	fs := newFlags("update")
	apply := fs.Bool("apply", false, "download, verify and install the latest release")
	if err := fs.Parse(args); err != nil {
		return errf("usage: adalmao update [--apply]")
	}
	fmt.Fprintf(stdout, "Installed: v%s\n", Version)
	rel, err := update.Latest()
	if err != nil {
		fmt.Fprintf(stdout, "Latest:    unavailable (%v)\n", err)
		if err == update.ErrNoRelease {
			fmt.Fprintf(stdout, "Nothing to update to. Build from source: https://github.com/%s\n", update.Repo)
			return 0
		}
		return 1
	}
	fmt.Fprintf(stdout, "Latest:    %s\n", rel.Tag)
	if !update.Newer(Version, rel.Tag) {
		fmt.Fprintln(stdout, "You are up to date.")
		return 0
	}
	if !*apply {
		fmt.Fprintln(stdout, "A newer release exists. Run `adalmao update --apply` to download it. The download is SHA-256 verified against the release's checksums.txt; unverifiable updates are refused.")
		return 0
	}
	exe, err := exePath()
	if err != nil {
		return errf("%v", err)
	}
	old, err := update.Apply(rel, exe)
	if err != nil {
		return errf("update failed, current binary untouched: %v", err)
	}
	fmt.Fprintf(stdout, "[✓] Updated to %s (checksum verified). Previous binary kept at %s\nHooks use the same path, so no reinstall is needed; restart AdaL to pick up the new binary.\n", rel.Tag, old)
	return 0
}

// ---- loop ----

func cmdLoop(args []string) int {
	fs := newFlags("loop")
	task := fs.String("task", "", "what AdaL should do")
	ver := fs.String("verify", "", "shell command; exit 0 means success")
	maxIt := fs.Int("max-iterations", 3, "iteration limit (1-10)")
	maxMin := fs.Int("max-minutes", 30, "wall-clock limit (1-240)")
	model := fs.String("model", "", "AdaL model key")
	pm := fs.String("permission-mode", "acceptEdits", "AdaL --permission-mode (acceptEdits: edits run, shell commands do not; yolo: everything runs)")
	yes := fs.Bool("yes", false, "actually start (otherwise only the plan is printed)")
	if err := fs.Parse(args); err != nil {
		return errf("usage: adalmao loop --task \"...\" --verify \"cmd\" [--max-iterations N] [--max-minutes N] [--model key] [--permission-mode m] --yes")
	}
	cfg, _ := loadCfg()
	o := loop.Options{Task: *task, Verify: *ver, MaxIterations: *maxIt, MaxMinutes: *maxMin, Model: *model, PermissionMode: *pm, Dir: cwd(), Out: stdout}
	if err := o.Validate(cfg); err != nil {
		return errf("%v", err)
	}
	fmt.Fprintf(stdout, "Loop plan\n  task:        %s\n  verify:      %s (exit 0 = success)\n  limits:      %d iteration(s), %d minute(s)\n  permission:  %s\n  isolation:   a new git worktree + branch; your working tree is not touched\n  budget:      token/cost limits are not available from AdaL; only the iteration and time caps apply\n",
		o.Task, o.Verify, o.MaxIterations, o.MaxMinutes, o.PermissionMode)
	if !*yes {
		fmt.Fprintln(stdout, "\nNothing was started. Re-run with --yes to begin (this uses model quota).")
		return 0
	}
	fmt.Fprintln(stdout)
	_, passed, err := loop.Run(cfg, o)
	if err != nil {
		return errf("%v", err)
	}
	if passed {
		fmt.Fprintln(stdout, "\nVerification PASSED.")
		return 0
	}
	fmt.Fprintln(stdout, "\nVerification did not pass within the limits.")
	return 1
}
