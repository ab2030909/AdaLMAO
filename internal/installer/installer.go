// Package installer registers/unregisters AdaLmao-owned hooks in AdaL's ~/.adal/settings.json.
// It never rewrites unrelated settings, backs up before changing, and is idempotent.
package installer

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/adalmao/adalmao/internal/config"
	"github.com/adalmao/adalmao/internal/hooks"
)

// ownedRe identifies hook commands that belong to AdaLmao: `<...adalmao[.exe]> hook <event>`.
var ownedRe = regexp.MustCompile(`(?i)adalmao(\.exe)?["']?\s+hook\s+[a-z-]+"?\s*$`)

var eventArg = map[string]string{
	"PreToolUse": "pre-tool-use", "PostToolUse": "post-tool-use", "UserPromptSubmit": "user-prompt-submit", "Stop": "stop",
}
var timeouts = map[string]int{"PreToolUse": 10, "PostToolUse": 15, "UserPromptSubmit": 10, "Stop": 10}

func SettingsPath() string { return filepath.Join(config.AdalDir(), "settings.json") }

// Report describes what an install/uninstall did.
type Report struct {
	SettingsPath string
	Backup       string
	Changed      bool
	Added        []string
	Removed      []string
	Notes        []string
}

func owned(cmd string) bool { return ownedRe.MatchString(strings.TrimSpace(cmd)) }

// HookCommand builds the command string for an event using the given executable path.
//
// AdaL runs hooks through `cmd.exe /c` on Windows and `/bin/sh -c` (or $SHELL) elsewhere. Verified against
// AdaL 1.11.5 on Windows: any hook command containing double quotes (a quoted path with spaces, with or
// without a `cmd /s /c` wrapper) silently failed to launch. So on Windows the path must contain no spaces;
// Install() converts it to its 8.3 short form and refuses if that is impossible.
func HookCommand(exe, event string) string {
	return hookCommandFor(runtime.GOOS, exe, event)
}

func hookCommandFor(goos, exe, event string) string {
	arg := eventArg[event]
	if goos != "windows" && strings.ContainsAny(exe, " \t&()^%!;'\"$`\\") {
		return "'" + strings.ReplaceAll(exe, "'", `'\''`) + "' hook " + arg
	}
	return exe + " hook " + arg
}

// resolveExe makes the executable path safe to embed in a hook command.
func resolveExe(exe string) (string, error) {
	if runtime.GOOS != "windows" {
		return exe, nil
	}
	if strings.ContainsAny(exe, " \t") {
		exe = shortPath(exe)
	}
	if strings.ContainsAny(exe, " \t&()^%!\"") {
		return "", fmt.Errorf("the executable path %q contains spaces or special characters, and AdaL's Windows hook launcher fails on quoted commands. Move adalmao.exe to a simple path such as C:\\adalmao\\ (or C:\\Users\\<you>\\bin) and run `adalmao install` from there", exe)
	}
	return exe, nil
}
func load() (map[string]json.RawMessage, []byte, error) {
	b, err := os.ReadFile(SettingsPath())
	if errors.Is(err, os.ErrNotExist) {
		return map[string]json.RawMessage{}, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return map[string]json.RawMessage{}, b, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, nil, fmt.Errorf("%s is not valid JSON (%v); refusing to touch it", SettingsPath(), err)
	}
	return m, b, nil
}

func encode(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// parseHooks decodes the "hooks" object into event -> []group.
func parseHooks(raw json.RawMessage) (map[string][]any, error) {
	h := map[string][]any{}
	if len(raw) == 0 || string(raw) == "null" {
		return h, nil
	}
	if err := json.Unmarshal(raw, &h); err != nil {
		return nil, fmt.Errorf("existing \"hooks\" setting has an unexpected shape: %v", err)
	}
	return h, nil
}

// stripOwned removes AdaLmao hooks from groups; returns remaining groups and how many hooks were removed.
func stripOwned(groups []any) ([]any, int) {
	removed := 0
	var out []any
	for _, g := range groups {
		gm, ok := g.(map[string]any)
		if !ok {
			out = append(out, g)
			continue
		}
		hs, ok := gm["hooks"].([]any)
		if !ok {
			out = append(out, g)
			continue
		}
		var keep []any
		for _, h := range hs {
			if hm, ok := h.(map[string]any); ok {
				if cmd, _ := hm["command"].(string); owned(cmd) {
					removed++
					continue
				}
			}
			keep = append(keep, h)
		}
		if len(keep) == 0 {
			continue // group contained only our hooks
		}
		gm["hooks"] = keep
		out = append(out, gm)
	}
	return out, removed
}

func write(m map[string]json.RawMessage, orig []byte, rep *Report) error {
	out, err := encode(m)
	if err != nil {
		return err
	}
	if orig != nil {
		rep.Backup = fmt.Sprintf("%s.adalmao-%s.bak", SettingsPath(), time.Now().Format("20060102-150405"))
		if err := os.WriteFile(rep.Backup, orig, 0o600); err != nil {
			return fmt.Errorf("could not write backup: %w", err)
		}
	} else if err := os.MkdirAll(filepath.Dir(SettingsPath()), 0o755); err != nil {
		return err
	}
	return config.WriteFileAtomic(SettingsPath(), out)
}

// Install registers the hooks. exe is the absolute path to the adalmao binary.
func Install(exe string) (*Report, error) {
	rep := &Report{SettingsPath: SettingsPath()}
	exe, err := resolveExe(exe)
	if err != nil {
		return nil, err
	}
	if _, err := exec.LookPath("adal"); err != nil {
		if _, serr := os.Stat(config.AdalDir()); serr != nil {
			return nil, fmt.Errorf("AdaL CLI not detected (no `adal` on PATH and %s missing). Install AdaL first", config.AdalDir())
		}
		rep.Notes = append(rep.Notes, "`adal` not found on PATH, but "+config.AdalDir()+" exists; continuing")
	}
	m, orig, err := load()
	if err != nil {
		return nil, err
	}
	h, err := parseHooks(m["hooks"])
	if err != nil {
		return nil, err
	}
	for _, ev := range hooks.Events {
		groups, _ := stripOwned(h[ev])
		entry := map[string]any{"type": "command", "command": HookCommand(exe, ev), "timeout": timeouts[ev]}
		g := map[string]any{"hooks": []any{entry}}
		if ev == "PreToolUse" || ev == "PostToolUse" {
			g["matcher"] = "*"
		}
		h[ev] = append(groups, g)
		rep.Added = append(rep.Added, ev)
	}
	newHooks, err := encode(h)
	if err != nil {
		return nil, err
	}
	var oldCompact, newCompact bytes.Buffer
	if len(m["hooks"]) > 0 {
		var oh any
		_ = json.Unmarshal(m["hooks"], &oh)
		b, _ := json.Marshal(oh)
		oldCompact.Write(b)
	}
	var nh any
	_ = json.Unmarshal(newHooks, &nh)
	nb, _ := json.Marshal(nh)
	newCompact.Write(nb)
	if bytes.Equal(oldCompact.Bytes(), newCompact.Bytes()) {
		rep.Added = nil
		rep.Notes = append(rep.Notes, "hooks already registered and up to date (no changes made)")
		return rep, nil
	}
	m["hooks"] = json.RawMessage(bytes.TrimSpace(newHooks))
	if err := write(m, orig, rep); err != nil {
		return nil, err
	}
	rep.Changed = true
	if _, err := exec.LookPath("adalmao"); err != nil {
		rep.Notes = append(rep.Notes, "`adalmao` is not on PATH; hooks use the absolute path "+exe+". Add its folder to PATH for CLI use.")
	}
	if strings.ContainsAny(exe, " \t") {
		rep.Notes = append(rep.Notes, "the executable path contains spaces and is double-quoted in the hook command; if hooks do not fire, move adalmao to a path without spaces and re-run install.")
	}
	rep.Notes = append(rep.Notes, "restart AdaL for hook changes to take effect")
	return rep, nil
}

// Uninstall removes only AdaLmao-owned hooks.
func Uninstall() (*Report, error) {
	rep := &Report{SettingsPath: SettingsPath()}
	m, orig, err := load()
	if err != nil {
		return nil, err
	}
	if orig == nil || len(m["hooks"]) == 0 {
		rep.Notes = append(rep.Notes, "no hooks found; nothing to remove")
		return rep, nil
	}
	h, err := parseHooks(m["hooks"])
	if err != nil {
		return nil, err
	}
	total := 0
	for ev, groups := range h {
		kept, n := stripOwned(groups)
		if n > 0 {
			total += n
			rep.Removed = append(rep.Removed, ev)
		}
		if len(kept) == 0 {
			delete(h, ev)
		} else {
			h[ev] = kept
		}
	}
	if total == 0 {
		rep.Notes = append(rep.Notes, "no AdaLmao hooks found; nothing to remove")
		return rep, nil
	}
	if len(h) == 0 {
		delete(m, "hooks")
	} else {
		b, err := encode(h)
		if err != nil {
			return nil, err
		}
		m["hooks"] = json.RawMessage(bytes.TrimSpace(b))
	}
	if err := write(m, orig, rep); err != nil {
		return nil, err
	}
	rep.Changed = true
	rep.Notes = append(rep.Notes, "your data in "+config.Dir()+" was kept (delete it manually or run `adalmao uninstall --purge`)", "restart AdaL for changes to take effect")
	return rep, nil
}

// Registered returns the events that currently have an AdaLmao hook and the command used.
func Registered() map[string]string {
	out := map[string]string{}
	m, _, err := load()
	if err != nil {
		return out
	}
	h, err := parseHooks(m["hooks"])
	if err != nil {
		return out
	}
	for ev, groups := range h {
		for _, g := range groups {
			gm, _ := g.(map[string]any)
			hs, _ := gm["hooks"].([]any)
			for _, x := range hs {
				hm, _ := x.(map[string]any)
				if cmd, _ := hm["command"].(string); owned(cmd) {
					out[ev] = cmd
				}
			}
		}
	}
	return out
}
