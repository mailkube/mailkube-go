# Engineering Standards: SOLID · DRY · KISS · Coverage · Docs

These are **enforced by CI** — a PR that violates them cannot merge. This file tells you the exact
thresholds and how to satisfy each gate locally *before* pushing.

## The gates

| Gate | Rule | Enforced by |
|---|---|---|
| **Coverage** | ≥ 90% (line/statement) | `go test ./... -coverprofile` + `scripts/check-coverage.sh` (the `test` CI job) |
| **DRY** | ≤ 1% duplicated code | `jscpd` (the `dry` CI job) — the module at `minTokens: 50`, `examples/` at 100 |
| **Examples** | every `examples/*.go` compiles and lints clean | `go build` + `golangci-lint run`, per file (the `test` CI job) |
| **KISS** | cyclomatic ≤ 10, cognitive ≤ 20 per unit | golangci-lint `gocyclo` / `gocognit` (the `test` CI job) |
| **Documentation** | every exported identifier + the package has a doc comment | golangci-lint `revive` (`exported`, `package-comments`) |
| **SOLID** | see below — approximated by lint + review | golangci-lint `unparam`/`gocritic`/`revive` + PR checklist |
| **Strict analysis** | no `staticcheck`/`govet`/`errcheck` findings | golangci-lint `standard` set (the `test` CI job) |
| **Formatting** | gofumpt-clean | `gofumpt -l .` (the `test` CI job) |

> **Coverage is line/statement only.** Go's tooling has no reliable branch-coverage metric, so there
> is nothing further to gate on. Keep tests exercising every meaningful path anyway — the number is a
> floor, not a target.

## Run the gates locally

```bash
golangci-lint run                                  # complexity (KISS), docs, SOLID smells, staticcheck
gofumpt -l .                                        # formatting — must print nothing
go vet ./...                                        # vet
go test ./... -race -covermode=atomic -coverprofile=coverage.out
./scripts/check-coverage.sh coverage.out            # 90% coverage gate
npx --yes jscpd@4 --config .jscpd.json .            # duplication (DRY) gate
npx --yes jscpd@4 --config .jscpd.examples.json examples/  # the same gate over examples/
for f in examples/*.go; do go build -o /dev/null "$f" || exit 1; done  # examples compile
for f in examples/*.go; do golangci-lint run "$f" || exit 1; done      # and are linted
./scripts/check-rule-index.sh                       # every .rules/*.md indexed in AGENTS.md
```

`pre-commit run --all-files` runs the gofumpt + golangci-lint + jscpd + commitlint hooks in one shot.

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

## SOLID, concretely (paradigm-neutral guidance)

SOLID is not a single lint rule; keep these in mind and confirm them in the PR checklist:

- **S**ingle responsibility — a function/type does one thing; if you need "and" to describe it, split it.
- **O**pen/closed — extend via new functions/types/strategies, not by editing stable call sites.
- **L**iskov — implementations honor their interface's contract (return values, errors, invariants).
- **I**nterface segregation — small, focused interfaces; unused parameters (golangci `unparam`) are a smell.
- **D**ependency inversion — depend on a Go **interface** at I/O and network boundaries, and inject it.

## Requesting a waiver

If a threshold is genuinely wrong for a specific line, add a **scoped, commented** ignore
(e.g. `//nolint:gocyclo // parser dispatch, intentionally flat`) and call it out in the PR. Blanket
config relaxations (lowering the coverage threshold, disabling a linter globally) require maintainer sign-off.
