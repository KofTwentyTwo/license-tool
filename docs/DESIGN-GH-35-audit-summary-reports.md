# DESIGN: GH-35 Audit Summary + Group-By Report Options

Status: **proposed** (brainstormed and approved; awaiting spec review). Issue: #35.

## Problem

The text and markdown audit renderers always emit a flat per-file list (every path), which buries the grouped rollups (`by SPDX id`, `by category`, `by file type`) on any non-trivial repo. There is no way to get a concise, human-readable, grouped view. Add opt-in summary and group-by options. The default output stays exactly as today (backward compatible).

## Approach

**Render-time grouping.** `model.Report` stays frozen. Grouping is derived from `r.Files` at render time by a pure, unit-tested function. A `RenderOptions` value is threaded into `report.Render` and honored by all three renderers. (Rejected: adding a `Groups` field to the frozen model; a separate `summarize` subcommand.)

## Flags (bound on both `audit` and `check` via the shared `bindAuditFlags`)

- `--summary` (bool, default false): counts only. Omits the per-file list, the per-dependency list, and pending diffs. Keeps findings, the count rollups, and policy violations.
- `--group-by <dim>` (string, default ""): one of `license` | `category` | `type` | `directory`. Organizes the source-file listing under each value of the dimension. Empty means the default flat list. An unknown value is a usage error (exit 2), validated like `--resolve-deps`.

### Orthogonal model

The three standalone count rollups (`by SPDX id`, `by category`, `by file type`) are **always** shown (unchanged). Only the **file listing** changes:

| flags | file listing |
|---|---|
| (none) | flat list of every source file — unchanged default |
| `--group-by X` | source files nested under each value of X: `KEY (n):` then the file lines |
| `--summary` | omitted entirely (also omits dep list + diffs) |
| `--summary --group-by X` | per-group **counts** for X only (`KEY: n`), no file paths |

### Scope

- `--group-by` groups **source files only**. The dependency section is unaffected (full list normally; replaced by the resolved/unresolved counts under `--summary`). Grouping dependencies by license/ecosystem is a deliberate non-goal.
- `GroupFiles` operates over the **non-skipped** source set (the licensable files), for every dimension. Skipped files (binary/uncommentable/unknown) are reported as a trailing `(skipped: N)` note in the grouped section, never grouped. The pre-existing count rollups keep their current populations.

### Group keys

- `license`: `fr.Detected.SPDXID` when a header is present and non-empty, else `(none)`.
- `category`: the policy category token for that id (`permissive`, `strong-copyleft`, ...), else `unknown`.
- `type`: `fr.FileType` (the matched type name).
- `directory`: the first path segment of `fr.Path`; files at the root group under `.`.

Groups are sorted by key; files within a group are sorted by path. Deterministic for identical input.

## Components

- **`internal/report/group.go`** (new):
  - `type GroupDimension int` with `GroupNone, GroupLicense, GroupCategory, GroupType, GroupDirectory`.
  - `func ParseGroupBy(string) (GroupDimension, error)` — `""`->`GroupNone` (no error); the four tokens; else error `report: unknown group-by dimension %q (expected license|category|type|directory)`.
  - `type Group struct { Key string; Count int; Files []model.FileResult }`.
  - `func GroupFiles(r model.Report, dim GroupDimension) (groups []Group, skipped int)` — pure, sorted, deterministic. `GroupNone` returns `(nil, skipped)`.
- **`internal/report/report.go`**:
  - `type RenderOptions struct { Summary bool; GroupBy GroupDimension }`.
  - `Render(w, r, format, opts)` — signature gains `opts`. `renderText` / `renderMarkdown` / `renderJSON` each honor `opts`:
    - text/markdown: always print findings + the three count rollups; render the file listing per the table above; under `--summary` also drop the dependency list and pending diffs (keep `dependencies: N (resolved R, unresolved U)` from findings) and policy violations stay.
    - JSON: when `GroupBy != GroupNone`, add a `groups` array (`[{key, count, files?}]`); under `Summary`, omit per-file `files` detail and the `dependencies` array, keeping the count maps and `violations`. Default JSON (no flags) is byte-identical to today. Additive schema, still `license-tool/report/v1`.
- **`cmd/license-tool/commands.go`**:
  - `auditFlags` gains `summary bool` and `groupBy string`.
  - `bindAuditFlags` registers `--summary` and `--group-by`.
  - `newAuditCmd`/`newCheckCmd` parse+validate `--group-by` (usage error on unknown) and pass `report.RenderOptions` through `renderCommandReport` -> `report.Render`.

## Error handling

- Unknown `--group-by` value: usage error, exit 2, before any rendering.
- `--summary` and `--group-by` compose (no conflict); both may be combined per the table.
- All existing exit-code and rendering behavior for the no-flag case is preserved.

## Testing (hold the 100% gate)

- `ParseGroupBy`: every token + empty + unknown.
- `GroupFiles`: each dimension's keying (including `(none)` license, `unknown` category, root `.` directory), skipped-count exclusion, sort determinism, `GroupNone`.
- Renderers: for text, markdown, and JSON — default unchanged (golden), `--summary`, `--group-by` each dimension, and `--summary --group-by`.
- Command layer: flag wiring, unknown-dimension usage error (exit 2), `--summary`/`--group-by` reach `Render`.

## Docs

- README `audit` section: document `--summary` and `--group-by` with a short example of each.

## Out of scope

- Grouping dependencies; multiple simultaneous `--group-by` dimensions; a configurable directory depth (top-level only for now).
