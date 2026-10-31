#  AdaLmao 
AdaL Model Action Oversight

**Your AdaL CLI's chaotic little bodyguard.**

AdaLmao is an open-source companion toolkit for [AdaL CLI](https://docs.sylph.ai). It adds configurable guardrails, deterministic tool-call enforcement, regression monitoring, composable prompt modes, usage tracking, a bounded experiment loop and Git preflight checks. It is a single standalone Go binary (standard library only), with no external AI API and no database.

> AdaLmao is a **practical guardrail layer, not a security sandbox.** AdaL hooks fail open: if a hook times out or fails to launch, the action goes ahead. Treat it as a seatbelt, not a vault.

```text
🚨 NICE TRY!

AdaLmao blocked a dangerous operation. It was NOT executed.

Rule:   no-hard-reset
Reason: `git reset --hard` irreversibly discards uncommitted work.
Safer alternative: Use `git stash` or `git reset --soft`/`--mixed`, or ask the user to run it.
```

## Release status: Early Access / Experimental

**v0.1.1 is an early-access release, not a stable 1.0.** Expect rough edges and breaking changes before 1.0.

| Platform | Status |
|---|---|
| Windows (amd64) | Tested with real AdaL CLI 1.11.5 (hook execution, command blocking, install/uninstall). |
| Linux (amd64, arm64) | Cross-compiled only. **Runtime testing pending.** |
| macOS (Intel, Apple Silicon) | Cross-compiled only. **Runtime testing pending.** |

Go tests and `go vet` pass. Protection is best-effort, not complete: see [Integration limitations](#integration-limitations) and [Security limitations](#security-limitations).

## Download

Binaries and `checksums.txt` are attached to the [GitHub release](https://github.com/ab2030909/AdaLMAO/releases/tag/v0.1.1) (`adalmao-windows-amd64.exe`, `adalmao-linux-amd64`, `adalmao-linux-arm64`, `adalmao-darwin-amd64`, `adalmao-darwin-arm64`). Verify before running:

```powershell
# Windows PowerShell: compare against the matching line in checksums.txt
(Get-FileHash .\adalmao-windows-amd64.exe -Algorithm SHA256).Hash.ToLower()
```
```bash
# Linux / macOS
sha256sum -c --ignore-missing checksums.txt     # macOS: shasum -a 256 -c --ignore-missing checksums.txt
chmod +x adalmao-linux-amd64 && ./adalmao-linux-amd64 version
```

Then rename it to `adalmao` / `adalmao.exe`, put it somewhere permanent (Windows: a path without spaces, e.g. `C:\adalmao\`) and run `adalmao install`. The binaries are not code-signed; on macOS you may need to clear the quarantine flag (`xattr -d com.apple.quarantine adalmao`) and Windows SmartScreen may warn.

## Status legend

✅ implemented and verified · 🟡 partially implemented · ⏳ planned · 🚫 unsupported by AdaL

| Buff | Status | Notes |
|---|---|---|
| Hook integration | ✅ | Verified against real AdaL 1.11.5 on Windows: AdaL calls the hooks, payloads parse, blocks reach the agent. See [Verification](#verification). |
| Guardrails | ✅ | Safe / review / risky / blocked verdicts for file tools. |
| Enforce | ✅ | Deterministic command rules (built-in + yours). |
| Sentinel | ✅ | Post-tool Git checks; coverage only when a report file exists. |
| Modes | ✅ | 8 modes, max 6 active. Global, not per project (🚫 AdaL has no project-level hooks). |
| Budget | ✅ | Real token counts from AdaL's transcript. Cost is an estimate from prices you provide. |
| Guard | 🟡 | Warns once per threshold. 🚫 Cannot stop spending: AdaL gives hooks no way to. |
| Preflight | ✅ | Read-only Git inspection. |
| Pre-push hook | ✅ | Optional, opt-in; tested with a real conflicting push. |
| Tune | ✅ | Read-only view of your model + effort settings. |
| Verify | ✅ | `adalmao verify` / `verify --live`. |
| Loop | 🟡 | Bounded, isolated-worktree loop over `adal -q`. Time + iteration limits only (🚫 no token budget API). |
| Update | 🟡 | Checksum-verified updater; **no release is published yet**, so it only reports that. |
| Party | 🟡 | Manual mode profiles. 🚫 No per-subagent hook scope. |
| Spank | ✅ | Guidance + read-only checks. Changes nothing. |
| Attest | ✅ | Opt-in trailer; never signs or adds `Signed-off-by`. |
| Statusline / HUD | 🚫 | AdaL documents no status-line hook. Use `adalmao status`. |

## Installation

Requires Go 1.22+ (to build) and AdaL CLI.

```powershell
# Windows
git clone https://github.com/ab2030909/AdaLMAO.git ; cd AdaLMAO
./scripts/build.ps1
mkdir C:\adalmao ; copy dist\adalmao.exe C:\adalmao\
C:\adalmao\adalmao.exe install      # then restart AdaL
# add C:\adalmao to PATH to run `adalmao` from anywhere
```

```bash
# Linux / macOS
git clone https://github.com/ab2030909/AdaLMAO.git && cd AdaLMAO
make build && sudo install dist/adalmao /usr/local/bin/   # or any directory on PATH
adalmao install                                            # then restart AdaL
```

`adalmao install` backs up `~/.adal/settings.json`, adds only AdaLmao-owned hooks (`PreToolUse`, `PostToolUse`, `UserPromptSubmit`, `Stop`), keeps every other setting and hook, and does nothing if already installed. It refuses to touch an invalid settings file. `adalmao uninstall` removes only AdaLmao's hooks and keeps `~/.adalmao` unless you pass `--purge`.

**Windows path rule (verified):** AdaL launches hooks with `cmd.exe /c`, and a hook command containing double quotes silently failed to launch in testing. So the install path must not need quoting. AdaLmao converts the path to its 8.3 short form, and refuses with instructions if it still contains spaces or special characters. Install it somewhere simple like `C:\adalmao\`. The hook points at the absolute path, so re-run `adalmao install` if you move the binary.

## Quick start

```bash
adalmao install
adalmao verify                      # launches each hook the way AdaL does, offline
adalmao verify --live               # one real headless AdaL turn in a throwaway repo (uses a little quota)
adalmao mode on focus paranoid
adalmao check -- git reset --hard   # dry-run the rules
adalmao status
```

## Verification

What was actually exercised against a real AdaL 1.11.5 install on Windows 10 (throwaway repos only):

- AdaL runs the registered hooks and sends `session_id`, `cwd`, `hook_event_name`, `transcript_path` plus `tool_name`/`tool_input` (tools seen: `bash` with `command`; `create_file` with `file_path`,`new_string`; `replace_by_string` with `file_path`,`old_string`,`new_string`).
- `bash: git reset --hard`, `git commit -s`, `rm a_test.go` and an edit to `.env` were blocked before execution; the files and commit history were intact afterwards, and the agent relayed the reason.
- Safe operations (create file, edit a normal file, `git status`) ran normally.
- Decisions appear in the AdaLmao log. Malformed hook input fails open.

Not verified: Linux and macOS at runtime; behavior with every AdaL tool (unrecognised tools are not inspected); AdaL desktop (`--ide`) behavior.

## Commands

```text
adalmao install | uninstall [--purge] | status | version | --help
adalmao verify [--live] [--model <key>]
adalmao modes | mode on <names...> | mode off [names...]
adalmao guardrails enable|disable|status|protect <path>|unprotect <path>
adalmao enforce enable|disable|rules
adalmao sentinel enable|disable|status
adalmao party profiles | show <name> | use <name>
adalmao spank explain
adalmao tune | budget | guard status
adalmao preflight
adalmao prepush install|remove|status
adalmao attest [install-hook|remove-hook]
adalmao loop --task "..." --verify "cmd" [--max-iterations N] [--max-minutes N] [--model key] [--permission-mode m] --yes
adalmao update [--apply]
adalmao check -- <shell command> | check --delete|--write <path>
adalmao log
adalmao config [path|show|validate|set <key> <value>]
```

## Guardrails and enforcement

Verdicts: **allowed**, **review** (note to the agent), **risky** (stronger note), **blocked** (exit code 2: the call never reaches the tool, and the reason plus a safer alternative are returned). AdaL documents that `PreToolUse` cannot add context, so review/risky notes are queued and delivered with the next `PostToolUse` result for that session.

Built-in enforce rules (`adalmao enforce rules`):

| Rule | Blocks |
|---|---|
| `no-hard-reset` | `git reset --hard` in any form |
| `protect-paths` | deleting/moving/overwriting/redirecting into `.git`, env files, test files, critical config, your protected paths, AdaL/AdaLmao config |
| `no-dco-signoff` | `git commit -s`, `--signoff`, `-sm`, `--trailer Signed-off-by` (unless `enforce.allow_dco_signoff`) |
| `git-clean-force`, `no-force-push`, `no-discard-all`, `git-history-rewrite` | destructive Git operations (`+ref` pushes included) |
| `catastrophic-delete` | `rm -rf /`, `~`, `.`, `..`, drive roots, home or project root |
| `pipe-to-shell` | `curl ... \| sh` |
| `disk-destroy` | `mkfs`, `dd` to devices, `format X:`, fork bombs |

Risky but allowed: anything run through `sudo`/`doas`, recursive deletes, `git rebase`, `git commit --amend`, `git branch -D`, discarding single files, reading `.env` files (review).

Commands are tokenized with quote/separator awareness and normalized (`sudo`/`env` wrappers, `bash -c`, `cmd /c`, `powershell -Command`, `$(...)`, backticks, redirects, `git -C`/`-c` options) before rules apply. `echo "git reset --hard"` is not a false positive. Editing a test file is allowed with a note; deleting one is blocked.

Custom rule:

```json
{ "enforce": { "rules": [
  { "id": "no-npm-publish", "enabled": true, "pattern": "npm publish", "action": "block",
    "reason": "Publishing requires manual review." } ] } }
```

`pattern` matches the start of a normalized command (`"regex": true` for a regex). `action` is `block` or `warn`. Disable a built-in with `"disabled_builtins": ["git-clean-force"]`. Always test with `adalmao check -- <command>`.

## Sentinel

After mutating tool calls (`bash`, create/edit/delete), Sentinel inspects `git status`/`git diff HEAD` and reports each finding once per session: deleted test files, deleted important files, fewer assertions in test diffs, protected-path changes, and coverage drops **only** when a coverage report (`coverage.out`, `lcov.info`, `coverage-summary.json`, `coverage.xml`) exists. Severity `info|warning|block`; `block` tells the agent to stop, because a `PostToolUse` hook cannot undo anything. It does not claim universal regression detection.

## Modes

`focus`, `paranoid`, `perf`, `debug`, `tech-cofounder`, `ship-it`, `terse`, `teacher` (text in `templates/modes/*.md`), injected through `UserPromptSubmit`. Precedence on conflict: `paranoid > debug > focus > ship-it > perf > tech-cofounder > terse > teacher`; known conflicts are reported when you enable them.

## Git preflight and pre-push

`adalmao preflight` reports branch, base, uncommitted changes, in-progress merge/rebase, ahead/behind vs upstream and base, commits already incorporated, and conflicts labelled **confirmed** (`git merge-tree --write-tree`, Git ≥ 2.38) or only **potential** (fallback heuristic). It never fetches, rebases, merges or pushes.

`adalmao prepush install` adds an optional `.git/hooks/pre-push` (respecting `core.hooksPath`). It refuses to overwrite a hook it doesn't own, fails open if the binary moved, aborts a push only for an in-progress merge/rebase or confirmed conflicts with the base, and is bypassed with `git push --no-verify` or `ADALMAO_SKIP_PREPUSH=1`. `adalmao prepush remove` deletes only its own hook.

## Budget and Guard

AdaL 1.11.5 writes `{"type":"usage", model, input_tokens, output_tokens, cached_read_input_tokens, cache_creation_tokens, reasoning_tokens, source}` records into the transcript it passes to hooks. `adalmao budget` sums them (per model, subagents included). Before any usage is recorded it prints `Usage data unavailable`. AdaL reports no prices, so cost is only an **estimate** from `budget.input_price_per_mtok`, `output_price_per_mtok` and `cached_input_price_per_mtok`; with no prices set there is no cost and Guard stays silent. Guard warns once at the threshold and once at the limit through prompt context. It cannot stop spending.

## Loop

```bash
adalmao loop --task "Make the failing test pass" --verify "go test ./..." --max-iterations 3 --yes
```

Explicit start only (`--yes`; otherwise it prints the plan). It creates a new git worktree and branch from `HEAD`, runs `adal -q` there with `--permission-mode acceptEdits` by default (edits run, shell commands do not), runs your verify command (refused if enforce rules would block it), commits each iteration, writes a JSONL ledger under `~/.adalmao/loops/`, and stops on success or at the caps (hard maximums: 10 iterations, 240 minutes). Score is pass/fail by exit code. It never merges, and your working tree is untouched. Token budgets are not available from AdaL, so only iteration and time limits apply. Your installed AdaLmao hooks still guard the loop's agent.

## Update

`adalmao update` compares against the latest GitHub release; `--apply` downloads the platform asset and `checksums.txt` from GitHub over HTTPS, refuses on a missing or mismatched SHA-256, and swaps the binary atomically, keeping the old one as `adalmao.exe.old`. It never runs downloaded scripts. As of v0.1.1 no release has been published, so there is nothing to download yet.

## Configuration

`~/.adalmao/config.json` (override with `ADALMAO_HOME`; AdaL's directory with `ADAL_HOME`). Missing = defaults, invalid = defaults + warning, unknown fields are preserved on save.

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
                  "input_price_per_mtok": 0, "output_price_per_mtok": 0, "cached_input_price_per_mtok": 0 },
  "git":        { "preflight_enabled": true, "base_branch": "" },
  "attest":     { "enabled": false, "trailer": "Assisted-by: AdaL" },
  "profiles":   { "builder": ["ship-it"], "reviewer": ["paranoid"], "optimizer": ["perf"], "mentor": ["teacher"] }
}
```

Edit with `adalmao config set budget.session_limit 3` (validated before saving) or by hand. Debugging: `ADALMAO_CAPTURE=1` records raw hook payloads to `$ADALMAO_HOME/capture.jsonl` (opt-in; may contain code and prompts).

## Integration limitations

- Hooks fail open on timeout, crash or launch failure; `status` and this README say so on purpose.
- Hooks can allow, block (`PreToolUse`, `UserPromptSubmit`) or add context (`PostToolUse`, `UserPromptSubmit`); they cannot modify tool input. `PostToolUse` cannot undo a change.
- AdaL loads hooks only from `~/.adal/settings.json` (no project or plugin scope) and must be restarted after install.
- AdaL doesn't publish a tool list. Detection is by name keywords (`bash`, `delete`, `edit`, `replace`, `write`, `create`, `read`) plus fields `command`, `file_path`/`path`, `old_string`/`new_string`. Unrecognised tools (including MCP tools) are not inspected.
- No documented status line, no per-subagent hooks, no token budget API.
- AdaL may auto-update itself when invoked.

## Security limitations

Shell parsing is a practical approximation. `eval`, encoded payloads, scripts that delete things internally, tools AdaL doesn't surface to hooks, and anything run outside AdaL are not covered. Don't rely on AdaLmao alone for destructive-action safety; keep backups and commit often. Logs are local (`~/.adalmao/decisions.log`) with likely secrets redacted and long values truncated.

## Platform support

Pure Go. **Windows 10: built, tested, and verified with real AdaL.** Linux and macOS: cross-compiled only, not run. Hook commands on Unix are `'path' hook <event>`-style and run by `/bin/sh -c`, but that path is untested against real AdaL.

## Roadmap

- Linux/macOS runtime verification
- Token-aware loop budgets if AdaL exposes usage to wrappers
- More modes (security review, SRE, frontend, backend, database, migration)
- First published release (so `update` has something to fetch)

## Reporting bugs

Open an issue at https://github.com/ab2030909/AdaLMAO/issues with: your OS, `adalmao version`, AdaL version, the output of `adalmao verify`, and what you expected vs. what happened. Linux/macOS runtime reports are especially welcome. **Do not paste secrets**; logs in `~/.adalmao/decisions.log` redact likely secrets, but check before sharing. For security-sensitive issues, please contact the maintainer privately rather than filing a public issue.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Quick loop: `make test && make build` (Windows: `./scripts/build.ps1`).

## License

MIT, see [LICENSE](LICENSE). Changes: [CHANGELOG.md](CHANGELOG.md).
