# 😂 AdaLmao

**Your AdaL CLI's chaotic little bodyguard.**

AdaLmao is an open-source companion toolkit for [AdaL CLI](https://docs.sylph.ai). It adds configurable guardrails, deterministic tool-call enforcement, regression monitoring, composable prompt modes, usage tracking and Git preflight checks. It is a single standalone Go binary with no external AI API and no database.

> AdaLmao is a **practical guardrail layer, not a security sandbox.** AdaL hooks fail open: if a hook times out or fails to launch, the action goes ahead. Treat AdaLmao as a seatbelt, not a vault.

```text
🚨 NICE TRY!

AdaLmao blocked a dangerous operation. It was NOT executed.

Rule:   no-hard-reset
Reason: `git reset --hard` irreversibly discards uncommitted work.
Safer alternative: Use `git stash` or `git reset --soft`/`--mixed`, or ask the user to run it.
```

## What problem does it solve?

Coding agents occasionally run `git reset --hard`, delete a failing test instead of fixing it, edit `.env`, or add a DCO sign-off on your behalf. AdaLmao sits in AdaL's lifecycle hooks and mechanically stops those, explains why, and logs the decision locally.

## Features

| Buff | Status | What it does |
|---|---|---|
| Guardrails | ✅ | Classifies file operations as safe / review / risky / blocked. Protects `.git`, env files, tests, config files, AdaL's own settings and your custom paths. Warns on public-API signature, removal and default-value changes in edits. |
| Enforce | ✅ | Deterministic rule engine for shell commands. Normalizes quotes, separators (`; && \|\|`), `sudo`/`env` wrappers, `bash -c`, `cmd /c`, `powershell -Command`, `$(...)` and git global options before matching. Custom rules supported. |
| Sentinel | ✅ | After tool calls, checks `git status`/`git diff` for deleted tests, reduced assertions, deleted important files, protected-path changes and coverage drops (only when a coverage report exists). |
| Modes | ✅ | 8 composable modes (max 6 active), injected via the `UserPromptSubmit` hook. |
| Party | 🟡 | Named mode profiles you apply manually. No automatic per-agent orchestration (see limitations). |
| Spank | ✅ (guidance) | Platform-specific advice and read-only checks for long-running work. Changes nothing. |
| Tune | ✅ (read-only) | Shows your real AdaL model and reasoning-effort settings from AdaL's files. |
| Budget / Guard | 🟡 | Reports token usage only if AdaL's session transcript contains it, otherwise "Usage data unavailable". Cost is an estimate from prices *you* configure. Warns once per threshold; cannot stop spending. |
| Status | ✅ | `adalmao status` (AdaL has no documented status-line hook, so there is no live HUD). |
| Preflight | ✅ | Read-only Git checks: behind/ahead, uncommitted work, in-progress merge/rebase, confirmed conflicts via `git merge-tree`, already-incorporated commits. |
| Attest | ✅ (opt-in) | Prints the attribution policy; optionally installs a `prepare-commit-msg` hook that appends `Assisted-by: AdaL`. Never adds `Signed-off-by`. |
| Loop | ⏳ planned | Not implemented. |
| Update | ⏳ planned | Not implemented. Rebuild or download a release and re-run `adalmao install`. |

## Installation

Requires Go 1.22+ and AdaL CLI (`adal`).

```bash
git clone <this repo> && cd adalmao
make build            # or on Windows PowerShell: ./scripts/build.ps1
# put dist/adalmao (or adalmao.exe) somewhere permanent and on your PATH
adalmao install       # registers hooks in ~/.adal/settings.json
```

Then **restart AdaL** so it re-reads its hooks.

`adalmao install`:
- backs up `~/.adal/settings.json` before changing it (`settings.json.adalmao-<timestamp>.bak`),
- adds only AdaLmao-owned hooks and preserves every other setting and hook,
- is idempotent (running it twice makes no second change),
- refuses to touch a settings file that is not valid JSON.

`adalmao uninstall` removes only AdaLmao-owned hooks and keeps your data in `~/.adalmao` unless you pass `--purge`.

Hooks call the absolute path of the binary you installed from. If you move the binary, re-run `adalmao install`. Put it in a path **without spaces** if you can: on Windows the hook command is double-quoted when the path has spaces, and AdaL's hook shell is not documented, so quoting is the one fragile bit.

## Quick start

```bash
adalmao install
adalmao mode on focus paranoid
adalmao status
adalmao check -- git reset --hard HEAD~3     # dry-run the rules, no AdaL needed
```

## Commands

```text
adalmao install | uninstall [--purge] | status | version | --help
adalmao modes | mode on <names...> | mode off [names...]
adalmao guardrails enable|disable|status|protect <path>|unprotect <path>
adalmao enforce enable|disable|rules
adalmao sentinel enable|disable|status
adalmao party profiles | show <name> | use <name>
adalmao spank explain
adalmao tune | budget | guard status
adalmao preflight
adalmao attest [install-hook|remove-hook]
adalmao check -- <shell command> | check --delete|--write <path>
adalmao log
adalmao config [path|show|validate|set <key> <value>]
adalmao loop | update          # planned, print a notice and exit 1
```

## Modes

`focus`, `paranoid`, `perf`, `debug`, `tech-cofounder`, `ship-it`, `terse`, `teacher`. Up to six at once. Each mode is a few lines of context, stored in `templates/modes/*.md`.

Precedence when active modes conflict: `paranoid > debug > focus > ship-it > perf > tech-cofounder > terse > teacher`. Known conflicts (`terse`/`teacher`, `ship-it`/`paranoid`, `focus`/`tech-cofounder`) are reported when you enable them, and the precedence line is included in the injected context.

Modes are global (AdaL hooks are only configurable at user level in `~/.adal/settings.json`, so project-specific mode preferences are not available).

## Guardrails and enforcement

Built-in enforce rules (`adalmao enforce rules`):

| Rule | Blocks |
|---|---|
| `no-hard-reset` | `git reset --hard` in any form |
| `protect-paths` | deleting/moving/overwriting `.git`, env files, test files, critical config, your protected paths, AdaL/AdaLmao config |
| `no-dco-signoff` | `git commit -s`, `--signoff`, `-sm`, `--trailer Signed-off-by` (unless `enforce.allow_dco_signoff`) |
| `git-clean-force`, `no-force-push`, `no-discard-all`, `git-history-rewrite` | destructive Git operations |
| `catastrophic-delete` | `rm -rf /`, `~`, `.`, `..`, drive roots, project root |
| `pipe-to-shell` | `curl ... \| sh` |
| `disk-destroy` | `mkfs`, `dd` to devices, `format X:`, fork bombs |

Editing a test file is **not** blocked, it just adds a review note. Deleting one is blocked. A blocked call never reaches the underlying tool (exit code 2 from a `PreToolUse` hook), and the reason and a safer alternative are returned to the agent. Decisions are logged (secrets redacted) to `~/.adalmao/decisions.log`; view with `adalmao log`.

Custom rule:

```json
{
  "enforce": {
    "rules": [
      { "id": "no-npm-publish", "enabled": true, "pattern": "npm publish", "action": "block",
        "reason": "Publishing requires manual review." }
    ]
  }
}
```

`pattern` matches the start of a normalized command segment (so `echo "git reset --hard"` is not a false positive). Set `"regex": true` for a regex. `action` is `block` or `warn`. Turn off a built-in with `"disabled_builtins": ["git-clean-force"]`.

Always verify with `adalmao check -- <command>`.

## Configuration

`~/.adalmao/config.json` (override the directory with `ADALMAO_HOME`). Missing file = defaults; invalid file = defaults plus a warning; unknown fields are preserved on save.

```json
{
  "version": 1,
  "general":    { "enabled": true, "active_modes": ["focus"] },
  "guardrails": { "enabled": true, "protect_env_files": true, "protect_git_directory": true,
                  "protect_tests": true, "protect_config_files": true,
                  "warn_on_public_api_changes": true, "warn_on_default_value_changes": true,
                  "protected_paths": ["migrations/", "secrets.yml"] },
  "enforce":    { "enabled": true, "allow_dco_signoff": false, "disabled_builtins": [], "rules": [] },
  "sentinel":   { "enabled": true, "severity": "warning" },
  "budget":     { "enabled": true, "session_limit": 5.0, "warning_threshold": 0.8,
                  "input_price_per_mtok": 0, "output_price_per_mtok": 0 },
  "git":        { "preflight_enabled": true, "base_branch": "" },
  "attest":     { "enabled": false, "trailer": "Assisted-by: AdaL" },
  "profiles":   { "builder": ["ship-it"], "reviewer": ["paranoid"], "optimizer": ["perf"], "mentor": ["teacher"] }
}
```

Edit with `adalmao config set budget.session_limit 3` (validated before saving) or by hand.

## Git preflight

```bash
adalmao preflight
```

Reports branch, base branch, uncommitted changes, in-progress merge/rebase, ahead/behind versus upstream and base, and local commits already present in the base. Conflicts are labelled honestly: **confirmed** (from `git merge-tree --write-tree`, git ≥ 2.38) or only **potential** (files changed on both sides, used as a fallback). It never fetches, rebases, merges or pushes, and never touches your working tree. Remote data is as of your last `git fetch`. A git pre-push hook is not implemented yet.

## Usage and budget tracking

`adalmao budget` shows token counts only if AdaL's session transcript (the `transcript_path` passed to hooks) contains `input_tokens`/`output_tokens`-style fields; otherwise it prints `Usage data unavailable`. Costs appear only if you set `budget.input_price_per_mtok` / `output_price_per_mtok`, and are labelled estimates. Guard warns once at the threshold and once at the limit through `UserPromptSubmit` context. It cannot stop spending: AdaL gives hooks no mechanism for that. As of v0.1.0 the transcript format is unverified, so treat this feature as best-effort.

## Platform support

Written for Windows, Linux and macOS (pure Go, standard library only). Developed and smoke-tested on Windows 10. Linux/macOS are expected to work but are not yet tested.

## Current limitations

- Hooks fail open on timeout or launch failure; protection is best-effort. `status` and this README say so on purpose.
- Hooks cannot modify tool input, only allow, block or add context.
- AdaL does not publish a list of tool names or `tool_input` fields. AdaLmao classifies by tool-name keywords (`bash`, `delete`, `edit`, `write`, ...) plus common field names (`command`, `file_path`, `path`, `old_string`/`new_string`). Unrecognised tools are not inspected.
- Shell parsing is a practical approximation, not a full shell. Obfuscated or indirect commands (`eval`, encoded payloads, scripts that run `rm` internally) can slip through.
- Sentinel's PostToolUse checks can warn and instruct the agent but cannot undo a change.
- No live status line or HUD, no per-agent party orchestration, no `loop`, no self-update.
- Project-level hooks and modes are not possible; AdaL only supports user-level hooks today.
- Coverage checks run only when a coverage report file (`coverage.out`, `lcov.info`, `coverage-summary.json`, `coverage.xml`) already exists.

## Roadmap

- `loop`: explicit, bounded, scored experiment loops on top of `adal -q` headless runs
- Git pre-push integration (with verified install/removal)
- Self-update
- More modes (security review, SRE, frontend, backend, database, migration)
- Verified Linux/macOS CI

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Quick loop: `make test && make build`.

## License

MIT, see [LICENSE](LICENSE).
