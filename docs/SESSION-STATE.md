# Session State

## Current Branch
`feature/GH-29-init-tui-wizard`

## Current Objective
Redesign `license-tool init` per `docs/DESIGN-init-tui-redesign.md`: single always-visible, repo-seeded form with an adaptive live preview; all logic in a tested `internal/initwizard`; `wizard.go` a thin Bubble Tea adapter. Plan: `docs/PLAN-GH-29-init-tui-redesign.md`.

## Status
- Full-codebase review complete: `docs/review/01-03`. Bugs filed: #30, #31, #32 (blocker for #29), #33, #34.
- Task 1 done (commit `4e3561e`): include globs honor `**` via shared `enumerate.CompileMatcher` (#32).
- Task 2 done (commit `bafa281`): single `initwizard.Translate` replaces `answersToConfig` + wizard `previewConfig`; tests moved to `internal/initwizard/translate_test.go`.

## Next Step
Task 3: trim dead types (`ProjectModel*`, `Step*`, `ReviewAnswer`, `ProjectAnswer`, `LicenseAnswer.Private`) and add `initwizard.Seed(root, deps)` repo detection. Then Tasks 4-7 (form state machine, layout, view, thin adapter + full gate). NOTE: the current `wizard.go` still references the soon-to-be-removed types; Task 7 rewrites it.
