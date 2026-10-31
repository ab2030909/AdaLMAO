> ⚠️ **Early Access / Experimental release.** Not a stable 1.0. Expect rough edges; protection is best-effort, not a security sandbox.

# AdaLmao v0.1.1 — Early Access

**Your AdaL CLI's chaotic little bodyguard.** AdaLmao is a single standalone Go binary that plugs into AdaL CLI's lifecycle hooks to block destructive commands, protect tests and secrets, watch for regressions, inject focus modes, track token usage and check Git before you push. No external AI API, no database, standard library only.

## What's included

| File | Platform |
|---|---|
| `adalmao-windows-amd64.exe` | Windows x64 |
| `adalmao-linux-amd64` | Linux x64 |
| `adalmao-linux-arm64` | Linux ARM64 |
| `adalmao-darwin-amd64` | macOS Intel |
| `adalmao-darwin-arm64` | macOS Apple Silicon |
| `checksums.txt` | SHA-256 for every binary |

Built with Go 1.27, `-trimpath`, `CGO_ENABLED=0`. Binaries are not code-signed.

## Key features

- **Enforce**: deterministic, parser-based rules that block `git reset --hard`, force pushes, `git clean -f`, `git commit -s`, `rm -rf /`, deleting test files / `.env` / `.git`, `curl | sh`, and more, plus your own rules.
- **Guardrails**: allowed / review / risky / blocked verdicts for file edits and deletes.
- **Sentinel**: flags deleted tests, fewer assertions and coverage drops (when a report exists).
- **Modes**: 8 composable prompt modes (focus, paranoid, debug, teacher, …).
- **Budget / Guard**: real token counts from AdaL's transcript; cost only as an estimate from your own prices.
- **Preflight + optional pre-push hook**: read-only Git checks with confirmed-conflict detection.
- **Loop**: bounded experiment loop in an isolated git worktree (iteration + time caps).
- **Verify**: `adalmao verify` / `verify --live` to prove the hooks work on *your* machine.
- **Update**: checksum-verified updater (`adalmao update --apply`).

## Install

1. Download the binary for your platform and `checksums.txt`; verify the SHA-256.
2. Rename to `adalmao` (`adalmao.exe` on Windows) and put it in a permanent folder. **Windows: use a path without spaces, e.g. `C:\adalmao\`.**
3. `adalmao install`, then **restart AdaL**.
4. `adalmao verify` (and optionally `adalmao verify --live`).

`adalmao install` backs up `~/.adal/settings.json`, adds only its own hooks and leaves everything else untouched. `adalmao uninstall` removes only its own hooks.

## Platform compatibility

- **Windows**: tested with real AdaL CLI 1.11.5: hooks run, dangerous commands (`git reset --hard`, `git commit -s`, deleting a test file, editing `.env`) were blocked before execution; install/uninstall verified.
- **Linux**: cross-compiled; **runtime testing pending**.
- **macOS**: cross-compiled; **runtime testing pending**.

## Known limitations

- AdaL hooks **fail open**: a timeout, crash or launch failure lets the action through.
- Shell parsing is a practical approximation. `eval`, encoded payloads, scripts that delete things internally and anything run outside AdaL are not covered.
- AdaL publishes no tool list; unrecognised tools (including MCP tools) are not inspected.
- Hooks load only from `~/.adal/settings.json` (no project scope) and AdaL must be restarted after install.
- No status line / HUD, no per-subagent hooks, no token-budget API; Guard can warn but cannot stop spending.
- `adalmao update` has nothing to compare against until a release exists (this is the first).

## Security notes

Treat AdaLmao as a seatbelt, not a vault: keep backups and commit often. Do not rely on it as your only safeguard for destructive actions.

## Feedback

Bug reports, especially Linux/macOS runtime results: https://github.com/ab2030909/AdaLMAO/issues. Docs: https://github.com/ab2030909/AdaLMAO#readme
