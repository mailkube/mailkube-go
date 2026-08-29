# Project Rules

`mailkube-go` is a public (Apache-2.0) mailkube SDK distributed as a
Go module (`github.com/mailkube/mailkube-go`). Load the relevant rule file from `.rules/` based on the task.

## Rule Index

> **Index every rule (required).** Every file in `.rules/` MUST have a row in the table below. When you
> add or rename a `.rules/` file, add or update its row in the **same change** — an unindexed rule is
> invisible, because this index is what drives progressive disclosure. The `docs` CI job (`scripts/check-rule-index.sh`)
> fails the build if `.rules/` and this index drift. This convention holds for every mailkube repo.

| Rule File | Load When |
|---|---|
| `.rules/SOLID_DRY_KISS.md` | Writing or changing any code — the enforced engineering standards (SOLID, DRY, KISS, coverage, docs) and how to run each gate locally. |
| `.rules/SDK_CONTRACT.md` | Adding a resource, verb, response model, paginated listing, or webhook event: the cross-SDK decisions (config, layering, naming, errors, pagination, webhooks) every mailkube SDK implements identically. Shared verbatim across every SDK; changes are made centrally. |
| `.rules/SDK_DESIGN.md` | The same tasks, for the **Go realization**: the layer-to-file map, the `*http.Client` injection seam, and the deliberate deviations (sentinel errors, functional options, one sync client, `iter.Seq2` pagination, the event registry, `HeaderGetter`). |
| `.rules/RELEASE.md` | Touching `release.yml`, `.releaserc.json`, versioning, or the module's public tags. |

## Key Conventions (always apply)

- **Standard module layout** — the package lives at the module root (`mailkube`); a CLI, if any, goes under `./cmd/…`.
- **golangci-lint (v2)** for lint; **gofumpt** for formatting; **staticcheck** (via the standard set) is non-negotiable.
- **Doc comment** every exported identifier (`revive` `exported`) and the package (`package-comments`).
- **≥ 90% coverage** (line/statement — Go has no branch metric) — enforced by `scripts/check-coverage.sh`; never lower the gate to make a change pass.
- **Max cyclomatic 10 / cognitive 20** (`gocyclo`/`gocognit`) — split, don't waive.
- **Depend on interfaces at boundaries** (DIP); keep interfaces small (ISP) — unused params (`unparam`) are a smell.
- **No duplication** — the `jscpd` gate blocks at > 1% duplicated code; extract shared logic.
- **Conventional Commits** for PR titles (squash-merged); only `feat:`/`fix:`/`perf:` cut a release.
- **No secrets in the repo** — local config lives in a git-ignored `.env`, excluded from the built image.
- **Releases are git tags** — `pkg.go.dev` indexes `vX.Y.Z` automatically; there is no registry publish step.
- **Keep the `README` current** with user-visible changes: it is the only reference this module publishes.
- **Releases commit nothing to `main`** — the git tag is the version, and the GitHub Release
  notes are the changelog; there is no `CHANGELOG.md` (see `.rules/RELEASE.md`).
