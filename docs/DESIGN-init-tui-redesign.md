# DESIGN: `init` TUI Clean-Room Redesign

Status: **proposed** (decisions confirmed in a grill-me session; not yet planned/implemented)
Supersedes the TUI shape from `docs/PLAN-GH-29-init-tui-wizard.md`.
Companion review findings: `docs/review/01-write-path-safety.md`, `02-audit-policy-resolve.md`, `03-cmd-tui-tests.md`.

## 1. Problem

The current `init` wizard (`cmd/license-tool/wizard.go`, ~990 lines, excluded from coverage) is functionally close but, per the user, "terrible and more or less not usable." Concrete causes, confirmed by code review:

- **3 panes fight for width** (Steps nav | controls | preview); collapses to a stacked mess below 100 cols.
- **Cram-fields:** Identity is one text input parsed as `holder | year`; Coverage is one input parsed as `include | exclude` with each side comma-split. Nobody guesses these grammars.
- **A dead step:** "Project model" is collected, previewed, and written to `Answers.Project`, but `answersToConfig` never reads it — the first screen has zero effect.
- **End-loaded validation:** an empty holder is accepted through Review and only rejected *after* the user confirms write, bouncing them out.
- **Two `Answers→Config` translators** (`previewConfig` vs `answersToConfig`) that can silently drift — so the preview can render something different from what gets written.
- **~400 lines of pure logic trapped** in the uncoverable `wizard.go` (selection math, filtering, parsing, preview assembly, layout sizing).

The *what* (collect config, teach via preview, write `.license-tool.yaml`) is right. The *how* is being redesigned clean-room.

## 2. North star & principles

**North star: a teaching tool driven by a live preview.** Every choice immediately re-renders an example source header (and the YAML) so the user understands the effect before writing. (Chosen over "fast correct config," "power config," and "minimal bootstrap.")

Principles, in priority order:
1. **See the effect, always.** The preview is persistent and never blank.
2. **Detect, don't interrogate.** Open pre-filled from the repo; the user confirms/tweaks.
3. **Can't reach a bad write.** Validate live; disable Write until valid.
4. **One source of truth.** A single `Answers→Config` translator feeds both preview and write.
5. **Honest coverage.** Logic is tested; the TUI shell is trivial enough that its exclusion is legitimate.

## 3. Resolved decisions

| # | Decision | Choice | Rejected alternatives |
|---|----------|--------|----------------------|
| 1 | North star | **Teaching tool via live preview** | fast-correct-config; power config; minimal bootstrap |
| 2 | Interaction model | **Single always-visible form**, all fields, edit in any order, persistent preview | linear stepper; grouped sections; preview-dominant overlay |
| 3 | Layout | **Adaptive panel count** (3 cols wide / 2 medium / stacked narrow); hard floor refuses the TUI | fixed responsive 2-pane; hard-min two-column; single-column peek |
| 4 | Defaults | **Detect & pre-fill, badged + editable** (retires the Project-model step) | light detection; static only; detection-on-keypress |
| 5 | Complex-field editing | **Inline-expanding rows** (filter-list / list editor / text expand in place) | modal overlay; full-pane focus; drill-in sub-screen |
| 6 | Validation | **Live inline; Write disabled until valid**, footer shows why, preview uses placeholder holder | validate-on-commit; validate-at-write; live + confirm-anyway |
| 7 | Code structure | **Thin shell + fat tested core**; single `Answers→Config` translator | eliminate exclusion via teatest; extract-only; spike-first |
| 8 | Write flow | **No review screen**; write disabled until valid; **confirm + old→new diff only when overwriting** | explicit final review; immediate no-confirm; always-diff |
| 9 | Private/internal path | **Cut now, track as a separate feature** | minimal "internal" marker; build LicenseRef-* now; leave dead |

## 4. Redesigned UX

### 4.1 The form (single screen, fields editable in any order)

Rows, top to bottom; each shows `label: <current value>` (with a `detected` badge where seeded), expands in place on Enter:

1. **License** — inline filter-as-you-type list over the *renderable* SPDX set (common ids first, then the rest). (Validation can accept any SPDX id, but `init` writes only renderable ids — so the picker is constrained to renderable, eliminating the "valid but unrenderable" error class.)
2. **Holder** — text input. Required.
3. **Year** — pick: `current` · `git` · `explicit` (then `YYYY` or `YYYY-YYYY`). Default `git`.
4. **Header style** — pick: `reuse` · `notice` · `reuse+notice`, each with a one-line plain-language implication. Default `reuse+notice`.
5. **Manage license files** — toggle (`LICENSE` + `LICENSES/<id>.txt`). Default true if no `LICENSE` exists yet; if one exists, default to standardize-with-confirm.
6. **Include globs** — inline list editor (add / select / remove). Empty ⇒ "all supported files."
7. **Exclude globs** — inline list editor; accumulates on top of `.gitignore`.

Bottom: a focusable **`Write .license-tool.yaml`** affordance, hard-disabled while any field is invalid; footer shows the blocking reason.

### 4.2 Layout breakpoints (starting values; validate during implementation)

- **Wide (≥ ~110 cols):** 3 columns — Form (~40) · Source preview (flex) · YAML preview (~40).
- **Medium (~80–109):** 2 columns — Form (~40) · Source preview (flex). YAML shown as a switchable tab on the preview pane (or stacked beneath source preview if height allows).
- **Narrow (≥ floor, < ~80):** single column — Form full width, Source preview stacked below (shorter, still persistent).
- **Below floor (< ~60 cols or < ~20 rows):** refuse the TUI; print a message pointing to the flag-only non-TTY path (`init --license ... --holder ...`) or to widen the terminal.

The dedicated "Steps" nav rail is **removed** (no steps to track in a single form), reclaiming ~24 cols. A compact validity/affordance line lives in the footer.

### 4.3 Preview content

- **Source preview** is the hero — the example file (language auto-selected from the repo; C fallback) with the generated header applied, re-rendered on every edit via the *same* `render` path apply uses.
- **YAML preview** shows the exact `.license-tool.yaml` to be written.
- **License-files** behavior and **coverage** summary become compact status lines (within/under the YAML panel), not full panels.
- On overwrite, the YAML panel switches to an **old→new diff**.

### 4.4 Keymap (replaces today's overloaded `backspace`)

- `Tab`/`Shift+Tab` or `↑`/`↓`: move field focus.
- `Enter`: expand focused row / commit an open editor.
- `Esc`: collapse an open editor without committing; if nothing is open, quit without writing.
- Filter-list editor: type to filter, `↑`/`↓` select, `Enter` commit, `Esc` cancel.
- Glob-list editor: type + `Enter` to add, `↑`/`↓` to select an entry, `d`/`Delete` to remove, `Esc` to finish.
- `Ctrl+S` (or focus the Write row + `Enter`): write — disabled while invalid; confirm if overwriting.
- `?`: toggle help. `Ctrl+C`: quit without writing.

### 4.5 Detection seeding (decision 4)

On launch, seed `Answers` (reusing tested code), badging each value and falling back to static defaults when detection is empty:
- **License / Holder:** run `internal/detect` over enumerated source files for existing managed/recognized headers → dominant SPDX id and holder. Cross-check a top-level `LICENSE` / `LICENSES/` for an id.
- **Holder fallback:** `git config user.name` (or repo org) when headers yield nothing.
- **Manage flag:** seeded from whether a top-level `LICENSE` already exists.
- **Preview sample:** `enumerate` language classification → `initwizard.SelectSample` (already implemented).
- Nothing is written unreviewed; seeds are editable form values, consistent with the tool's "never guess silently" ethos.

## 5. Architecture (decision 7)

**All logic in `internal/initwizard` (tested); `wizard.go` is a trivial Bubble Tea adapter.**

`internal/initwizard` gains:
- `Translate(Answers) (model.Config, error)` — the **single** translator. `cmd`'s `answersToConfig` and the preview's `previewConfig` both collapse into this. Kills the drift class (review 03).
- `Seed(root, env) Answers` — detection seeding (reuses `detect`/`enumerate`/`gitutil`).
- A pure form state machine: field focus, per-field expansion state, per-field validation results, derived "can write" flag — as plain types + transition functions.
- `Layout(width, height) PanelPlan` — pure breakpoint selection.
- View helpers returning strings (unit-testable; drive selected transitions with synthetic `tea.Msg` where cheap).

`cmd/license-tool/wizard.go`: an `Init/Update/View` adapter that routes `tea.KeyMsg` to `initwizard` transitions and renders via `initwizard` view helpers. Small enough that its coverage exclusion is honest. (Eliminating the exclusion via `teatest` was considered and deferred.)

## 6. Entangled review fixes

**Hard dependency (must fix to ship this design):**
- **[HIGH] `include` `**` globs match nothing** (`enumerate.go`, review 02 #1). Includes use `filepath.Match` (no `**`); excludes use go-gitignore (has `**`). Decision 5 validates globs against "the real matcher" and the Coverage rows teach glob authoring — so includes must move to the **same gitignore-style matcher** as excludes, and the test that enshrines the broken single-segment behavior must be removed. Without this, the wizard would actively teach broken globs.

**Independent but recommended to track alongside (not blockers):**
- **[HIGH]** `detect` absorbs an adjacent doc comment across a blank line and deletes it on replace (review 01 #1).
- **[HIGH]** `spdxnorm` alias table *guesses* licenses (`"bsd"`->BSD-2-Clause, LGPL->`-or-later`), violating "never guess" (review 02 #2).
- **[MED]** `detect` matches `SPDX-License-Identifier` mentioned anywhere in a leading comment (01); symlinked `LICENSE` clobber (01); `--quiet`/`--verbose` are no-op flags (03).

## 7. Out of scope

- **Proprietary / `LicenseRef-*` licenses** (decision 9): cut the dead `LicenseAnswer.Private` / private-model scaffolding; file proprietary support as its own issue spanning `render`/`policy`/license-file management. The "standard SPDX used internally" case is already served by picking that license.

## 8. Lower-confidence details to confirm during planning

- Exact breakpoint widths/heights and the medium-layout YAML treatment (tab vs stacked).
- Whether `Manage license files` defaulting to "standardize an existing LICENSE" needs its own confirm.
- Help affordance depth (`?` overlay vs persistent footer hints).
