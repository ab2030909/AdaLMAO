# Contributing to AdaLmao 😂

Thanks for helping. Ground rules:

1. **Never invent AdaL capabilities.** Verify hook schemas and settings against the installed AdaL and https://docs.sylph.ai/customize/lifecycle-hooks/. If AdaL cannot do something, document the limitation.
2. **No fake success.** A command that is not implemented must say so and exit non-zero.
3. **Safety changes need tests.** Anything in `internal/enforce`, `internal/guardrails` or `internal/installer` should come with a case in the existing table tests.
4. Keep dependencies at zero (standard library only) unless there is a strong reason.
5. Hooks must stay fast and must never touch the network.

## Dev loop

```bash
make test      # go test ./...
make build     # dist/adalmao
make cross     # linux/darwin/windows binaries in dist/
```

On Windows without `make`: `./scripts/build.ps1`.

Use an isolated environment when testing install/uninstall so you do not touch your real AdaL config:

```bash
export ADAL_HOME=$(mktemp -d) ADALMAO_HOME=$(mktemp -d)
```

## Layout

`cmd/adalmao` entry point, `internal/cli` commands, `internal/hooks` AdaL hook protocol, `internal/enforce` command parsing and rules, `internal/guardrails` file-operation policy, `internal/sentinel` regression checks, `internal/installer` settings.json handling, `templates/modes` mode text.
