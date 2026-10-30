# Changelog

## 0.1.1 — 2026-10-31

Release-readiness pass. Verified against a real AdaL 1.11.5 on Windows 10.

### Fixed
- **Hooks failed to launch when the binary path contained spaces** (found by a real AdaL run: the quoted command was silently dropped, so nothing was blocked). Install now uses the 8.3 short path and refuses, with instructions, if the path still needs quoting.
- Review/risky warnings were written to `PreToolUse` stdout, which AdaL documents as unable to add context. They are now queued and delivered with the next `PostToolUse` result.
- `git restore --staged` no longer flagged as discarding changes.
- Budget parsed any `*_tokens` field; it now reads AdaL's real `usage` transcript records (input, output, cached read/write, reasoning, per model) and reports cached tokens separately.

### Added
- `adalmao verify` (offline launch test via `cmd.exe /c` / `sh -c`) and `verify --live` (real headless AdaL turn in a throwaway repo).
- `adalmao prepush install|remove|status`: optional, opt-in git pre-push preflight; never overwrites foreign hooks.
- `adalmao loop`: bounded, explicit-start experiment loop in an isolated git worktree with a JSONL ledger.
- `adalmao update`: GitHub Releases updater with mandatory SHA-256 verification (no release exists yet).
- `sudo`/`doas` flagged as risky; more enforce coverage and tests (`+refspec` pushes, redirects, `sh -c`, `git -C`, `sed -i`).
- `ADALMAO_CAPTURE=1` raw payload capture for debugging.
- `budget.cached_input_price_per_mtok`.

### Changed
- Release workflow publishes a `checksums.txt`.
- v0.1.0 binary preserved locally as `dist/adalmao-0.1.0.exe`.

## 0.1.0

Initial MVP: guardrails, enforce, sentinel, modes, preflight, installer, status/tune/spank/attest/party/config/log.
