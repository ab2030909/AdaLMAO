package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/adalmao/adalmao/internal/attest"
	"github.com/adalmao/adalmao/internal/budget"
	"github.com/adalmao/adalmao/internal/config"
	"github.com/adalmao/adalmao/internal/decision"
	"github.com/adalmao/adalmao/internal/git"
	"github.com/adalmao/adalmao/internal/hooks"
	"github.com/adalmao/adalmao/internal/installer"
	"github.com/adalmao/adalmao/internal/modes"
	"github.com/adalmao/adalmao/internal/preflight"
	"github.com/adalmao/adalmao/internal/sentinel"
	"github.com/adalmao/adalmao/internal/tune"
)

func loadCfg() (*config.Config, bool) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "adalmao: warning: %v\n", err)
		if cfg == nil {
			cfg = config.Default()
		}
	}
	return cfg, err == nil
}

func cwd() string {
	d, err := os.Getwd()
	if err != nil {
		return "."
	}
	return d
}

func onoff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// ---- install / uninstall ----

func cmdInstall(_ []string) int {
	exe, err := os.Executable()
	if err != nil {
		return errf("cannot resolve executable path: %v", err)
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	if _, err := os.Stat(config.Path()); os.IsNotExist(err) {
		if err := config.Default().Save(); err != nil {
			return errf("cannot create config: %v", err)
		}
		fmt.Fprintf(stdout, "[✓] Created config %s\n", config.Path())
	}
	rep, err := installer.Install(exe)
	if err != nil {
		return errf("install failed: %v", err)
	}
	fmt.Fprintln(stdout, "😂 AdaLmao is watching your back.")
	fmt.Fprintf(stdout, "\nAdaL settings: %s\n", rep.SettingsPath)
	if rep.Backup != "" {
		fmt.Fprintf(stdout, "Backup:        %s\n", rep.Backup)
	}
	for _, ev := range rep.Added {
		fmt.Fprintf(stdout, "[✓] Registered %s hook\n", ev)
	}
	for _, n := range rep.Notes {
		fmt.Fprintf(stdout, "ℹ  %s\n", n)
	}
	fmt.Fprintln(stdout, "\nNote: AdaL hooks fail open (timeouts/launch failures allow the action). This is a guardrail layer, not a sandbox.")
	return 0
}

func cmdUninstall(args []string) int {
	rep, err := installer.Uninstall()
	if err != nil {
		return errf("uninstall failed: %v", err)
	}
	if rep.Backup != "" {
		fmt.Fprintf(stdout, "Backup: %s\n", rep.Backup)
	}
	for _, ev := range rep.Removed {
		fmt.Fprintf(stdout, "[✓] Removed AdaLmao %s hook\n", ev)
	}
	for _, n := range rep.Notes {
		fmt.Fprintf(stdout, "ℹ  %s\n", n)
	}
	if len(args) > 0 && args[0] == "--purge" {
		if err := os.RemoveAll(config.Dir()); err != nil {
			return errf("could not remove %s: %v", config.Dir(), err)
		}
		fmt.Fprintf(stdout, "[✓] Deleted %s\n", config.Dir())
	}
	return 0
}

// ---- status ----

func cmdStatus(_ []string) int {
	cfg, _ := loadCfg()
	wd := cwd()
	fmt.Fprintf(stdout, "😂 AdaLmao v%s — status\n\n", Version)
	reg := installer.Registered()
	for _, ev := range hooks.Events {
		mark := "[ ]"
		if reg[ev] != "" {
			mark = "[✓]"
		}
		fmt.Fprintf(stdout, "%s hook %s\n", mark, ev)
	}
	if len(reg) == 0 {
		fmt.Fprintln(stdout, "    (not installed — run `adalmao install`)")
	}
	fmt.Fprintf(stdout, "\nEnabled:    %s\n", onoff(cfg.General.Enabled))
	if m := modes.Ordered(cfg.General.ActiveModes); len(m) > 0 {
		fmt.Fprintf(stdout, "Modes:      %s\n", join(m))
	} else {
		fmt.Fprintln(stdout, "Modes:      none")
	}
	fmt.Fprintf(stdout, "Guardrails: %s\nEnforce:    %s\nSentinel:   %s (severity %s)\n", onoff(cfg.Guardrails.Enabled), onoff(cfg.Enforce.Enabled), onoff(cfg.Sentinel.Enabled), cfg.Sentinel.Severity)
	fmt.Fprintf(stdout, "\nProject:    %s\n", wd)
	if root := git.Root(wd); root != "" {
		fmt.Fprintf(stdout, "Git branch: %s\nGit state:  %s\n", git.Branch(root), git.DirtySummary(root))
	} else {
		fmt.Fprintln(stdout, "Git:        not a git repository")
	}
	if s := budget.LatestSession(); s != "" {
		if u, ok := budget.Load(s); ok {
			fmt.Fprintf(stdout, "Usage:      %d in / %d out tokens (session %s)\n", u.InputTokens, u.OutputTokens, s)
			warned := u.Warned
			if warned == "" {
				warned = "none"
			}
			fmt.Fprintf(stdout, "Budget:     limit $%.2f, warning state: %s\n", cfg.Budget.SessionLimit, warned)
		}
	} else {
		fmt.Fprintln(stdout, "Usage:      Usage data unavailable")
	}
	fmt.Fprintln(stdout, "\nNotes:")
	fmt.Fprintln(stdout, "- AdaL hooks fail open on timeout or launch failure: protection is best-effort.")
	fmt.Fprintln(stdout, "- AdaL has no documented status-line hook, so there is no live HUD; use this command.")
	return 0
}

// ---- modes ----

func cmdModes(_ []string) int {
	cfg, _ := loadCfg()
	act := map[string]bool{}
	for _, m := range modes.Ordered(cfg.General.ActiveModes) {
		act[m] = true
	}
	fmt.Fprintln(stdout, "😂 AdaLmao modes (max 6 active; listed by precedence):")
	for _, m := range modes.Priority {
		mark := "[ ]"
		if act[m] {
			mark = "[✓]"
		}
		fmt.Fprintf(stdout, "  %s %-15s %s\n", mark, m, modes.Description(m))
	}
	for _, n := range modes.Conflicts(cfg.General.ActiveModes) {
		fmt.Fprintf(stdout, "\n⚠  %s\n", n)
	}
	fmt.Fprintln(stdout, "\nActive modes are injected into every prompt via the UserPromptSubmit hook (requires `adalmao install`).")
	return 0
}

func cmdMode(args []string) int {
	if len(args) == 0 || (args[0] != "on" && args[0] != "off") {
		return errf("usage: adalmao mode on <names...> | adalmao mode off [names...]")
	}
	cfg, _ := loadCfg()
	cur := modes.Ordered(cfg.General.ActiveModes)
	names := args[1:]
	switch args[0] {
	case "on":
		if len(names) == 0 {
			return errf("usage: adalmao mode on <names...> (available: %s)", join(modes.Priority))
		}
		cur = append(cur, names...)
	case "off":
		if len(names) == 0 {
			cur = nil
		} else {
			drop := map[string]bool{}
			for _, n := range names {
				n = modes.Normalize(n)
				if !modes.Valid(n) {
					return errf("unknown mode %q", n)
				}
				drop[n] = true
			}
			var keep []string
			for _, c := range cur {
				if !drop[c] {
					keep = append(keep, c)
				}
			}
			cur = keep
		}
	}
	if err := modes.Set(cfg, cur); err != nil {
		return errf("%v", err)
	}
	if now := modes.Ordered(cfg.General.ActiveModes); len(now) > 0 {
		fmt.Fprintf(stdout, "[✓] Active modes: %s\n", join(now))
	} else {
		fmt.Fprintln(stdout, "[✓] No modes active")
	}
	for _, n := range modes.Conflicts(cfg.General.ActiveModes) {
		fmt.Fprintf(stdout, "⚠  %s\n", n)
	}
	return 0
}

// ---- guardrails / enforce / sentinel ----

func cmdGuardrails(args []string) int {
	cfg, _ := loadCfg()
	sub := "status"
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "enable", "disable":
		cfg.Guardrails.Enabled = sub == "enable"
		if err := cfg.Save(); err != nil {
			return errf("%v", err)
		}
		fmt.Fprintf(stdout, "[✓] Guardrails %sd\n", sub)
	case "protect", "unprotect":
		if len(args) < 2 {
			return errf("usage: adalmao guardrails %s <path-or-glob>", sub)
		}
		p := filepath.ToSlash(args[1])
		var out []string
		found := false
		for _, e := range cfg.Guardrails.ProtectedPaths {
			if e == p {
				found = true
				if sub == "unprotect" {
					continue
				}
			}
			out = append(out, e)
		}
		if sub == "protect" && !found {
			out = append(out, p)
		}
		cfg.Guardrails.ProtectedPaths = out
		if err := cfg.Save(); err != nil {
			return errf("%v", err)
		}
		fmt.Fprintf(stdout, "[✓] %sed %s (use a trailing / for directories)\n", sub, p)
	case "status":
		g := cfg.Guardrails
		fmt.Fprintf(stdout, "Guardrails: %s\n", onoff(g.Enabled))
		fmt.Fprintf(stdout, "  protect_env_files:      %v\n  protect_git_directory:  %v\n  protect_tests:          %v\n  protect_config_files:   %v\n", g.ProtectEnvFiles, g.ProtectGitDir, g.ProtectTests, g.ProtectConfigFiles)
		fmt.Fprintf(stdout, "  warn_on_public_api_changes:    %v\n  warn_on_default_value_changes: %v\n", g.WarnOnPublicAPIChanges, g.WarnOnDefaultValueChange)
		fmt.Fprintf(stdout, "  protected_paths: %s\n", orNone(g.ProtectedPaths))
		fmt.Fprintln(stdout, "\nPre-execution checks use only what the hook sees (command / file path / edit text). Effects that exist only after")
		fmt.Fprintln(stdout, "execution are covered by Sentinel (PostToolUse + git diff), not by guardrails.")
	default:
		return errf("usage: adalmao guardrails enable|disable|status|protect <path>|unprotect <path>")
	}
	return 0
}

func orNone(s []string) string {
	if len(s) == 0 {
		return "(none)"
	}
	return join(s)
}

var builtinDocs = [][2]string{
	{"no-hard-reset", "block `git reset --hard` (Rule A)"},
	{"protect-paths", "block deleting/moving/overwriting protected files: .git, .env, tests, critical config, your protected_paths (Rules B, D)"},
	{"no-dco-signoff", "block `git commit -s/--signoff` unless enforce.allow_dco_signoff (Rule C)"},
	{"git-clean-force", "block `git clean -f` (without dry run)"},
	{"no-force-push", "block `git push --force/-f/+ref`"},
	{"no-discard-all", "block `git checkout .`/`git restore .`/`checkout -f`"},
	{"git-history-rewrite", "block filter-branch, reflog expire, gc --prune=now"},
	{"catastrophic-delete", "block rm/del/Remove-Item on /, ~, ., .., drive roots, home or project root"},
	{"pipe-to-shell", "block `curl ... | sh` style downloads piped to a shell"},
	{"disk-destroy", "block mkfs, dd to devices, format X:, fork bombs"},
}

func cmdEnforce(args []string) int {
	cfg, _ := loadCfg()
	sub := "rules"
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "enable", "disable":
		cfg.Enforce.Enabled = sub == "enable"
		if err := cfg.Save(); err != nil {
			return errf("%v", err)
		}
		fmt.Fprintf(stdout, "[✓] Enforce %sd\n", sub)
	case "rules":
		off := map[string]bool{}
		for _, id := range cfg.Enforce.DisabledBuiltins {
			off[id] = true
		}
		fmt.Fprintf(stdout, "Enforce: %s\n\nBuilt-in rules:\n", onoff(cfg.Enforce.Enabled))
		for _, r := range builtinDocs {
			st := "on "
			if off[r[0]] {
				st = "off"
			}
			fmt.Fprintf(stdout, "  [%s] %-20s %s\n", st, r[0], r[1])
		}
		fmt.Fprintln(stdout, "\nUser rules (config enforce.rules):")
		if len(cfg.Enforce.Rules) == 0 {
			fmt.Fprintln(stdout, "  (none)")
		}
		for _, r := range cfg.Enforce.Rules {
			fmt.Fprintf(stdout, "  [%s] %-20s %s %q — %s\n", onoff(r.Enabled), r.ID, r.Action, r.Pattern, r.Reason)
		}
		fmt.Fprintln(stdout, "\nCommands are normalized (quotes, separators, sudo/env wrappers, `bash -c`, git global options) before matching.")
		fmt.Fprintln(stdout, "This is a practical filter, not proof against every shell trick. Test with: adalmao check -- <command>")
	default:
		return errf("usage: adalmao enforce enable|disable|rules")
	}
	return 0
}

func cmdSentinel(args []string) int {
	cfg, _ := loadCfg()
	sub := "status"
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "enable", "disable":
		cfg.Sentinel.Enabled = sub == "enable"
		if err := cfg.Save(); err != nil {
			return errf("%v", err)
		}
		fmt.Fprintf(stdout, "[✓] Sentinel %sd\n", sub)
	case "status":
		fmt.Fprintf(stdout, "Sentinel: %s (severity: %s)\n", onoff(cfg.Sentinel.Enabled), cfg.Sentinel.Severity)
		fs := sentinel.Check(cfg, cwd())
		if root := git.Root(cwd()); root == "" {
			fmt.Fprintln(stdout, "Not in a git repository: git-based checks unavailable.")
		} else if len(fs) == 0 {
			fmt.Fprintln(stdout, "[✓] No regressions detected in the current working tree.")
			if pct, f, ok := sentinel.Coverage(root); ok {
				fmt.Fprintf(stdout, "Coverage:  %.1f%% (from %s)\n", pct, f)
			} else {
				fmt.Fprintln(stdout, "Coverage:  unavailable (no coverage report found)")
			}
		}
		for _, f := range fs {
			fmt.Fprintf(stdout, "- [%s] %s\n", f.Sev, f.Msg)
		}
		fmt.Fprintln(stdout, "\nSentinel runs after tool calls (PostToolUse). A PostToolUse hook cannot undo changes; 'block' severity instructs the agent to stop.")
	default:
		return errf("usage: adalmao sentinel enable|disable|status")
	}
	return 0
}

// ---- party ----

func cmdParty(args []string) int {
	cfg, _ := loadCfg()
	if len(args) == 0 {
		args = []string{"profiles"}
	}
	switch args[0] {
	case "profiles":
		names := make([]string, 0, len(cfg.Profiles))
		for n := range cfg.Profiles {
			names = append(names, n)
		}
		sort.Strings(names)
		fmt.Fprintln(stdout, "Party profiles (named mode sets):")
		for _, n := range names {
			fmt.Fprintf(stdout, "  %-12s %s\n", n, join(cfg.Profiles[n]))
		}
		fmt.Fprintln(stdout, "\nApply one with `adalmao party use <name>`. Automatic per-agent assignment / parallel orchestration is NOT implemented:")
		fmt.Fprintln(stdout, "hooks are global and AdaL exposes no documented per-subagent hook scope. Profiles are manual presets for now.")
	case "show":
		if len(args) < 2 {
			return errf("usage: adalmao party show <name>")
		}
		p, ok := cfg.Profiles[args[1]]
		if !ok {
			return errf("unknown profile %q", args[1])
		}
		fmt.Fprintf(stdout, "Profile %s: %s\n\n%s\n", args[1], join(p), modes.Compose(p))
	case "use":
		if len(args) < 2 {
			return errf("usage: adalmao party use <name>")
		}
		p, ok := cfg.Profiles[args[1]]
		if !ok {
			return errf("unknown profile %q", args[1])
		}
		if err := modes.Set(cfg, p); err != nil {
			return errf("%v", err)
		}
		fmt.Fprintf(stdout, "[✓] Active modes now: %s (profile %s)\n", join(modes.Ordered(p)), args[1])
	default:
		return errf("usage: adalmao party profiles | show <name> | use <name>")
	}
	return 0
}

// ---- tune / budget / guard / preflight / attest ----

func cmdTune(_ []string) int {
	fmt.Fprint(stdout, tune.Report(cwd()))
	return 0
}

func cmdBudget(_ []string) int {
	cfg, _ := loadCfg()
	fmt.Fprint(stdout, budget.Report(cfg, ""))
	return 0
}

func cmdGuard(args []string) int {
	if len(args) > 0 && args[0] != "status" {
		return errf("usage: adalmao guard status")
	}
	cfg, _ := loadCfg()
	b := cfg.Budget
	fmt.Fprintf(stdout, "Guard: %s\n  session_limit:     $%.2f\n  warning_threshold: %.0f%%\n", onoff(b.Enabled), b.SessionLimit, b.WarningThreshold*100)
	if b.InputPricePerMTok <= 0 && b.OutputPricePerMTok <= 0 {
		fmt.Fprintln(stdout, "  prices: not set — cost-based warnings are inactive (set budget.input_price_per_mtok / output_price_per_mtok)")
	}
	if s := budget.LatestSession(); s != "" {
		if u, ok := budget.Load(s); ok {
			w := u.Warned
			if w == "" {
				w = "none"
			}
			fmt.Fprintf(stdout, "  latest session %s: warning state = %s\n", s, w)
		}
	} else {
		fmt.Fprintln(stdout, "  no usage recorded yet (Usage data unavailable)")
	}
	fmt.Fprintln(stdout, "Guard only warns (once per level). AdaL gives hooks no way to stop API spending.")
	return 0
}

func cmdPreflight(_ []string) int {
	cfg, _ := loadCfg()
	if !cfg.Git.PreflightEnabled {
		fmt.Fprintln(stdout, "Preflight is disabled (git.preflight_enabled=false); running anyway on request.")
	}
	r := preflight.Run(cfg, cwd())
	fmt.Fprint(stdout, r.Text)
	if r.Problems > 0 {
		return 1
	}
	return 0
}

func cmdAttest(args []string) int {
	cfg, _ := loadCfg()
	if len(args) == 0 {
		fmt.Fprint(stdout, attest.Report(cfg, cwd()))
		return 0
	}
	switch args[0] {
	case "install-hook":
		p, err := attest.InstallHook(cfg, cwd())
		if err != nil {
			return errf("%v", err)
		}
		fmt.Fprintf(stdout, "[✓] Installed prepare-commit-msg hook: %s\n", p)
	case "remove-hook":
		p, err := attest.RemoveHook(cwd())
		if err != nil {
			return errf("%v", err)
		}
		fmt.Fprintf(stdout, "[✓] Removed %s\n", p)
	default:
		return errf("usage: adalmao attest [install-hook|remove-hook]")
	}
	return 0
}

// ---- check / log ----

func cmdCheck(args []string) int {
	cfg, _ := loadCfg()
	var ds []decision.Decision
	var subject string
	switch {
	case len(args) >= 2 && (args[0] == "--delete" || args[0] == "--write"):
		tool := "edit_file"
		if args[0] == "--delete" {
			tool = "delete_file"
		}
		subject = args[1]
		ds = hooks.Evaluate(cfg, cwd(), tool, map[string]any{"file_path": args[1]})
	default:
		if len(args) > 0 && args[0] == "--" {
			args = args[1:]
		}
		if len(args) == 0 {
			return errf("usage: adalmao check -- <shell command>  |  adalmao check --delete|--write <path>")
		}
		subject = strings.Join(args, " ")
		ds = hooks.Evaluate(cfg, cwd(), "bash", map[string]any{"command": subject})
	}
	top := decision.Max(ds)
	fmt.Fprintf(stdout, "Input:   %s\nVerdict: %s\n", subject, strings.ToUpper(top.Level.String()))
	for _, d := range ds {
		fmt.Fprintf(stdout, "  - [%s/%s] %s: %s", d.Source, d.Level, d.Rule, d.Reason)
		if d.Suggestion != "" {
			fmt.Fprintf(stdout, " (alternative: %s)", d.Suggestion)
		}
		fmt.Fprintln(stdout)
	}
	if top.Level == decision.Blocked {
		return 2
	}
	return 0
}

func cmdLog(_ []string) int {
	b, err := os.ReadFile(filepath.Join(config.Dir(), "decisions.log"))
	if err != nil {
		fmt.Fprintln(stdout, "No decisions logged yet.")
		return 0
	}
	ls := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(ls) > 20 {
		ls = ls[len(ls)-20:]
	}
	for _, l := range ls {
		fmt.Fprintln(stdout, l)
	}
	return 0
}

// ---- config ----

func cmdConfig(args []string) int {
	sub := "show"
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "path":
		fmt.Fprintln(stdout, config.Path())
	case "show":
		cfg, _ := loadCfg()
		b, _ := json.MarshalIndent(cfg, "", "  ")
		fmt.Fprintf(stdout, "# %s\n%s\n", config.Path(), b)
	case "validate":
		if _, err := config.Load(); err != nil {
			return errf("%v", err)
		}
		fmt.Fprintln(stdout, "[✓] Config is valid")
	case "set":
		if len(args) < 3 {
			return errf("usage: adalmao config set <dotted.key> <json-or-string>   e.g. config set budget.session_limit 3")
		}
		return configSet(args[1], strings.Join(args[2:], " "))
	default:
		return errf("usage: adalmao config [path|show|validate|set <key> <value>]")
	}
	return 0
}

func configSet(key, val string) int {
	cfg, _ := loadCfg()
	b, _ := json.Marshal(cfg)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	parts := strings.Split(key, ".")
	cur := m
	for i, p := range parts {
		if i == len(parts)-1 {
			if _, ok := cur[p]; !ok && !(len(parts) > 1 && parts[0] == "profiles") {
				return errf("unknown config key %q", key)
			}
			var v any
			if json.Unmarshal([]byte(val), &v) != nil {
				v = val
			}
			cur[p] = v
			break
		}
		next, ok := cur[p].(map[string]any)
		if !ok {
			return errf("unknown config key %q", key)
		}
		cur = next
	}
	nb, _ := json.Marshal(m)
	// decode over a copy so type errors are caught before saving
	cand := *cfg
	if err := json.Unmarshal(nb, &cand); err != nil {
		return errf("invalid value for %s: %v", key, err)
	}
	if err := cand.Validate(); err != nil {
		return errf("%v", err)
	}
	if err := cand.Save(); err != nil {
		return errf("%v", err)
	}
	fmt.Fprintf(stdout, "[✓] %s = %s\n", key, val)
	return 0
}

// ---- spank ----

func cmdSpank(args []string) int {
	if len(args) == 0 || args[0] != "explain" {
		return errf("usage: adalmao spank explain")
	}
	fmt.Fprintln(stdout, "😂 AdaLmao Spank — keeping long AdaL jobs alive while you're away")
	fmt.Fprintln(stdout, "\nWhat is actually possible: AdaL runs as a normal process (interactive, or headless via `adal -q \"...\"`).")
	fmt.Fprintln(stdout, "It stops if the process is killed, the terminal closes, the machine sleeps, or it shuts down. AdaLmao does not")
	fmt.Fprintln(stdout, "change OS power settings or run privileged commands; options below are for YOU to run if you choose.")
	switch runtimeOS() {
	case "windows":
		fmt.Fprintln(stdout, "\nWindows:")
		fmt.Fprintln(stdout, "  - Keep the terminal open; Settings > System > Power: set sleep to Never while plugged in.")
		fmt.Fprintln(stdout, "  - Laptop lid: Control Panel > Power Options > 'Choose what closing the lid does' (by default it SLEEPS).")
		fmt.Fprintln(stdout, "  - Headless background run: Start-Process adal -ArgumentList '-q','<task>' -WindowStyle Hidden  (dies on logoff/shutdown).")
		if out := capture("powercfg", "/getactivescheme"); out != "" {
			fmt.Fprintf(stdout, "  [read-only check] %s\n", out)
		}
	case "darwin":
		fmt.Fprintln(stdout, "\nmacOS:")
		fmt.Fprintln(stdout, "  - Prevent sleep while a command runs:  caffeinate -dimsu adal -q \"<task>\"")
		fmt.Fprintln(stdout, "  - Closing the lid sleeps a laptop unless on power with an external display (clamshell mode).")
		fmt.Fprintln(stdout, "  - Survive terminal closing: run inside tmux or `nohup`.")
		if out := capture("pmset", "-g", "assertions"); out != "" {
			fmt.Fprintln(stdout, "  [read-only check] pmset assertions available")
		}
	default:
		fmt.Fprintln(stdout, "\nLinux:")
		fmt.Fprintln(stdout, "  - Survive terminal closing: tmux new -s adal   (detach with Ctrl-b d, reattach with tmux attach -t adal)")
		fmt.Fprintln(stdout, "  - Prevent sleep while a command runs:  systemd-inhibit --what=sleep:idle adal -q \"<task>\"")
		fmt.Fprintln(stdout, "  - Laptop lid: see HandleLidSwitch in /etc/systemd/logind.conf (needs root; AdaLmao will not touch it).")
	}
	if os.Getenv("TMUX") != "" || os.Getenv("STY") != "" {
		fmt.Fprintln(stdout, "\n[✓] You are inside a tmux/screen session: closing the terminal will not kill your job.")
	} else {
		fmt.Fprintln(stdout, "\n[ ] Not inside tmux/screen (or not detectable).")
	}
	fmt.Fprintln(stdout, "\nWarning: a sleeping or powered-off machine stops everything. Remember that AdaL needs network access to reach its model.")
	return 0
}

// ---- hook entry point ----

func cmdHook(args []string) int {
	data, _ := io.ReadAll(os.Stdin)
	ev := ""
	if len(args) > 0 {
		ev = hooks.EventFromArg(args[0])
	}
	res := hooks.Handle(ev, data)
	if res.Stdout != "" {
		fmt.Fprintln(stdout, res.Stdout)
	}
	if res.Stderr != "" {
		fmt.Fprintln(stderr, res.Stderr)
	}
	return res.Exit
}
