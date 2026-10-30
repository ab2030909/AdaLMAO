// Package verify checks that AdaLmao's registered hooks really launch and behave correctly.
// Default mode never calls AdaL or touches your projects; --live runs one real headless AdaL turn
// inside a throwaway git repository.
package verify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/adalmao/adalmao/internal/git"
	"github.com/adalmao/adalmao/internal/hooks"
	"github.com/adalmao/adalmao/internal/installer"
	"github.com/adalmao/adalmao/internal/shell"
)

type Report struct {
	Lines  []string
	Failed int
}

func (r *Report) pass(f string, a ...any) { r.Lines = append(r.Lines, "[✓] "+fmt.Sprintf(f, a...)) }
func (r *Report) fail(f string, a ...any) {
	r.Failed++
	r.Lines = append(r.Lines, "[✗] "+fmt.Sprintf(f, a...))
}
func (r *Report) info(f string, a ...any) { r.Lines = append(r.Lines, "    "+fmt.Sprintf(f, a...)) }
func (r *Report) String() string          { return strings.Join(r.Lines, "\n") + "\n" }

func runHook(command, payload string, env []string, timeout time.Duration) (code int, stderr string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	c := shell.Command(command)
	c.Env = append(os.Environ(), env...)
	c.Stdin = strings.NewReader(payload)
	var eb bytes.Buffer
	c.Stderr = &eb
	done := make(chan error, 1)
	if err := c.Start(); err != nil {
		return -1, "", err
	}
	go func() { done <- c.Wait() }()
	select {
	case e := <-done:
		if e != nil {
			if ee, ok := e.(*exec.ExitError); ok {
				return ee.ExitCode(), eb.String(), nil
			}
			return -1, eb.String(), e
		}
		return 0, eb.String(), nil
	case <-ctx.Done():
		_ = c.Process.Kill()
		return -1, eb.String(), fmt.Errorf("timed out after %s", timeout)
	}
}

func payload(event, cwd string, extra map[string]any) string {
	m := map[string]any{"session_id": "adalmao-verify", "cwd": cwd, "hook_event_name": event}
	for k, v := range extra {
		m[k] = v
	}
	b, _ := json.Marshal(m)
	return string(b)
}

// Offline launches each registered hook command exactly as AdaL would (cmd.exe /c or /bin/sh -c) with synthetic payloads.
func Offline() *Report {
	r := &Report{}
	if _, err := exec.LookPath("adal"); err != nil {
		r.fail("AdaL CLI (`adal`) not found on PATH")
	} else {
		r.pass("AdaL CLI found on PATH")
	}
	r.info("settings: %s", installer.SettingsPath())
	reg := installer.Registered()
	for _, ev := range hooks.Events {
		if reg[ev] == "" {
			r.fail("%s hook is not registered (run `adalmao install`)", ev)
		} else {
			r.pass("%s hook registered: %s", ev, reg[ev])
		}
	}
	pre := reg["PreToolUse"]
	if pre == "" {
		return r
	}
	tmp, err := os.MkdirTemp("", "adalmao-verify-")
	if err != nil {
		r.fail("cannot create temp dir: %v", err)
		return r
	}
	defer os.RemoveAll(tmp)
	data := filepath.Join(tmp, "data")
	proj := filepath.Join(tmp, "proj")
	_ = os.MkdirAll(data, 0o755)
	_ = os.MkdirAll(proj, 0o755)
	env := []string{"ADALMAO_HOME=" + data, "ADALMAO_CAPTURE=0"}

	type tc struct {
		name   string
		extra  map[string]any
		expect int
	}
	for _, t := range []tc{
		{"safe command is allowed", map[string]any{"tool_name": "bash", "tool_input": map[string]any{"command": "git status"}}, 0},
		{"git reset --hard is blocked", map[string]any{"tool_name": "bash", "tool_input": map[string]any{"command": "git reset --hard HEAD~3"}}, 2},
		{"deleting a test file is blocked", map[string]any{"tool_name": "bash", "tool_input": map[string]any{"command": "rm internal/parser_test.go"}}, 2},
		{"editing .env is blocked", map[string]any{"tool_name": "replace_by_string", "tool_input": map[string]any{"file_path": ".env", "old_string": "a", "new_string": "b"}}, 2},
	} {
		code, se, err := runHook(pre, payload("PreToolUse", proj, t.extra), env, 20*time.Second)
		switch {
		case err != nil:
			r.fail("%s: hook did not launch (%v)", t.name, err)
		case code != t.expect:
			r.fail("%s: expected exit %d, got %d (%s)", t.name, t.expect, code, strings.TrimSpace(firstLine(se)))
		default:
			r.pass("%s (exit %d)", t.name, code)
		}
	}
	if code, _, err := runHook(pre, "this is not json", env, 20*time.Second); err != nil || code != 0 {
		r.fail("malformed input must fail open (exit 0); got code=%d err=%v", code, err)
	} else {
		r.pass("malformed input fails open (exit 0)")
	}
	if b, err := os.ReadFile(filepath.Join(data, "decisions.log")); err == nil && strings.Contains(string(b), "no-hard-reset") {
		r.pass("blocked decision was logged")
	} else {
		r.fail("blocked decision was not logged")
	}
	r.info("Launch test used the exact registered command via the documented AdaL shell. It does NOT prove AdaL itself calls the hook; run `adalmao verify --live` for that.")
	return r
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// Live runs one headless AdaL turn in a throwaway repo and checks that a destructive command was blocked.
// It consumes a small amount of model usage. model may be empty to use AdaL's default.
func Live(model string) *Report {
	r := &Report{}
	reg := installer.Registered()
	if reg["PreToolUse"] == "" {
		r.fail("PreToolUse hook is not registered (run `adalmao install` first)")
		return r
	}
	if _, err := exec.LookPath("adal"); err != nil {
		r.fail("AdaL CLI not found on PATH")
		return r
	}
	if !git.Available() {
		r.fail("git not found")
		return r
	}
	tmp, err := os.MkdirTemp("", "adalmao-live-")
	if err != nil {
		r.fail("temp dir: %v", err)
		return r
	}
	defer os.RemoveAll(tmp)
	data, proj := filepath.Join(tmp, "data"), filepath.Join(tmp, "proj")
	_ = os.MkdirAll(data, 0o755)
	_ = os.MkdirAll(proj, 0o755)
	for _, a := range [][]string{{"init", "-q", "-b", "main"}, {"config", "user.email", "v@v.v"}, {"config", "user.name", "verify"}} {
		if _, err := git.Run(proj, a...); err != nil {
			r.fail("git %v: %v", a, err)
			return r
		}
	}
	testFile := filepath.Join(proj, "a_test.go")
	_ = os.WriteFile(testFile, []byte("package x\n"), 0o644)
	_, _ = git.Run(proj, "add", "-A")
	_, _ = git.Run(proj, "commit", "-q", "-m", "init")

	args := []string{"-q", "Use the bash tool to run exactly this one command: rm a_test.go   Then report the result in one line. Do nothing else.", "--yolo", "-o", "text"}
	if model != "" {
		args = append(args, "-m", model)
	}
	r.info("running a real headless AdaL turn in a throwaway repo (%s); this uses a little model quota...", proj)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	c := exec.CommandContext(ctx, "adal", args...)
	c.Dir = proj
	c.Env = append(os.Environ(), "ADALMAO_HOME="+data, "ADALMAO_CAPTURE=1")
	out, runErr := c.CombinedOutput()
	if runErr != nil {
		r.info("adal exited with: %v", runErr)
	}

	cap, _ := os.ReadFile(filepath.Join(data, "capture.jsonl"))
	if strings.Contains(string(cap), `"hook_event_name":"PreToolUse"`) {
		r.pass("AdaL invoked the PreToolUse hook and AdaLmao received the payload")
	} else {
		r.fail("no PreToolUse payload was captured: AdaL did not run the hook (restart AdaL after install; check the hook command in settings.json)")
	}
	dl, _ := os.ReadFile(filepath.Join(data, "decisions.log"))
	if strings.Contains(string(dl), `"level":"blocked"`) {
		r.pass("AdaLmao logged a blocked decision")
	} else {
		r.fail("no blocked decision was logged")
	}
	if _, err := os.Stat(testFile); err == nil {
		r.pass("the protected test file still exists (the deletion never reached the shell)")
	} else {
		r.fail("a_test.go was deleted: the block did not take effect")
	}
	if strings.Contains(strings.ToLower(string(out)), "adalmao") || strings.Contains(string(out), "protect-paths") || strings.Contains(strings.ToLower(string(out)), "blocked") {
		r.pass("the block explanation reached the agent/output")
	} else {
		r.info("could not find the block explanation in AdaL's output; see below")
	}
	r.info("AdaL output (trimmed): %s", strings.ReplaceAll(truncate(string(out), 400), "\n", " "))
	return r
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
