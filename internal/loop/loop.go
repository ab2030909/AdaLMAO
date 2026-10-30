// Package loop runs a bounded, user-initiated experiment: ask AdaL (headless `adal -q`) to do a task in an
// isolated git worktree, run a verification command, record the result, and stop on success or limits.
// It never loops without a cap, never merges, and never touches your working tree.
package loop

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/adalmao/adalmao/internal/config"
	"github.com/adalmao/adalmao/internal/decision"
	"github.com/adalmao/adalmao/internal/enforce"
	"github.com/adalmao/adalmao/internal/git"
	"github.com/adalmao/adalmao/internal/shell"
)

const (
	HardMaxIterations = 10
	HardMaxMinutes    = 240
)

type Options struct {
	Task           string
	Verify         string
	MaxIterations  int
	MaxMinutes     int
	Model          string
	PermissionMode string // AdaL --permission-mode; default acceptEdits (edits run, shell commands do not)
	Dir            string
	Out            io.Writer
}

// Entry is one ledger line.
type Entry struct {
	Iteration  int     `json:"iteration"`
	Started    string  `json:"started"`
	AgentSecs  float64 `json:"agent_seconds"`
	AgentExit  int     `json:"agent_exit"`
	VerifyExit int     `json:"verify_exit"`
	Score      float64 `json:"score"` // 1 = verification passed, 0 = failed
	DiffStat   string  `json:"diff_stat"`
	Note       string  `json:"note,omitempty"`
}

func adalBin() string {
	if v := os.Getenv("ADALMAO_ADAL_BIN"); v != "" {
		return v
	}
	return "adal"
}

func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return "…" + s[len(s)-n:]
	}
	return s
}

// Validate checks options and applies defaults.
func (o *Options) Validate(cfg *config.Config) error {
	if strings.TrimSpace(o.Task) == "" {
		return fmt.Errorf("--task is required")
	}
	if strings.TrimSpace(o.Verify) == "" {
		return fmt.Errorf("--verify <command> is required: the loop needs a success condition")
	}
	if o.MaxIterations == 0 {
		o.MaxIterations = 3
	}
	if o.MaxIterations < 1 || o.MaxIterations > HardMaxIterations {
		return fmt.Errorf("--max-iterations must be between 1 and %d", HardMaxIterations)
	}
	if o.MaxMinutes == 0 {
		o.MaxMinutes = 30
	}
	if o.MaxMinutes < 1 || o.MaxMinutes > HardMaxMinutes {
		return fmt.Errorf("--max-minutes must be between 1 and %d", HardMaxMinutes)
	}
	if o.PermissionMode == "" {
		o.PermissionMode = "acceptEdits"
	}
	if d := decisionFor(cfg, o.Dir, o.Verify); d != "" {
		return fmt.Errorf("the verification command is blocked by AdaLmao enforce rules: %s", d)
	}
	return nil
}

func decisionFor(cfg *config.Config, dir, cmd string) string {
	for _, d := range enforce.EvalBash(cfg, dir, cmd) {
		if d.Level >= decision.Blocked {
			return d.Rule + ": " + d.Reason
		}
	}
	return ""
}

func run(ctx context.Context, c *exec.Cmd) (string, int) {
	out, err := c.CombinedOutput()
	if err == nil {
		return string(out), 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return string(out), ee.ExitCode()
	}
	if ctx.Err() != nil {
		return string(out) + "\n(timed out)", -2
	}
	return string(out) + "\n" + err.Error(), -1
}

// Run executes the loop. It returns the ledger path and whether verification passed.
func Run(cfg *config.Config, o Options) (ledger string, passed bool, err error) {
	if err := o.Validate(cfg); err != nil {
		return "", false, err
	}
	out := o.Out
	if out == nil {
		out = io.Discard
	}
	if _, err := exec.LookPath(adalBin()); err != nil {
		return "", false, fmt.Errorf("AdaL CLI not found on PATH")
	}
	root := git.Root(o.Dir)
	if root == "" {
		return "", false, fmt.Errorf("loop must be started inside a git repository (it works in an isolated worktree)")
	}
	if _, err := git.Run(root, "rev-parse", "--verify", "HEAD"); err != nil {
		return "", false, fmt.Errorf("the repository has no commits yet; commit once so the loop has a base")
	}
	stamp := time.Now().Format("20060102-150405")
	branch := "adalmao/loop-" + stamp
	wt := filepath.Join(os.TempDir(), "adalmao-loop-"+stamp)
	if _, err := git.RunT(30*time.Second, root, "worktree", "add", "-q", "-b", branch, wt, "HEAD"); err != nil {
		return "", false, fmt.Errorf("could not create worktree: %w", err)
	}
	dir := filepath.Join(config.Dir(), "loops")
	_ = os.MkdirAll(dir, 0o755)
	ledger = filepath.Join(dir, stamp+".jsonl")
	lf, err := os.Create(ledger)
	if err != nil {
		return "", false, err
	}
	defer lf.Close()

	fmt.Fprintf(out, "Loop worktree: %s (branch %s, based on HEAD; your working tree is untouched)\n", wt, branch)
	deadline := time.Now().Add(time.Duration(o.MaxMinutes) * time.Minute)
	feedback := ""
	for i := 1; i <= o.MaxIterations; i++ {
		if time.Now().After(deadline) {
			fmt.Fprintln(out, "Stopping: time limit reached.")
			break
		}
		e := Entry{Iteration: i, Started: time.Now().Format(time.RFC3339)}
		prompt := o.Task
		if feedback != "" {
			prompt += "\n\nThe previous attempt did not pass verification (`" + o.Verify + "`). Its output:\n" + feedback + "\nFix the problem."
		}
		args := []string{"-q", prompt, "-o", "text", "--permission-mode", o.PermissionMode}
		if o.Model != "" {
			args = append(args, "-m", o.Model)
		}
		ctx, cancel := context.WithDeadline(context.Background(), deadline)
		c := exec.CommandContext(ctx, adalBin(), args...)
		c.Dir = wt
		t0 := time.Now()
		agentOut, code := run(ctx, c)
		cancel()
		e.AgentSecs, e.AgentExit = time.Since(t0).Seconds(), code
		if code != 0 {
			e.Note = "agent exit " + fmt.Sprint(code) + ": " + tail(agentOut, 200)
		}

		vctx, vcancel := context.WithTimeout(context.Background(), 10*time.Minute)
		vc := shell.Command(o.Verify)
		vc.Dir = wt
		vout, vcode := run(vctx, vc)
		vcancel()
		e.VerifyExit = vcode
		if vcode == 0 {
			e.Score = 1
		}
		_, _ = git.Run(wt, "add", "-A")
		e.DiffStat, _ = git.Run(wt, "diff", "--cached", "--stat")
		if e.DiffStat != "" {
			_, _ = git.RunT(30*time.Second, wt, "commit", "-q", "--no-verify", "-m", fmt.Sprintf("adalmao loop iteration %d", i))
		}
		b, _ := json.Marshal(e)
		fmt.Fprintln(lf, string(b))
		fmt.Fprintf(out, "Iteration %d: agent exit %d (%.0fs), verify exit %d, score %.0f\n", i, code, e.AgentSecs, vcode, e.Score)
		if e.Score == 1 {
			passed = true
			break
		}
		feedback = tail(vout, 1500)
	}
	fmt.Fprintf(out, "\nLedger: %s\n", ledger)
	fmt.Fprintf(out, "Result is on branch %s in worktree %s. Nothing was merged.\n", branch, wt)
	fmt.Fprintf(out, "Review:  git -C \"%s\" log --stat\nKeep:    git merge %s\nDiscard: git worktree remove --force \"%s\" && git branch -D %s\n", wt, branch, wt, branch)
	return ledger, passed, nil
}
