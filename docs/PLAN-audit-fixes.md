# PLAN: Audit Fixes

## Goal
Resolve GitHub issues #6 through #27 locally, using TDD, while preserving the 100% CI coverage gate and leaving the branch ready for a manual review pass before any push or release.

## Approach
Work on `feature/GH-12-27-audit-fixes` and split fixes into isolated worktrees by concern: header boundaries, file-type coverage, write safety, CLI/reporting, policy/SPDX, dependency/release/docs, and shared header placement. Each slice starts with failing behavior tests, then minimal implementation, then local verification. Results are integrated back into this branch and finally merged to local `develop` only after the full test and CI-equivalent gate passes.

## Files Affected
- `cmd/license-tool/*`: CLI output, exit codes, command behavior tests, wizard filtering.
- `internal/applier/*`: write gates, scoped enumeration, dry-run diffs, scoped commits.
- `internal/report/*`: report rendering for apply/check/audit outputs.
- `internal/config/*`: flag/config layering and policy ID validation.
- `internal/spdx/*`: renderable license contract.
- `internal/resolve/*`: resolver tier validation and dependency manifest discovery.
- `internal/render/*`, `internal/detect/*`, `internal/header/*`: shared header placement behavior and preserve-first constructs.
- `internal/filetype/*`, `internal/enumerate/*`: file-type coverage and shebang-based classification.
- `.github/workflows/*`, `.goreleaser.yaml`: release supply-chain pinning.
- `docs/*`, `SECURITY.md`, `.github/*`: public support and status scaffolding.

## Steps
1. Create worktrees and assign disjoint issue slices.
2. For each slice, write one failing test for one user-visible behavior.
3. Implement the minimal fix and keep the slice green.
4. Integrate slices into the feature branch, resolving conflicts by preserving tested behavior.
5. Run `gofmt`, `go vet`, `golangci-lint run`, `go test ./... -race -cover`, the 100% coverage gate, `go build ./...`, and gitleaks.
6. Merge the passing feature branch back into local `develop`.

## Open Questions
- None. Push, release tags, and promotion to `main` remain out of scope until manual review and explicit approval.
