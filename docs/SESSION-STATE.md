# Session State

**Last Updated:** 2026-06-08

## Current Status
Two feature branches complete locally, both green (gofmt/vet/golangci-lint, `go test ./... -race`, 100% coverage gate, build). **Neither is pushed; no PRs opened.**

## Active Branches
| Branch | Base | Status |
|--------|------|--------|
| `feature/GH-29-init-tui-wizard` | develop | Init TUI redesign — single-form + adaptive live preview; `wizard.go` is a thin adapter, logic in `internal/initwizard` at 100%. Complete, unpushed. |
| `feature/GH-35-audit-summary-reports` | develop | Audit reporting overhaul (current branch, 12 commits ahead). Complete, unpushed. Tree clean. |

GH-29 and GH-35 are independent (touch different code); both branch from `develop`.

## What Was Done (GH-35, this work)
- `--summary` (counts-only) and `--group-by license|category|type|directory` across text/markdown/JSON; default output preserved.
- Attributable policy violations (which license/rule/file) in all formats — `policy.Violation` data was being discarded at `report.Build`; now surfaced via `model.ViolationDetail` / JSON `violationDetails`.
- `findings` + per-row percentages/totals + `riskLevel`/`worstCategory` parity in JSON & markdown (was text-only).
- Per-group risk markers + license breakdown (fixes directory-license-blindness); `(no-header)` rename; `--sort key|count`; `--depth N`; `--only missing,unknown,copyleft,violations`.
- JSON now always emits the full report (`--summary` trims human formats only).
- Two clean-room audits run: `docs/review/04-...` (pre) and `05-...` (post). 2nd audit: 6 addressed / 3 partial / 2 open, then 4 more fixed this turn.

## Pending Work (GH-35 punch list — from `docs/review/05-...md`)
- [ ] `--group-by dependency` (deps can't be grouped; distinct code path — deps aren't files).
- [ ] Policy-aware group risk (a group's `[risk]` ignores repo-level incompatibilities, e.g. Apache group beside AGPL reads `low`).
- [ ] Exclude the tool's own `.license-tool.yaml` from source/header expectations (inflates `sourceTotal`/`sourceMissing`). Cheap.
- [ ] JSON explicit percentages/totals (low value; derivable from counts + `findings`).
- [ ] Dep resolver returned 0 for a `go.mod` require (likely module-cache dependent) — needs CI-verified repro.

## Other Open Threads
- [ ] Push branches + open PRs into `develop` (awaiting permission) — GH-29 and GH-35.
- [ ] GH-29 commit `bafa281` bundled planning docs (CLAUDE.md/DESIGN/PLAN/review) via `git add -A` — optional history tidy before PR.
- [ ] Independent review bugs filed but untouched: #30 (detect over-match), #31 (spdxnorm guessing), #33 (symlink LICENSE), #34 (no-op --quiet/--verbose). #32 (include `**`) was fixed on GH-29.

## Key Reference
- Issues: #29 (init TUI), #35 (audit reports); bugs #30/#31/#33/#34.
- Design/spec: `docs/DESIGN-init-tui-redesign.md`, `docs/DESIGN-GH-35-audit-summary-reports.md`, `docs/PLAN-GH-29-init-tui-redesign.md`.
- Reviews: `docs/review/01-03` (full codebase), `04`/`05` (audit clean-room).
- Gate: `gofmt -l .`; `go vet ./...`; `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run`; `go test ./... -race -coverpkg=./internal/...,./cmd/... -covermode=atomic -coverprofile=cover.out`; `go run github.com/vladopajic/go-test-coverage/v2@v2.18.8 --config=.testcoverage.yml`.
- Repo: GitHub `KofTwentyTwo/license-tool`; default branch `develop`; commits must reference issues, no AI attribution.
