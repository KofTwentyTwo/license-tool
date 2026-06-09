# Session State

**Last Updated:** 2026-06-09

## Current Status
Releasing **v0.4.0**. All six feature/bugfix branches were integrated into `develop`
(six `--no-ff` merges, conflicts resolved, combined gate green: gofmt/vet/lint, race
tests, 100% coverage). CHANGELOG finalized as `[0.4.0] - 2026-06-09`. Promoting
`develop` → `main` and tagging `v0.4.0` to trigger the GoReleaser `Release` workflow.

## What shipped in v0.4.0 (issues closed)
- #35 audit reporting overhaul (`--summary`, `--group-by`, `--sort`, `--depth`, `--only`, per-group risk + breakdowns, attributable violations, findings parity) + self-config exclusion + policy-aware group risk.
- #29 init full TUI wizard + live previews + persisted `include`.
- #31 resolve: stop guessing SPDX ids for ambiguous aliases.
- #34 remove no-op `--quiet`/`--verbose` flags.
- #33 refuse to clobber a symlinked LICENSE.
- #30 confine header detection to contiguous comment lines.

## Release process
Documented end-to-end in `RELEASES.md` (full runbook) and `RELEASING.md` (short ref).
Tag `vX.Y.Z` on `main` triggers `goreleaser release --clean`: GitHub Release + Homebrew
cask (`KofTwentyTwo/homebrew-tap`). Verify with `gh release view` and
`brew install --cask KofTwentyTwo/tap/license-tool && license-tool version`.

## Open Threads / follow-ups
- [ ] Deferred LOW items: #41/#30 block-comment de-stacking; #39/#34 `isWriteRefusal` typed-sentinel refactor and the still-dead `model.ResolveOptions.Verbose` field.
- [ ] #32 (include `**`) was fixed on GH-29.

## Key Reference
- Default branch `develop`; production `main`; tags `vX.Y.Z` on `main` publish via GoReleaser.
- Gate: `gofmt -l .`; `go vet ./...`; `golangci-lint run`; `go test ./... -race -coverpkg=./internal/...,./cmd/... -covermode=atomic -coverprofile=cover.out`; `go run github.com/vladopajic/go-test-coverage/v2@v2.18.8 --config=.testcoverage.yml`. (Do not pipe `go build`/`go test` to `tail` — it masks the exit code.)
- Repo: GitHub `KofTwentyTwo/license-tool`; commits reference issues, no AI attribution.
