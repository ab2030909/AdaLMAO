// Package cli implements the adalmao command line (stdlib only).
package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// Version is set at build time via -ldflags "-X github.com/adalmao/adalmao/internal/cli.Version=..."
var Version = "0.1.0"

var (
	stdout io.Writer = os.Stdout
	stderr io.Writer = os.Stderr
)

type command struct {
	name    string
	summary string
	run     func(args []string) int
	hidden  bool
	status  string // "", "planned"
}

func commands() []command {
	return []command{
		{name: "install", summary: "Register AdaLmao hooks in AdaL (idempotent, backs up settings)", run: cmdInstall},
		{name: "uninstall", summary: "Remove only AdaLmao-owned hooks (--purge also deletes ~/.adalmao)", run: cmdUninstall},
		{name: "status", summary: "Show modes, guardrails, hooks, git state and budget", run: cmdStatus},
		{name: "modes", summary: "List available modes and which are active", run: cmdModes},
		{name: "mode", summary: "mode on <names...> | mode off [names...]", run: cmdMode},
		{name: "guardrails", summary: "guardrails enable|disable|status|protect <path>|unprotect <path>", run: cmdGuardrails},
		{name: "enforce", summary: "enforce enable|disable|rules", run: cmdEnforce},
		{name: "sentinel", summary: "sentinel enable|disable|status", run: cmdSentinel},
		{name: "party", summary: "party profiles | show <name> | use <name>", run: cmdParty},
		{name: "spank", summary: "spank explain — keep long jobs running while you're away", run: cmdSpank},
		{name: "tune", summary: "Show AdaL's model and reasoning-effort settings (read-only)", run: cmdTune},
		{name: "budget", summary: "Token usage and cost report (only real data)", run: cmdBudget},
		{name: "guard", summary: "guard status — budget warning state", run: cmdGuard},
		{name: "preflight", summary: "Check git state before pushing (read-only)", run: cmdPreflight},
		{name: "attest", summary: "AI-attribution trailer policy (opt-in) [install-hook|remove-hook]", run: cmdAttest},
		{name: "check", summary: "Dry-run the safety rules: check -- <shell command>", run: cmdCheck},
		{name: "log", summary: "Show recent AdaLmao decisions", run: cmdLog},
		{name: "config", summary: "config [path|show|validate|set <key> <value>]", run: cmdConfig},
		{name: "loop", summary: "Scored experiment loops (PLANNED — not implemented)", run: cmdPlanned("loop"), status: "planned"},
		{name: "update", summary: "Self-update (PLANNED — not implemented)", run: cmdPlanned("update"), status: "planned"},
		{name: "version", summary: "Print version", run: cmdVersion},
		{name: "hook", summary: "internal: AdaL hook entry point", run: cmdHook, hidden: true},
	}
}

// Run executes the CLI and returns the process exit code.
func Run(args []string) int {
	if len(args) == 0 {
		printHelp()
		return 0
	}
	switch args[0] {
	case "-h", "--help", "help":
		printHelp()
		return 0
	case "-v", "--version":
		return cmdVersion(nil)
	}
	for _, c := range commands() {
		if c.name == args[0] {
			rest := args[1:]
			if len(rest) > 0 && (rest[0] == "-h" || rest[0] == "--help") && c.name != "hook" {
				fmt.Fprintf(stdout, "adalmao %s — %s\n", c.name, c.summary)
				return 0
			}
			return c.run(rest)
		}
	}
	fmt.Fprintf(stderr, "adalmao: unknown command %q\nRun `adalmao --help` for the command list.\n", args[0])
	return 2
}

func printHelp() {
	fmt.Fprintln(stdout, "😂 AdaLmao — Your AdaL CLI's chaotic little bodyguard.")
	fmt.Fprintf(stdout, "version %s\n\nUsage: adalmao <command> [args]\n\nCommands:\n", Version)
	for _, c := range commands() {
		if c.hidden {
			continue
		}
		fmt.Fprintf(stdout, "  %-11s %s\n", c.name, c.summary)
	}
	fmt.Fprintln(stdout, "\nAdaLmao is a practical guardrail layer, not a security sandbox: AdaL hooks fail open on timeout or launch failure.")
	fmt.Fprintln(stdout, "Docs: README.md  |  Data dir: ~/.adalmao  |  Override with ADALMAO_HOME / ADAL_HOME")
}

func cmdVersion(_ []string) int {
	fmt.Fprintf(stdout, "adalmao v%s\n", Version)
	return 0
}

func cmdPlanned(name string) func([]string) int {
	return func(_ []string) int {
		switch name {
		case "loop":
			fmt.Fprintln(stderr, "`adalmao loop` is PLANNED and not implemented yet.")
			fmt.Fprintln(stderr, "AdaL does expose headless runs (`adal -q \"...\" -o json`, `--permission-mode`, `--thinking-effort`), which a")
			fmt.Fprintln(stderr, "future loop runner (explicit start, iteration + budget limits, results ledger) can build on.")
		default:
			fmt.Fprintln(stderr, "`adalmao update` (self-update) is PLANNED and not implemented yet.")
			fmt.Fprintln(stderr, "To update: rebuild from source (`make build`) or download a release binary, then re-run `adalmao install`.")
		}
		return 1
	}
}

func errf(format string, a ...any) int {
	fmt.Fprintf(stderr, "adalmao: "+format+"\n", a...)
	return 1
}

func join(parts []string) string { return strings.Join(parts, ", ") }
