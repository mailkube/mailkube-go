# Contributing to mailkube-go

Thanks for helping improve **mailkube-go**, a [mailkube](https://mailkube.com) SDK.
Contributions of all kinds are welcome: bug reports, fixes, docs, and features.

By contributing you agree that your contributions are licensed under the project's
[Apache License 2.0](LICENSE) (inbound = outbound). **No CLA and no sign-off are required.**
Please also read our [Code of Conduct](CODE_OF_CONDUCT.md).

## Development setup

Requires the [Go toolchain](https://go.dev/dl/) (1.23+),
[golangci-lint](https://golangci-lint.run/) (v2), [gofumpt](https://github.com/mvdan/gofumpt),
and Node.js (for the `jscpd` duplication check).

```bash
git clone https://github.com/mailkube/mailkube-go
cd mailkube-go

go mod download
pre-commit install                            # gofumpt + golangci-lint + jscpd hooks
pre-commit install --hook-type commit-msg     # Conventional Commits hook
```

## Quality gates

Every change must pass the same checks CI runs (see [.rules/SOLID_DRY_KISS.md](.rules/SOLID_DRY_KISS.md)):

```bash
golangci-lint run                                    # complexity (KISS) + docs (revive) + SOLID smells
gofumpt -l .                                          # formatting — must print nothing
go vet ./...                                          # vet
go test ./... -race -covermode=atomic -coverprofile=coverage.out
./scripts/check-coverage.sh coverage.out             # 90% coverage gate
npx --yes jscpd@4 --config .jscpd.json .             # duplication (DRY) gate, blocks at > 1%
npx --yes jscpd@4 --config .jscpd.examples.json examples/  # the same gate over examples/
for f in examples/*.go; do go build -o /dev/null "$f" || exit 1; done  # examples compile
for f in examples/*.go; do golangci-lint run "$f" || exit 1; done      # and are linted
./scripts/check-rule-index.sh                        # every .rules/*.md indexed in AGENTS.md
```

`pre-commit run --all-files` runs the format/lint/jscpd hooks in one shot.

**`examples/` is compiled AND linted.** It is runnable documentation, which is the reason, not
an exception to it: customers copy those files, and every defect the SDK certification run
surfaced lived there because no gate looked at it. The `//go:build ignore` tag keeps examples out
of the module (so uncovered example statements cannot drag the 90% coverage gate down), but
naming a file explicitly makes the toolchain ignore its build constraints — which is how both
loops above reach them. Never add `--build-tags ignore` to make that work: `ignore` is a live
build tag inside the Go standard library, and setting it corrupts stdlib type-checking.

Duplication is measured by a *separate* pass, `.jscpd.examples.json`, at `minTokens: 100` instead
of 50. Every example repeats the same scaffolding — import, read `MAILKUBE_FROM`, construct the
client, check the error — and it cannot be hoisted into a shared helper anyway, because each
example is its own `package main` compiled on its own. 100 clears that scaffolding (measured: the
cliff is at 90) and still fails on a copy-pasted example. Coverage excludes examples, because
nothing in CI executes them: they need live credentials.

## Commit & PR conventions

This project follows **[Conventional Commits](https://www.conventionalcommits.org/)**. A CI check
enforces the **PR title** (PRs are **squash-merged** using it), and it drives releases: only
`feat:`, `fix:`, and `perf:` cut a new version. See [.rules/RELEASE.md](.rules/RELEASE.md).

Suggested scopes: `client`, `models`, `ci`, `deps`, `docs`.

```
feat(client): add retry with exponential backoff
fix(models): correct optional field serialization
docs: document the pagination helper
```

## Reporting bugs / requesting features

Open an issue using the templates. For **security vulnerabilities**, do not open a public
issue — follow [SECURITY.md](SECURITY.md) instead.
