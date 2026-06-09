# PLAN: GH-29 Init TUI Redesign

> Execution-oriented plan. Design rationale lives in `docs/DESIGN-init-tui-redesign.md`; review findings in `docs/review/`. Supersedes the TUI shape in `PLAN-GH-29-init-tui-wizard.md`. Implemented on the existing `feature/GH-29-init-tui-wizard` branch (issue #29 is unmerged).

**Goal:** Replace the 7-step 3-pane wizard with a single always-visible, repo-seeded form + persistent adaptive live preview, with all logic in a tested `internal/initwizard` package and `wizard.go` reduced to a thin Bubble Tea adapter.

**Architecture:** Pure logic (translate, seed, form state machine, validation, layout, view rendering) lives in `internal/initwizard` and is unit-tested to 100%. `cmd/license-tool/wizard.go` becomes an `Init/Update/View` adapter that delegates to those pure functions. A single `Translate` feeds both the live preview and the write path, eliminating the `previewConfig`/`answersToConfig` drift.

**Tech Stack:** Go 1.25, Bubble Tea / Bubbles / Lip Gloss (already present), go-gitignore (already present), testify. No new dependencies.

---

## File map

| File | Responsibility | Status |
|------|----------------|--------|
| `internal/enumerate/glob.go` (+ existing enumerate.go) | gitignore-style matcher reused by includes, excludes, and wizard glob validation | new + modify (#32) |
| `internal/initwizard/model.go` | `Answers` (trimmed: drop Project/Review/Private dead types), `Field` enum | modify |
| `internal/initwizard/translate.go` | single `Translate(Answers, TranslateOptions) (model.Config, error)` | new |
| `internal/initwizard/seed.go` | `Seed(root, SeedDeps) (Answers, Detected)` repo detection | new |
| `internal/initwizard/validate.go` | per-field validation (`Validate`, `FieldError`) | new |
| `internal/initwizard/form.go` | `FormState` + transitions (focus, expand, commit, glob/license editors, `CanWrite`) | new |
| `internal/initwizard/layout.go` | `Layout(w,h) PanelPlan` adaptive breakpoints + too-small sentinel | new |
| `internal/initwizard/view.go` | pure string rendering from `FormState`+`PanelPlan` | new |
| `internal/initwizard/catalog.go` | sample catalog + preview builders | keep |
| `cmd/license-tool/wizard.go` | thin `tea.Model` adapter delegating to initwizard | rewrite (shrink) |
| `cmd/license-tool/commands.go` | `newInitCmd` seeds + runs form; `answersToConfig` -> `initwizard.Translate` | modify |

---

## Task sequence (each task: red -> green -> refactor -> commit; commit references `#29`/`#32`)

### Task 1 - Fix include `**` (issue #32, unblocks wizard glob teaching)
- Test: `internal/enumerate/glob_test.go` - `include: ["src/**"]` matches `src/sub/x.go`; `["**/generated/**"]` matches `a/generated/b.go`; single-segment still works.
- Impl: route includes through the same go-gitignore matcher as excludes (extract `internal/enumerate/glob.go` with `CompileMatcher(patterns) Matcher` + `Matcher.Match(rel) bool`). Replace `filepath.Match` include path.
- Remove `TestMatchesIncludes`'s assertion that enshrines single-segment-only behavior; replace with `**` cases.
- Gate: `go test ./internal/enumerate -race`. Commit: `fix(#32): include globs honor ** via gitignore matcher`.

### Task 2 - Single translator
- Test: `internal/initwizard/translate_test.go` - strict mode rejects empty holder / unrenderable license; placeholder mode substitutes `Example, Inc.` and never errors on holder; both map year/style/manage/include/exclude identically.
- Impl: `Translate(a Answers, opts TranslateOptions) (model.Config, error)` starting from `config.Defaults()`. `TranslateOptions{AllowPlaceholders bool}`.
- Rewire: `commands.go answersToConfig` -> `Translate(a, TranslateOptions{})`; `catalog.go`/preview path + `wizard.go previewConfig` -> `Translate(a, TranslateOptions{AllowPlaceholders:true})`. Delete `previewConfig`.
- Gate: `go test ./internal/initwizard ./cmd/... -race`. Commit: `refactor(#29): single Answers->Config translator`.

### Task 3 - Trim dead types + seed detection
- Test: `internal/initwizard/seed_test.go` - holder/license seeded from existing managed headers; holder falls back to git author then empty; manage flag false when LICENSE present; `Detected` flags set accordingly; empty repo -> static defaults.
- Impl: remove `ProjectModel*`, `Step*`, `ReviewAnswer`, `ProjectAnswer`, `LicenseAnswer.Private`. Add `Seed(root string, deps SeedDeps) (Answers, Detected)` reusing `detect`, `enumerate`, `spdx`, and a `GitAuthor` seam (default `gitutil`). `Detected struct{ License, Holder, Manage bool }`.
- Gate: `go test ./... -race` (catches removed-type fallout). Commit: `feat(#29): seed init answers from repo detection`.

### Task 4 - Form state machine + validation
- Test: `internal/initwizard/form_test.go`, `validate_test.go` - focus wraps across fields incl. Write row; Enter expands focused field and Esc collapses; license filter narrows choices and commit updates Answers; glob editor adds/removes entries; `Validate` flags empty holder, unparseable year, malformed glob; `CanWrite` false until valid.
- Impl: `Field` enum; `FormState{ Answers; Detected; Focus Field; Expanded bool; License editor state; Glob editor state }`; transitions `MoveFocus`, `Toggle/Expand/Collapse`, `Commit`, `SetFilter`, `AddGlob/RemoveGlob`, `Validate() []FieldError`, `CanWrite() bool`. License options from `licenseSelectOptions` (moved in from wizard.go).
- Gate: `go test ./internal/initwizard -race`. Commit: `feat(#29): pure form state machine and live validation`.

### Task 5 - Adaptive layout
- Test: `internal/initwizard/layout_test.go` - >=110 cols -> 3 panels; 80-109 -> 2; floor..79 -> stacked; <60 or <20 rows -> `TooSmall`; widths sum within bounds.
- Impl: `PanelPlan{ Panels []Panel; TooSmall bool; ... }`; `Layout(w,h int) PanelPlan`.
- Gate: `go test ./internal/initwizard -race`. Commit: `feat(#29): adaptive panel layout planner`.

### Task 6 - Pure view rendering
- Test: `internal/initwizard/view_test.go` - renders form rows with values + detected badges; focused/expanded row shows editor; footer shows blocking validation reason when `!CanWrite`; too-small plan renders the fallback message; preview panels present per plan.
- Impl: `Render(s FormState, plan PanelPlan) string` (+ row/preview/footer helpers) using lipgloss. Move `fitText`/`ellipsize`/`selectionList` in from wizard.go.
- Gate: `go test ./internal/initwizard -race`. Commit: `feat(#29): pure view rendering for init form`.

### Task 7 - Thin Bubble Tea adapter + wire-up + full gate
- Impl: rewrite `wizard.go` to hold `initwizard.FormState`+size; `Update` maps `tea.KeyMsg`/`WindowSizeMsg` to transitions, triggers write on the Write action via `config.WriteFile` (confirm + diff when overwriting), quits on Esc/ctrl+c; `View` calls `initwizard.Render`. Update `newInitCmd` to `Seed` then run. Update `commands_test.go` for the new collector contract.
- Update `.testcoverage.yml` comment if the wizard.go exclusion rationale changes (it stays excluded; now genuinely thin).
- Update `README.md`/`DEVELOPERS.md` init sections.
- Gate (full, per CONTRIBUTING): `gofmt -l .`, `go vet ./...`, `golangci-lint run`, `go test ./... -race -coverpkg=./internal/...,./cmd/... -covermode=atomic -coverprofile=cover.out`, `go run github.com/vladopajic/go-test-coverage/v2@v2.18.8 --config=.testcoverage.yml`, `go build ./...`.
- Commit: `feat(#29): single-form init wizard with adaptive live preview`.

---

## Out of scope (tracked separately)
- Proprietary / `LicenseRef-*` licenses (design decision 9).
- Issues #30, #31, #33, #34 (independent review bugs; not required for #29). #32 is in-scope as Task 1.

## Risks
- 100% coverage gate: every new exported/branch path needs a test before the gate passes. Form state transitions are the largest surface; budget most test effort there.
- View rendering tests can be brittle; assert on stable substrings/structure, not full styled output.
