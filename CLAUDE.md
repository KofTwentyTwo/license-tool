# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`license-tool` is a license-agnostic Go CLI that audits and standardizes license metadata across codebases. Target license, holder, year policy, and header style are all *inputs*, never assumptions (AGPL-3.0-or-later + Kingsrook is only the default profile). It has two halves: an **audit/check** half (read-only reporting + CI gate) and an **apply/license** half (writes canonical headers and top-level LICENSE files). License text is never invented — it comes from a vendored SPDX snapshot.

## Commands

The local quality gate mirrors CI; run all of it before considering work done:

```bash
gofmt -l .                  # must print nothing (tabs, not spaces); CI fails on any output
go vet ./...
golangci-lint run
go test ./... -race -cover
go build ./...
```

```bash
go test ./internal/render -run TestSplice -race   # single package / single test
go run ./scripts/gen_spdx.go                       # regenerate the vendored SPDX snapshot
./scripts/verify-release.sh                        # the full 6-gate release preflight
```

**100% coverage is a hard gate.** CI runs `go-test-coverage` over `./internal/...` and `./cmd/...` and fails below 100% total. Only three files are excluded (see `.testcoverage.yml`): `cmd/license-tool/main.go`, `cmd/license-tool/wizard.go`, and `scripts/gen_spdx.go` — each because it is an untestable shell (process entry point, Bubble Tea TUI, or build-tagged generator). **All behavior must live in covered packages**, not in these three files. This is why `wizard.go` delegates to `answersToConfig` + `internal/initwizard` + `internal/config`, and why business logic never accretes in `main.go`.

## Architecture

### `internal/model` is the frozen contract

`internal/model/model.go` defines every shared type and interface (`Config`, `License`, `FileType`, `DetectedHeader`, `FileResult`, `Report`, `Resolver`, `YearResolver`, etc.). It is **stdlib-only and dependency-free** so every other package can import it without cycles. Changing a field or signature here ripples into all consumers — treat it as load-bearing and change it deliberately, not casually.

### The pipeline (what flows through model)

```
config.Resolve  →  enumerate  →  detect  →  render  →  applier   (apply/license: the write half)
                       ↓            ↓
                    detect  →  report (+ resolve for deps)        (audit/check: the read half)
```

- **`internal/config`** — resolves effective config by layering, high→low: flags > repo `.license-tool.yaml` > user/global (`$XDG_CONFIG_HOME/license-tool/config.yaml`) > built-in defaults. `include` takes the highest non-empty layer; `exclude` accumulates across layers.
- **`internal/enumerate`** — produces the ordered file set. In a git repo, `git ls-files` (authoritative `.gitignore` honoring); otherwise a tree walk via go-gitignore. Skips symlinks/binaries, classifies each file via the file-type table.
- **`internal/filetype`** — the data-driven comment-syntax table mapping path → `FileType` (comment delimiters + ordered preserve-first rules). Data-driven so render/apply stays generic and users extend coverage via config, not code.
- **`internal/detect`** — *safety-critical.* Identifies an existing managed header by, in confidence order: own sentinel → `SPDX-License-Identifier` → known SPDX standard header → curated phrase fingerprint. A false positive means apply deletes a real doc comment, so detection demands a positive license signal inside the leading comment block and never scans below the header.
- **`internal/render`** — produces canonical header text *and* performs the byte-level splice (insert/replace). Rendering and mutation live together so producer and splicer can't drift on header shape, line endings, or the blank-line separator. Carries explicit SAFETY INVARIANTS in its package doc.
- **`internal/applier`** — Mode B orchestration: render per file, compute unified diff, atomically (temp-then-rename) write or replace. Also manages top-level `LICENSE` + `LICENSES/<id>.txt` (REUSE).
- **`internal/resolve`** — per-ecosystem dependency license resolution (maven/npm/gradle). Tiered: on-disk metadata by default, opt-in tool shell-out behind a flag. **Never guesses** — an unresolvable dep is reported `unresolved` with a reason.
- **`internal/report`** — folds per-file + dependency + policy results into one `Report`, renders text/JSON/Markdown deterministically (byte-identical for identical input regardless of order), and owns `Report.Passed` (the `check` exit decision) plus the "not legal advice" disclaimer.
- **`internal/policy`** — classifies licenses and enforces allow/deny/required + curated incompatibilities, emitting violation tokens that drive `check`'s `fail_on` exit. Routes classification through `internal/spdx` so there's no second drifting opinion.
- **`internal/spdx`** — exposes the vendored snapshot with zero runtime network. Two surfaces: `Validate(id)` accepts *any* real SPDX id (permissive — any valid id is a legal target); `Lookup(id)` returns full text/header/category only for the curated set the tool ships rendering for.
- **`internal/gitutil`** — shells out to `git` (clean-tree check, first-commit year, atomic commit) because `git ls-files` is the authoritative `.gitignore` honorer.

### Command layer (`cmd/license-tool`)

`commands.go` builds the cobra tree (`audit`, `check`, `apply`, `license`, `init`, `version`) and wires the concrete `internal` packages into a `report.Pipeline` indirection (`buildAuditPipeline`) so the report layer stays decoupled from enumeration/detection. `main.go` is a thin shim mapping errors to exit codes via `commandError`/`withExitCode`.

## Safety invariants (these are the product, not nice-to-haves)

- **`apply` and `license` are dry-run by default.** Writing without an explicit `--write` is a bug. They require a clean git tree (`--allow-dirty` to override), refuse non-git dirs without `--force`, write atomically, and are idempotent — a second apply is a no-op and headers never stack. Git is the undo.
- **Never invent license text.** New/updated text comes from re-running `gen_spdx.go` against a pinned upstream ref, never hand-edited. (The one documented override is the wrapped AGPL standard header — see `internal/spdx`.)
- **Exit codes are an API:** `0` ok · `1` policy/check failure · `2` usage · `3` write refused · `4` internal.

## Workflow

Feature branches → PR into `develop` → promote green commits to `main`; tags `vX.Y.Z` from `main` drive GoReleaser (GitHub Release + Homebrew cask). When changing header generation, detection, comment-syntax, or policy, add/adjust tests and update golden files (explain the golden diff in the PR). Release flow: `RELEASING.md`.
