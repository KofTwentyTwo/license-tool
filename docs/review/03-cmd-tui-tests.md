# Review 03 — Command layer, init TUI, and test quality

Scope: `cmd/license-tool/main.go`, `cmd/license-tool/commands.go` (+ `commands_test.go`),
`cmd/license-tool/wizard.go`, `internal/initwizard/{model.go,catalog.go}` (+ `catalog_test.go`).
Reviewed against the repo at branch `feature/GH-29-init-tui-wizard`.

## Summary table

| # | Area | Finding | Severity | Location |
|---|------|---------|----------|----------|
| 1 | Command layer | `--quiet` / `--verbose` persistent flags are bound but never read anywhere — dead UX surface | high | `commands.go:57-58` |
| 2 | Init TUI | Dead "Project model" step: collected, previewed, written to `Answers.Project`, but `answersToConfig` never reads `a.Project` — the whole first wizard screen has no effect on output | high | `commands.go:404-440`, `commands.go:452`, `wizard.go:353-354` |
| 3 | Init TUI | ~990-line `wizard.go` mixes pure logic (selection math, parsing, layout, preview assembly) with the Bubble Tea shell, so genuinely testable logic is parked in a coverage-excluded file | high | `wizard.go` (whole file) |
| 4 | Init TUI | Cram-two-values-into-one-textfield: Identity = `holder \| year`, Coverage = `include \| exclude`. Fragile `strings.Cut`/`splitCSV` parsing with no validation feedback in the field | medium | `wizard.go:728-768` |
| 5 | Model | `LicenseAnswer.Private` field is fully dead (never set, never read) | medium | `model.go:73-74` |
| 6 | Init correctness | `init` ignores `cfg.Year`/`cfg.Style`/etc. from a discovered config file: it scaffolds purely from `Defaults()` + flags, never calling `config.Resolve`. A second `init --force` silently discards prior non-flag settings | medium | `commands.go:419`, `commands.go:466-470` |
| 7 | Init correctness | `init` is the only write command that does NOT validate the target dir as a git tree / honor `--allow-dirty`; `--allow-dirty`/`--commit`/`--commit-message`/`--write` flags are bound on `init` (via `bindApplyFlags`) but meaningless there | medium | `commands.go:481` |
| 8 | Test quality | `internal/initwizard/model.go` has NO test file; the `Answers`/`Step` model and `ErrAborted` reach 100% only incidentally via `commands_test.go`/`wizard.go` usage — no behavioral assertions on the model itself | low | `internal/initwizard/` |
| 9 | Test quality | `TestInitCommandConsumesCollectorAnswers` asserts `initial.Project.Model` is passed in but never asserts the returned `Project`/`Review` affect output (consistent with finding #2 — there is nothing to assert, which masks the dead field) | low | `commands_test.go:1134-1192` |
| 10 | Command layer | `isWriteRefusal` classifies write-refusal vs internal errors by `strings.Contains` on the error message — brittle string coupling to `internal/applier` and `internal/config` wording | medium | `commands.go:290-293` |
| 11 | Command layer | `check --fail-on` only overrides policy when the flag was `Changed`; the bound default `[]string{...}` is therefore inert and exists only as help text. Mild redundancy, not a bug | low | `commands.go:143-149`, `commands.go:189` |

Overall: the command layer is correct on the load-bearing guarantees (dry-run-by-default,
exit-code mapping, `--fail-on` parsing, output-file handling, `isTTY` gating) and is
genuinely well-tested. The init TUI is the weak spot: it is large, carries a dead step and
a dead field, and parks testable logic inside the coverage-excluded shell. Test quality is
high and substantive almost everywhere — the few hollow spots are a direct consequence of
the dead wizard surface, not coverage-padding tricks.

---

## 1. Command layer correctness (focus area 1)

### Dry-run-by-default — CORRECT
`apply` and `license` default `--write=false` (`commands.go:385`) and pass `Write: f.write`
into `applier.Apply`/`applier.License`. `applier.go` guards every mutating branch behind
`opts.Write` (`applier.go:73,134,137,153,165,176`), so absent `--write` the run only
produces diffs. `TestApplyCommand/dry-run reports without writing` (`commands_test.go:646`)
asserts the source file is left untouched, and `dry-run json includes unified diff` /
`write json omits unified diff` pin the diff-presence contract. This guarantee is solid and
well covered.

### Exit-code mapping — CORRECT
`main.go` maps via `commandError`/`withExitCode`/`exitCode`:
- usage = 2 (also the fallback for untyped cobra errors via `exitCode`'s default arm),
- check failure = 1,
- write refusal = 3,
- internal = 4.

`commands_test.go:195-246` covers all four through `executeRoot`, and
`TestCheckFailingExits` (`commands_test.go:564`) uses the subprocess re-exec idiom to prove
the real `os.Exit(1)` path. `TestWithExitCodeNilError` covers the nil short-circuit. This is
thorough and correct.

### `--fail-on` parsing — CORRECT, one redundancy (finding #11)
`parseFailOnFlags` (`commands.go:337`) splits on commas AND across repeated flags via
`splitFlagTokens`, trims, drops empties, and rejects unknown tokens. `TestParseFailOnFlags`
covers the mixed `"a, b"` + repeated-flag + unknown cases.

Redundancy: the override only fires when `cmd.Flags().Changed("fail-on")` is true
(`commands.go:143`). The bound default list at `commands.go:189` is therefore never used to
*set* policy (the config/Defaults layer supplies the effective default); it serves only as
`--help` text. Harmless, but the dead default invites a future reader to assume it is
authoritative. Consider binding `--fail-on` with a nil default and documenting the real
default in the flag usage string.

### Output-file handling — CORRECT
`renderCommandReport` (`commands.go:314`) writes to stdout when `--output` is empty,
otherwise opens via the `createReportFile` seam, renders, and closes — correctly reporting
both render and close errors as internal (exit 4). `TestRenderCommandReportErrors` injects a
close-failing writer through the seam and asserts the close error is surfaced; the
`audit`/`check` "output file create error is internal error" subtests cover the create-fail
path. Good seam design and good coverage.

### `isTTY` gating — CORRECT
`isTTY` (`commands.go:652`) uses `term.IsTerminal` rather than an `os.ModeCharDevice` bit
test; the comment correctly explains that `/dev/null` is a char device but not a terminal.
`TestIsTTY` covers char-device, regular-file, and closed-fd cases. Note the gating reaches
TWO different interactive paths: the Bubble Tea wizard (`init`) and the line-prompt
`fillRequired` in `internal/config/config.go:617` (`apply`/`license`). Both are driven off
`isTTY()`; the test suite forces non-TTY via `forceNonTTYStdin`, so neither prompts in CI.

### Dead flags — finding #1 (high)
`--quiet`/`--verbose` are registered on the root persistent flag set (`commands.go:57-58`)
and stored on `sharedFlags`, but a grep across `cmd/` and `internal/` shows the fields are
read nowhere. They are pure no-ops. Either wire them into report verbosity / output
suppression or remove them; shipping flags that silently do nothing is a correctness/UX
trap. (They are not covered by any assertion either, so removing them is free.)

### `isWriteRefusal` string-matching — finding #10 (medium)
`isWriteRefusal` (`commands.go:290`) decides exit 3 vs exit 4 by
`strings.Contains(msg, "refusing to write")` / `"already exists (use --force)"`. This
couples the command layer to exact error wording in `internal/applier` and
`internal/config`. A reworded error in those packages would silently downgrade a write
refusal to an internal error (3→4) with no compile-time or test signal. Prefer a typed
sentinel (e.g. `errors.Is(err, applier.ErrWriteRefused)`) so the classification is
enforced by the type system.

---

## 2. The init TUI as code (focus area 2)

### Why it is 990 lines and uncoverable
`wizard.go` is excluded from the 100% gate (`.testcoverage.yml`) on the stated grounds that
it is "a Bubble Tea terminal shell ... no write-path validation of its own." That is only
half true. The file contains a large amount of pure, deterministic, trivially-testable
logic that has nothing to do with the terminal event loop:

- selection math: `wrapIndex`, `clampInt`, `windowStart`, `indexString`,
  `indexProjectModel`, `moveSelection*` (`wizard.go:578-692,924-961`);
- license filtering: `filteredLicenseChoices`, `licenseChoicePosition`,
  `alignLicenseSelection`, `isCommonLicense` (`wizard.go:639-682`);
- input parsing: `commitInput`, `coverageInputValue`, `splitCSV` (`wizard.go:728-768`);
- preview assembly: `previewConfig`, `previewResolvedYear`, `previewCandidatePaths`,
  `ignoredPreviewDir`, `previewSummary` (`wizard.go:501-527,770-853`);
- text layout: `fitText`, `ellipsize`, `selectionList`, `licenseBody`, `reviewBody`,
  `navigationBody`, `stepTitle` (`wizard.go:228-499,895-922`);
- formatting helpers: `yesNo`, `listOrAll`, `listOrNone`, `valueOrPlaceholder`.

None of these require a PTY. Only `Init`/`Update`/`View`/`runInitWizard`/`newInitWizardModel`
and the layout-width branches are genuinely shell-bound. The coverage exclusion is being
used to hide ~400+ lines of untested-but-testable logic behind the "it's a TUI" rationale.

Recommended extraction (does NOT change UX): move the pure pieces into
`internal/initwizard` (the package already exists and is fully tested) — e.g.
`initwizard.LicenseChoices(opts, filter, selectedIndex)`, `initwizard.PreviewConfig(...)`,
`initwizard.CoverageInput(...)`/`ParseCoverageInput(...)`, and the `wrapIndex`/`windowStart`
math. `wizard.go` then keeps only `tea.Model` glue and remains legitimately excluded. This
would meaningfully raise real (not gamed) coverage and let edge cases (empty license list,
filter-with-no-matches, window scrolling at boundaries) be asserted directly.

### Dead "Project model" step — finding #2 (high)
The wizard's first screen collects `Answers.Project.Model` (`wizard.go:353-354,580-582`),
the review screen displays it (`wizard.go:482`), and `newInitCmd` seeds it to
`ProjectModelAdvancedManual` (`commands.go:452`). But `answersToConfig` (`commands.go:404`)
never reads `a.Project` — it builds the config from License/Identity/HeaderStyle/
LicenseFiles/Coverage only. So the entire Project-model step is a no-op: whatever the user
picks, the generated `.license-tool.yaml` is identical. This is the single most misleading
thing in the init flow. Either (a) make the project model actually drive defaults
(open-source → manage license files + reuse+notice, private-internal → notice-only, etc.),
or (b) remove the step and the `ProjectAnswer` field until it has an effect. The
`projectModelOptions` slice and `ProjectModel*` constants are otherwise only self-referential.

### Cram-two-values-into-one-textfield — finding #4 (medium)
- Identity field packs `holder | year` into one text input (`wizard.go:716-719`), parsed by
  `strings.Cut(value, "|")` in `commitInput` (`wizard.go:730-737`). If the holder legitimately
  contains a `|`, parsing breaks. If the user omits the separator, the whole string becomes
  the holder and year silently stays at its prior value.
- Coverage field packs `include globs | exclude globs` (`wizard.go:720-722,738-747`), split
  on `|` then comma via `splitCSV`. Same separator-collision fragility; a glob containing a
  literal `|` (rare but legal in some shells/patterns) would be mis-split.

There is no inline validation: a malformed entry is only rejected later in
`answersToConfig` (bad year/style) or accepted silently (bad globs). The instruction says
do not redesign UX — flagging purely as a code-fragility / maintainability concern. If kept,
the parse functions should at minimum be extracted and unit-tested with collision cases.

### Maintainability problems (concrete)
- `bodyView` (`wizard.go:271-332`) hard-codes a thicket of magic layout numbers
  (`< 100`, `46`, `72`, `34`, `navWidth 22/18/24`, `topHeight` clamps) with no named
  constants and no tests; any change risks silent visual regressions that the excluded-file
  status guarantees won't be caught.
- Two parallel ordered lists of steps exist: the `Step` iota in `model.go:28-43` and the
  literal slice in `navigationBody` (`wizard.go:389-397`). They must be kept in sync by hand;
  adding a step requires editing both plus `stepTitle`'s switch (`wizard.go:232-248`).
- `previewConfig` (`wizard.go:770`) and `answersToConfig` (`commands.go:404`) independently
  translate `Answers → model.Config` with subtly different rules (preview substitutes
  `"Example, Inc."` for an empty holder and swallows parse errors; the real path rejects
  them). Two translators for one concept is a drift hazard — the preview can show a config
  the real writer would reject.

---

## 3. Test quality across the repo (focus area 3)

### Command layer tests — substantive, not padding
`commands_test.go` is strong: assertions check real output content (`+  SPDX-License-Identifier`,
unified-diff markers, JSON schema string, `result: PASS/FAIL`), file-system side effects
(header applied vs. not, LICENSE files created vs. absent), git commit scoping
(`--name-only` of HEAD excludes unrelated files), and exit codes via `executeRoot`. Negative
paths are covered with message assertions (bad format, bad fail-on token, unknown resolver
tier, missing `--config`, non-curated `Zlib`). The seams (`createReportFile`,
`interactiveCollect`) are injected to reach error arms deterministically. The subprocess
re-exec for `os.Exit` is the correct idiom. testify usage is idiomatic (`require` for
fatal preconditions, `assert` for soft checks); table tests in `TestArgPath`/`TestParseFailOnFlags`
are sound. This is meaningful coverage.

### `answersToConfig` gate — well-tested
`TestAnswersToConfig` exercises the valid case plus every rejection arm (unknown license,
empty holder, bad year, bad style) and the "empty year/style keep Defaults" branch. This is
exactly the right place to concentrate assertions given the wizard is excluded, and it does
the job.

### `internal/initwizard/catalog_test.go` — substantive
Asserts the catalog's exact membership and stable order (15 families), copy-semantics
(`TestCatalogReturnsACopy` mutates the returned slice and re-reads), extension→family
mapping including the C vs C++ split and C#, and `BuildSourcePreview` edge cases (empty
sample→C fallback, missing year, unknown license, no-file-type, uncommentable override).
`BuildYAMLPreview` is asserted to byte-match `config.RenderFile`. Good golden-style and
edge-case coverage. Minor: `indexOf` is a hand-rolled substring search where
`strings.Index` would do — harmless test-helper verbosity.

### Hollow / missing spots
- **`internal/initwizard/model.go` has no `model_test.go`** (finding #8, low). The
  `Answers`/`Step`/`*Answer` types and `ErrAborted` are data/constants, so reaching them via
  other packages satisfies the line gate, but there is no test that documents the model's
  contract (e.g. step ordering, that `ErrAborted` is the sentinel `runInitWizard` returns).
  This is benign for a pure data file, but it means the 100% number is partly incidental
  here rather than earned by direct assertions.
- **`TestInitCommandConsumesCollectorAnswers`** (finding #9, low) passes `initial.Project.Model`
  in and asserts it, and returns a `Project`/`Review` in the fake answers, but never asserts
  those affect the written file — because they can't (finding #2). The test thus *looks* like
  it validates the project model round-trips when in fact the field is inert. This is not
  coverage-padding in the gaming sense, but it does give a false impression of behavior; a
  reader should not trust that the Project field matters from this test.

### testify / table-test soundness — good
`require` vs `assert` discipline is correct throughout. `t.Setenv`/`t.TempDir`/`t.Cleanup`
are used to isolate (`isolateEnv` pins `XDG_CONFIG_HOME` and forces non-TTY stdin). No
obvious flaky-by-clock or order-dependent tests in the reviewed files. The one global-state
mutation (`os.Stdin`, `createReportFile`, `interactiveCollect`) is always paired with a
`t.Cleanup` restore.

---

## Recommended actions (priority order)
1. Resolve the dead Project step (#2): either wire `a.Project` into `answersToConfig`
   defaults or remove the step + `ProjectAnswer` + `LicenseAnswer.Private` (#5).
2. Wire or remove `--quiet`/`--verbose` (#1).
3. Extract pure logic from `wizard.go` into `internal/initwizard` and add direct tests (#3);
   collapse the two `Answers→Config` translators (preview vs real) into one.
4. Replace `isWriteRefusal` string matching with a typed sentinel error (#10).
5. Reconcile `init`'s flag surface: drop `--allow-dirty`/`--commit`/`--commit-message`
   (or honor them) and decide whether `init` should layer an existing config via
   `config.Resolve` rather than scaffolding only from Defaults+flags (#6, #7).
