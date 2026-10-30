// Package enforce is the deterministic rule engine that mechanically blocks specific commands.
package enforce

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/adalmao/adalmao/internal/config"
	"github.com/adalmao/adalmao/internal/decision"
	"github.com/adalmao/adalmao/internal/guardrails"
)

// BuiltinIDs lists the built-in rules (can be disabled via enforce.disabled_builtins).
var BuiltinIDs = []string{
	"no-hard-reset", "git-clean-force", "no-force-push", "no-discard-all", "no-dco-signoff",
	"git-history-rewrite", "protect-paths", "catastrophic-delete", "pipe-to-shell", "disk-destroy",
}

func blk(rule, reason, sugg string) decision.Decision {
	return decision.Decision{Level: decision.Blocked, Rule: rule, Reason: reason, Suggestion: sugg, Source: "enforce"}
}
func lvl(l decision.Level, rule, reason, sugg string) decision.Decision {
	return decision.Decision{Level: l, Rule: rule, Reason: reason, Suggestion: sugg, Source: "enforce"}
}

// EvalBash evaluates a shell command against built-in and user-defined rules.
func EvalBash(cfg *config.Config, cwd, command string) []decision.Decision {
	if !cfg.Enforce.Enabled {
		return nil
	}
	off := map[string]bool{}
	for _, id := range cfg.Enforce.DisabledBuiltins {
		off[id] = true
	}
	cmds := Parse(command)
	var out []decision.Decision
	for i, c := range cmds {
		for _, dd := range evalCmd(cfg, cwd, c, cmds, i) {
			if !off[dd.Rule] {
				out = append(out, dd)
			}
		}
	}
	if strings.Contains(strings.ReplaceAll(command, " ", ""), ":(){:|:&};:") && !off["disk-destroy"] {
		out = append(out, blk("disk-destroy", "Fork bomb detected.", ""))
	}
	out = append(out, userRules(cfg, command, cmds)...)
	return out
}

func userRules(cfg *config.Config, raw string, cmds []Cmd) []decision.Decision {
	var out []decision.Decision
	for _, r := range cfg.Enforce.Rules {
		if !r.Enabled {
			continue
		}
		matched := false
		if r.Regex {
			re, err := regexp.Compile("(?i)" + r.Pattern)
			if err != nil {
				continue
			}
			matched = re.MatchString(raw)
			for _, c := range cmds {
				matched = matched || re.MatchString(c.Text)
			}
		} else {
			p := strings.Join(strings.Fields(strings.ToLower(r.Pattern)), " ")
			for _, c := range cmds {
				if c.Text == p || strings.HasPrefix(c.Text, p+" ") {
					matched = true
				}
			}
		}
		if !matched {
			continue
		}
		reason := r.Reason
		if reason == "" {
			reason = "Matched user rule " + r.ID
		}
		l := decision.Review
		if r.Action == "block" {
			l = decision.Blocked
		}
		out = append(out, lvl(l, r.ID, reason, ""))
	}
	return out
}

func has(args []string, flags ...string) bool {
	for _, a := range args {
		for _, f := range flags {
			if a == f {
				return true
			}
		}
	}
	return false
}

// shortCluster reports whether any single-dash cluster (e.g. -fd) contains letter l,
// stopping at argument-taking letters in stop.
func shortCluster(args []string, l byte, stop string) bool {
	for _, a := range args {
		if len(a) < 2 || a[0] != '-' || a[1] == '-' {
			continue
		}
		for i := 1; i < len(a); i++ {
			if a[i] == l {
				return true
			}
			if strings.IndexByte(stop, a[i]) >= 0 {
				break
			}
		}
	}
	return false
}

func evalCmd(cfg *config.Config, cwd string, c Cmd, all []Cmd, idx int) []decision.Decision {
	var out []decision.Decision
	if c.Prog == "git" {
		out = append(out, evalGit(cfg, c)...)
	}
	out = append(out, evalFS(cfg, cwd, c)...)

	// pipe to shell: curl ... | sh
	if c.Op == "|" && idx+1 < len(all) {
		n := all[idx+1]
		fetch := c.Prog == "curl" || c.Prog == "wget" || c.Prog == "invoke-webrequest" || c.Prog == "iwr" || c.Prog == "irm" || c.Prog == "invoke-restmethod"
		sh := shellProgs[n.Prog] || n.Prog == "iex" || n.Prog == "invoke-expression" || n.Prog == "python" || n.Prog == "python3" || n.Prog == "powershell" || n.Prog == "pwsh"
		if fetch && sh {
			out = append(out, blk("pipe-to-shell", "Piping a download straight into a shell/interpreter is blocked.", "Download the script, review it, then run it yourself."))
		}
	}
	switch {
	case strings.HasPrefix(c.Prog, "mkfs"), c.Prog == "format" && len(c.Args) > 0 && len(c.Args[0]) == 2 && c.Args[0][1] == ':':
		out = append(out, blk("disk-destroy", "Formatting a disk is blocked.", ""))
	case c.Prog == "dd":
		for _, a := range c.Args {
			if strings.HasPrefix(a, "of=/dev/") || strings.HasPrefix(strings.ToLower(a), "of=\\\\.\\") {
				out = append(out, blk("disk-destroy", "Writing directly to a block device is blocked.", ""))
			}
		}
	}
	return out
}

func evalGit(cfg *config.Config, c Cmd) []decision.Decision {
	var out []decision.Decision
	a := c.Args
	switch c.Sub {
	case "reset":
		if has(a, "--hard") {
			out = append(out, blk("no-hard-reset", "`git reset --hard` irreversibly discards uncommitted work.", "Use `git stash` or `git reset --soft`/`--mixed`, or ask the user to run it."))
		}
	case "clean":
		if (shortCluster(a, 'f', "") || has(a, "--force")) && !has(a, "-n", "--dry-run") && !shortCluster(a, 'n', "") {
			out = append(out, blk("git-clean-force", "`git clean -f` permanently deletes untracked files.", "Run `git clean -n` first for a dry run and let the user confirm."))
		}
	case "push":
		force := has(a, "--force", "-f") || shortCluster(a, 'f', "o")
		lease := false
		for _, x := range a {
			if strings.HasPrefix(x, "--force-with-lease") || strings.HasPrefix(x, "--force-if-includes") {
				lease = true
			}
			if strings.HasPrefix(x, "+") && len(x) > 1 && !strings.HasPrefix(x, "+-") {
				force = true
			}
		}
		if force {
			out = append(out, blk("no-force-push", "Force-pushing can overwrite remote history.", "Use a normal push, or ask the user to force-push after review."))
		} else if lease || has(a, "--delete", "-d") {
			out = append(out, lvl(decision.Risky, "push-risky", "This push can rewrite or delete remote refs.", ""))
		}
	case "checkout", "restore":
		discardAll := false
		for i, x := range a {
			if x == "." || x == ":/" || x == "*" {
				discardAll = true
			}
			_ = i
		}
		if c.Sub == "checkout" && (has(a, "-f", "--force") || shortCluster(a, 'f', "")) {
			discardAll = true
		}
		if discardAll && !has(a, "-b", "-B") {
			out = append(out, blk("no-discard-all", "This would discard all uncommitted changes in the working tree.", "Stash first (`git stash`) or restore specific files after confirming with the user."))
		} else if has(a, "--") || c.Sub == "restore" {
			out = append(out, lvl(decision.Risky, "git-discard-file", "Discarding uncommitted changes to specific files.", "Make sure nothing there is unsaved work."))
		}
	case "stash":
		if len(a) > 0 && (a[0] == "drop" || a[0] == "clear") {
			out = append(out, lvl(decision.Risky, "stash-drop", "Dropping stashes loses saved work.", ""))
		}
	case "branch":
		if has(a, "-D") || (has(a, "--delete") && has(a, "--force")) {
			out = append(out, lvl(decision.Risky, "branch-delete", "Force-deleting a branch can lose unmerged commits.", ""))
		}
	case "filter-branch", "filter-repo":
		out = append(out, blk("git-history-rewrite", "Rewriting repository history is blocked.", "Ask the user to do this manually."))
	case "reflog":
		if len(a) > 0 && (a[0] == "expire" || a[0] == "delete") {
			out = append(out, blk("git-history-rewrite", "Expiring the reflog removes your recovery points.", ""))
		}
	case "gc":
		for _, x := range a {
			if strings.HasPrefix(x, "--prune=now") {
				out = append(out, blk("git-history-rewrite", "Pruning unreachable objects now removes recovery points.", ""))
			}
		}
	case "rebase":
		out = append(out, lvl(decision.Risky, "git-rebase", "Rebasing rewrites commit history.", "Run `adalmao preflight` first and review conflicts."))
	case "commit":
		if !cfg.Enforce.AllowDCOSignoff {
			signoff := has(a, "--signoff") || shortCluster(a, 's', "mFCctu")
			for i, x := range a {
				if x == "--trailer" && i+1 < len(a) && strings.HasPrefix(strings.ToLower(a[i+1]), "signed-off-by") {
					signoff = true
				}
			}
			if signoff {
				out = append(out, blk("no-dco-signoff", "Signing off (DCO) is a personal legal attestation; the agent must not do it for you.", "Commit without -s; add the sign-off yourself, or set enforce.allow_dco_signoff=true."))
			}
		}
		if has(a, "--amend") {
			out = append(out, lvl(decision.Risky, "git-amend", "Amending rewrites the last commit.", ""))
		}
	}
	return out
}

var removeProgs = map[string]bool{"rm": true, "rmdir": true, "del": true, "erase": true, "rd": true, "remove-item": true, "ri": true, "unlink": true, "rimraf": true, "shred": true}
var moveProgs = map[string]bool{"mv": true, "move": true, "ren": true, "rename": true, "move-item": true, "mi": true}
var copyProgs = map[string]bool{"cp": true, "copy": true, "copy-item": true, "xcopy": true, "robocopy": true, "install": true}
var writeProgs = map[string]bool{"tee": true, "truncate": true, "set-content": true, "add-content": true, "out-file": true, "clear-content": true, "new-item": true, "sc": true, "ac": true}
var readProgs = map[string]bool{"cat": true, "type": true, "get-content": true, "gc": true, "less": true, "more": true, "head": true, "tail": true, "bat": true, "printenv": true}
var psValueFlags = map[string]bool{"-path": true, "-literalpath": true, "-filter": true, "-include": true, "-exclude": true, "-destination": true, "-value": true, "-encoding": true}

// operands extracts non-flag arguments (best effort for POSIX, cmd and PowerShell).
func operands(c Cmd) []string {
	var ops []string
	ps := strings.Contains(c.Prog, "-") // Verb-Noun cmdlets
	for i := 0; i < len(c.Args); i++ {
		a := c.Args[i]
		la := strings.ToLower(a)
		switch {
		case ps && psValueFlags[la]:
			if i+1 < len(c.Args) && (la == "-path" || la == "-literalpath") {
				ops = append(ops, c.Args[i+1])
			}
			i++
		case strings.HasPrefix(a, "-") && len(a) > 1:
		case (c.Prog == "del" || c.Prog == "erase" || c.Prog == "rd" || c.Prog == "rmdir" || c.Prog == "move" || c.Prog == "copy" || c.Prog == "xcopy") && len(a) == 2 && a[0] == '/':
		case strings.HasPrefix(a, "of="):
			ops = append(ops, a[3:])
		default:
			ops = append(ops, a)
		}
	}
	return ops
}

func recursive(c Cmd) bool {
	for _, a := range c.Args {
		la := strings.ToLower(a)
		if la == "-recurse" || la == "--recursive" || la == "-r" || la == "-rf" || la == "-fr" || la == "/s" || la == "-recursive" {
			return true
		}
		if len(a) > 1 && a[0] == '-' && a[1] != '-' && strings.ContainsAny(a, "rR") && !strings.Contains(la, "recurse") {
			return true
		}
	}
	return false
}

var rootish = map[string]bool{"/": true, "/*": true, "~": true, "~/": true, "~/*": true, "$home": true, "${home}": true, "%userprofile%": true, "*": true, ".": true, "..": true, "./": true, "./*": true, ".*": true, "c:\\": true, "c:/": true, "c:": true, "$env:userprofile": true, "/home": true, "/usr": true, "/etc": true, "/var": true, "/bin": true}

func evalFS(cfg *config.Config, cwd string, c Cmd) []decision.Decision {
	var out []decision.Decision
	g := cfg.Guardrails
	check := func(p string, kind string) {
		rel := guardrails.Norm(p, cwd)
		if rel == "" {
			return
		}
		why := ""
		rule := "protect-paths"
		switch {
		case guardrails.IsSelfConfig(p, cwd):
			why = "AdaL/AdaLmao configuration is off limits to the agent"
		case g.Enabled && g.ProtectGitDir && guardrails.InGitDir(rel):
			why = "protected project directory (.git)"
		case g.Enabled && g.ProtectEnvFiles && guardrails.IsEnvFile(rel):
			why = "environment file"
		case g.Enabled && func() bool { _, ok := guardrails.UserProtected(cfg, rel); return ok }():
			why = "user-protected path"
		case (kind == "delete" || kind == "move") && g.Enabled && g.ProtectTests && (guardrails.IsTestFile(rel) || guardrails.IsTestFile(rel+"/x")):
			why = "test file"
		case (kind == "delete" || kind == "move") && g.Enabled && g.ProtectConfigFiles && guardrails.IsConfigFile(rel):
			why = "critical config file"
		}
		if why != "" {
			out = append(out, blk(rule, fmt.Sprintf("Refusing to %s %s (%s).", kind, rel, why), "Ask the user to do this manually if it is really intended."))
		}
	}

	switch {
	case removeProgs[c.Prog]:
		for _, t := range operands(c) {
			lt := strings.ToLower(strings.TrimRight(t, "\\"))
			if rootish[lt] || rootish[strings.ToLower(t)] || isHomeOrRoot(t, cwd) {
				out = append(out, blk("catastrophic-delete", fmt.Sprintf("Deleting %q would wipe a root, home or the whole project.", t), "Delete specific files by name instead."))
				continue
			}
			check(t, "delete")
		}
		if len(out) == 0 && recursive(c) {
			out = append(out, lvl(decision.Risky, "recursive-delete", "Recursive delete.", "Double-check the target path."))
		}
	case moveProgs[c.Prog]:
		ops := operands(c)
		if len(ops) > 1 {
			for _, t := range ops[:len(ops)-1] {
				check(t, "move")
			}
		}
		if len(ops) > 0 {
			check(ops[len(ops)-1], "overwrite")
		}
	case copyProgs[c.Prog]:
		if ops := operands(c); len(ops) > 1 {
			check(ops[len(ops)-1], "overwrite")
		}
	case writeProgs[c.Prog]:
		for _, t := range operands(c) {
			check(t, "overwrite")
		}
	case c.Prog == "sed" || c.Prog == "perl" || c.Prog == "awk":
		inplace := false
		for _, a := range c.Args {
			if strings.HasPrefix(a, "-i") || a == "--in-place" || (c.Prog == "perl" && strings.HasPrefix(a, "-") && strings.Contains(a, "i") && strings.Contains(a, "p")) {
				inplace = true
			}
		}
		if inplace {
			for _, t := range operands(c) {
				check(t, "overwrite")
			}
		}
	case readProgs[c.Prog]:
		if g.Enabled && g.ProtectEnvFiles {
			for _, t := range operands(c) {
				if guardrails.IsEnvFile(guardrails.Norm(t, cwd)) {
					out = append(out, lvl(decision.Review, "env-read", "Reading an env file may expose secrets into the conversation: "+t, "Ask for variable names instead of values."))
				}
			}
		}
	}
	for _, r := range c.Redirects {
		check(r, "overwrite")
	}
	return out
}

func isHomeOrRoot(t, cwd string) bool {
	t = strings.Trim(t, "\"'")
	if t == "" {
		return false
	}
	if strings.ContainsAny(t, "*?") {
		// wildcards only matter when they are the whole operand (handled by rootish)
		return false
	}
	abs := t
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(cwd, abs)
	}
	abs = filepath.Clean(abs)
	if h, err := os.UserHomeDir(); err == nil && strings.EqualFold(abs, filepath.Clean(h)) {
		return true
	}
	if cwd != "" && strings.EqualFold(abs, filepath.Clean(cwd)) {
		return true
	}
	vol := filepath.VolumeName(abs)
	return abs == string(filepath.Separator) || (vol != "" && strings.EqualFold(abs, vol+string(filepath.Separator)))
}
