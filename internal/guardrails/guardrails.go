// Package guardrails classifies file operations (Safe / Review / Risky / Blocked) from tool input.
package guardrails

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/adalmao/adalmao/internal/config"
	"github.com/adalmao/adalmao/internal/decision"
)

type OpKind string

const (
	OpNone    OpKind = ""
	OpBash    OpKind = "bash"
	OpRead    OpKind = "read"
	OpCreate  OpKind = "create"
	OpEdit    OpKind = "edit"
	OpRewrite OpKind = "rewrite"
	OpDelete  OpKind = "delete"
)

var pathKeys = []string{"file_path", "path", "filepath", "file", "target_file", "filename", "target"}

func str(in map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := in[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// ClassifyTool maps a tool call onto an operation kind and target path.
// AdaL does not publish a tool-name list, so this is a name-heuristic plus input-field inspection.
func ClassifyTool(toolName string, in map[string]any) (OpKind, string) {
	n := strings.ToLower(toolName)
	if str(in, "command", "cmd") != "" && (strings.Contains(n, "bash") || strings.Contains(n, "shell") || strings.Contains(n, "exec") || strings.Contains(n, "command") || strings.Contains(n, "run")) {
		return OpBash, ""
	}
	p := str(in, pathKeys...)
	switch {
	case (strings.Contains(n, "delete") || strings.Contains(n, "remove")) && strings.Contains(n, "line"):
		return OpEdit, p // deleting lines inside a file is an edit, not a file deletion
	case strings.Contains(n, "delete") || strings.Contains(n, "remove"):
		return OpDelete, p
	case strings.Contains(n, "read") || strings.Contains(n, "view") || n == "cat":
		return OpRead, p
	case strings.Contains(n, "rewrite") || strings.Contains(n, "overwrite"):
		return OpRewrite, p
	case strings.Contains(n, "create") || strings.Contains(n, "write"):
		return OpCreate, p
	case strings.Contains(n, "edit") || strings.Contains(n, "replace") || strings.Contains(n, "insert") || strings.Contains(n, "patch"):
		return OpEdit, p
	}
	if str(in, "command", "cmd") != "" {
		return OpBash, ""
	}
	return OpNone, p
}

func d(l decision.Level, rule, reason, sugg string) decision.Decision {
	return decision.Decision{Level: l, Rule: rule, Reason: reason, Suggestion: sugg, Source: "guardrails"}
}

// EvalFile evaluates a file-oriented tool call.
func EvalFile(cfg *config.Config, cwd, toolName string, in map[string]any) []decision.Decision {
	g := cfg.Guardrails
	if !g.Enabled {
		return nil
	}
	kind, p := ClassifyTool(toolName, in)
	if kind == OpNone || kind == OpBash || p == "" {
		return nil
	}
	rel := Norm(p, cwd)
	writes := kind != OpRead
	var out []decision.Decision

	if writes && IsSelfConfig(p, cwd) {
		out = append(out, d(decision.Blocked, "protect-adal-config", "AdaL/AdaLmao configuration is off limits to the agent: "+rel, "Edit it yourself outside the agent, or use the `adalmao` CLI."))
	}
	if writes && g.ProtectGitDir && InGitDir(rel) {
		out = append(out, d(decision.Blocked, "protect-git-dir", "Protected project directory (.git): "+rel, "Use normal git commands instead of editing .git internals."))
	}
	if pat, ok := UserProtected(cfg, rel); ok && writes {
		out = append(out, d(decision.Blocked, "protected-path", fmt.Sprintf("%s matches protected path %q", rel, pat), "Ask the user to change this file or remove it from guardrails.protected_paths."))
	}
	if g.ProtectEnvFiles && IsEnvFile(rel) {
		if writes {
			out = append(out, d(decision.Blocked, "protect-env", "Environment files may hold secrets and are protected: "+rel, "Edit a .env.example file and let the user update the real .env."))
		} else {
			out = append(out, d(decision.Review, "env-read", "Reading an env file may expose secrets into the conversation: "+rel, "Ask for the variable names instead of values."))
		}
	}
	if g.ProtectTests && IsTestFile(rel) {
		switch kind {
		case OpDelete:
			out = append(out, d(decision.Blocked, "protect-tests", "Deleting a test file requires manual review: "+rel, "Fix or update the test rather than removing it."))
		case OpEdit, OpRewrite:
			out = append(out, d(decision.Review, "test-edit", "Modifying a test file: "+rel+". Make sure the test is not being weakened to pass.", ""))
		}
	}
	if g.ProtectConfigFiles && IsConfigFile(rel) {
		switch kind {
		case OpDelete:
			out = append(out, d(decision.Blocked, "protect-config", "Deleting a critical config file: "+rel, ""))
		case OpEdit, OpRewrite, OpCreate:
			out = append(out, d(decision.Review, "config-edit", "Changing a project config file: "+rel, "Confirm the change is part of the requested task."))
		}
	}
	if kind == OpDelete && len(out) == 0 {
		out = append(out, d(decision.Risky, "file-delete", "Deleting a file: "+rel, "Make sure it is tracked in git so it can be restored."))
	}
	if kind == OpRewrite {
		if _, err := os.Stat(absPath(p, cwd)); err == nil {
			out = append(out, d(decision.Review, "overwrite", "Whole-file overwrite of an existing file: "+rel, "Prefer targeted edits to preserve existing content."))
		}
	}
	if kind == OpEdit {
		old := str(in, "old_string", "old_str", "old_text", "old")
		nw := str(in, "new_string", "new_str", "new_text", "new")
		out = append(out, apiDecisions(cfg, rel, old, nw)...)
	}
	return out
}

func absPath(p, cwd string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(cwd, p)
}

var sigRe = regexp.MustCompile(`(?m)^\s*(?:export\s+)?(?:pub(?:lic)?\s+)?(?:async\s+)?(?:func|def|function|fn)\s+(?:\([^)]*\)\s*)?([A-Za-z_][A-Za-z0-9_]*)\s*(\([^)]*\))`)

func signatures(s string) map[string]string {
	m := map[string]string{}
	for _, g := range sigRe.FindAllStringSubmatch(s, -1) {
		m[g[1]] = strings.Join(strings.Fields(g[2]), " ")
	}
	return m
}

func isPublic(name string) bool {
	if name == "" || strings.HasPrefix(name, "_") {
		return false
	}
	return true
}

func apiDecisions(cfg *config.Config, rel, old, nw string) []decision.Decision {
	g := cfg.Guardrails
	if old == "" || (!g.WarnOnPublicAPIChanges && !g.WarnOnDefaultValueChange) {
		return nil
	}
	var out []decision.Decision
	os_, ns := signatures(old), signatures(nw)
	for name, oldSig := range os_ {
		if !isPublic(name) {
			continue
		}
		newSig, ok := ns[name]
		switch {
		case !ok && g.WarnOnPublicAPIChanges:
			out = append(out, d(decision.Risky, "api-removed", fmt.Sprintf("Function %q removed or renamed in %s", name, rel), "Check callers before removing public functions."))
		case ok && newSig != oldSig:
			if g.WarnOnDefaultValueChange && strings.Contains(oldSig+newSig, "=") && defaults(oldSig) != defaults(newSig) {
				out = append(out, d(decision.Review, "default-changed", fmt.Sprintf("Default value changed in %s() in %s", name, rel), "Default changes silently affect existing callers."))
			} else if g.WarnOnPublicAPIChanges {
				out = append(out, d(decision.Review, "api-signature", fmt.Sprintf("Signature of %s() changed in %s: %s -> %s", name, rel, oldSig, newSig), "Update all callers."))
			}
		}
	}
	return out
}

var defRe = regexp.MustCompile(`=\s*([^,)]+)`)

func defaults(sig string) string {
	var parts []string
	for _, m := range defRe.FindAllStringSubmatch(sig, -1) {
		parts = append(parts, strings.TrimSpace(m[1]))
	}
	return strings.Join(parts, "|")
}
