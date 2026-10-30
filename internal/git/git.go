// Package git is a thin wrapper around the git CLI.
package git

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"time"
)

// Run executes git in dir with a timeout and returns trimmed stdout.
func Run(dir string, args ...string) (string, error) {
	return RunT(5*time.Second, dir, args...)
}

func RunT(timeout time.Duration, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	if err != nil && errb.Len() > 0 {
		return strings.TrimRight(out.String(), "\r\n \t"), &Error{Msg: strings.TrimSpace(errb.String()), Err: err}
	}
	return strings.TrimRight(out.String(), "\r\n \t"), err
}

type Error struct {
	Msg string
	Err error
}

func (e *Error) Error() string { return e.Msg }
func (e *Error) Unwrap() error { return e.Err }

// Available reports whether git is on PATH.
func Available() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

// Root returns the repository root or "" when dir is not in a work tree.
func Root(dir string) string {
	out, err := Run(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return ""
	}
	return out
}

// Branch returns the current branch name ("HEAD" when detached).
func Branch(dir string) string {
	out, err := Run(dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return ""
	}
	return out
}

// StatusEntry is one line of `git status --porcelain`.
type StatusEntry struct {
	X, Y byte
	Path string
}

func (e StatusEntry) Deleted() bool { return e.X == 'D' || e.Y == 'D' }

// Status returns porcelain status entries.
func Status(dir string) ([]StatusEntry, error) {
	out, err := Run(dir, "status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return nil, err
	}
	var es []StatusEntry
	for _, l := range strings.Split(out, "\n") {
		if len(l) < 4 {
			continue
		}
		p := strings.TrimSpace(l[3:])
		if i := strings.Index(p, " -> "); i >= 0 {
			p = p[i+4:]
		}
		es = append(es, StatusEntry{X: l[0], Y: l[1], Path: strings.Trim(p, "\"")})
	}
	return es, nil
}

// DirtySummary returns "clean" or "N changed, M untracked".
func DirtySummary(dir string) string {
	out, err := Run(dir, "status", "--porcelain")
	if err != nil {
		return "unknown"
	}
	if out == "" {
		return "clean"
	}
	changed, untracked := 0, 0
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "??") {
			untracked++
		} else if strings.TrimSpace(l) != "" {
			changed++
		}
	}
	return itoa(changed) + " changed, " + itoa(untracked) + " untracked"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
