# Clean-Room Review 2 — `audit` Grouping & Reporting

Reviewer: independent (engineer + LLM-consumer lens). Skeptical, evidence-based.
Date: 2026-06-08. Branch: `feature/GH-29-init-tui-wizard`.
Binary built to `/tmp/cr2-lt` from `cmd/license-tool` (clean build, no tracked files touched).

## Test rig

Throwaway git repo at `/tmp/cr2repo`. Real headers applied with the tool's own
`apply --write --force`, one license per subtree so groupings have something to chew on:

- `src/core/*.go` → MIT (one file deliberately stripped of its header → `(no-header)`)
- `src/util/{c.go,d.py}` → Apache-2.0
- `scripts/{f.sh,g.py}` → AGPL-3.0-only
- `vendor/lib/e.go` → MIT
- `.license-tool.yaml` committed with `policy: { allow: [MIT] }` so Apache/AGPL fire violations
- `go.mod` with a `require` to exercise the dependency path

File mix: Go, Python, Shell, YAML across 4 directory subtrees and 2 depths. Every flag
combination was run across `text`, `json`, and `markdown`.

---

## Prior-issue verdicts

### 1. Grouped/summary views had no risk signal; `--group-by directory` lost license info — ADDRESSED
Every group now carries a per-group `[risk: …]` and (for non-license groupings) a
`licenses:` breakdown. `--group-by directory` shows both, in all three formats. JSON
groups carry `key/count/risk/licenses{}/files[]`. Markdown renders per-group `risk:` and a
`Licenses:` line plus a file table. Solid.

### 2. `policy-violation` was opaque — ADDRESSED
File-attributable violations now name license + rule + file. Text/markdown:
`scripts/f.sh: license "AGPL-3.0-only" is not on the allow list`. Markdown gives a real
table with separate `Condition | License | Location | Detail` columns. JSON
`violationDetails[]` carries structured `{condition, spdxId, path, message}`. An LLM can
branch on `condition`/`spdxId` without string-parsing. This is the biggest improvement.

### 3. No totals or percentages — ADDRESSED (text/markdown), PARTIAL (JSON)
Text and markdown rollups now show per-row `%` and a `total` row. JSON deliberately does
**not** carry percentages or totals — it ships raw count maps (`licenseCounts{}` etc.) and
expects the consumer to compute. Defensible (machines can divide), but worth noting it is
not symmetric with the human formats. Counts-only in JSON is fine; the asymmetry is a
documentation footnote, not a bug.

### 4. `findings` block existed only in text — ADDRESSED
`findings` is present in all three. JSON `findings{}` is the richest: explicit
`riskLevel`, `worstCategory`, `sourceTotal/Headered/Missing`, `unknownCount`, `copyleft[]`,
and `depsScanned/Total/Resolved/Unresolved`. Markdown has a `## Findings` section. Good for
LLMs — a model can read one object and decide pass/fail/triage.

### 5. Lexical-only sort; no sort-by-count — ADDRESSED
`--sort count` works and reorders rollups and groups by descending count (ties fall back to
key order, so output stays byte-stable). `--sort key` remains the default.

### 6. Directory grouping was top-segment only — ADDRESSED
`--depth N` works. At `--depth 1`, `src/core` and `src/util` collapse to `src`; at
`--depth 2` they split correctly into `src/core` and `src/util` with independent risk and
license breakdowns. Behaves as documented.

### 7. No "problems only" filter (`--only missing,copyleft,violations`) — STILL OPEN
`--only` is not a flag (`unknown flag: --only`). There is no way to filter the report to
just the failing rows. For large trees an engineer still has to eyeball or post-filter, and
an LLM has to ingest the whole file list to find the 3 violations. This is the most
impactful remaining gap.

### 8. Dependencies couldn't be grouped; dropped from `--summary` — PARTIAL
- Dropped-from-summary: ADDRESSED. JSON `--summary` `findings{}` retains
  `depsScanned/depsTotal/depsResolved/depsUnresolved`, so dependency posture survives the
  trim.
- Grouped: STILL OPEN. `--group-by dependency` is rejected:
  `unknown group-by dimension "dependency" (expected license|category|type|directory)`.
  Dependencies cannot be grouped by license/ecosystem. (Note: in my rig the resolver
  reported 0 deps despite a `go.mod require`, likely module-cache dependent; I could not
  exercise a populated dependency grouping, but the dimension simply does not exist.)

### 9. `(none)` overloaded as both missing-header bucket and empty placeholder — PARTIAL
Mostly fixed: the missing-header bucket is consistently `(no-header)` in rollup tables and
in every group's `licenses:` map; the empty config placeholder is `(none)`. They are now
visually distinct. **Residual inconsistency:** the `findings:` line still labels the same
bucket `none` (`license types: … none 2`), a third spelling for the same concept. Pick one
token (`(no-header)`) everywhere.

### 10. JSON `--summary` trimmed files/deps (wrong for machines) — STILL OPEN (by design)
JSON `--summary` still omits the `files` and `dependencies` arrays (confirmed in
`internal/report/report.go:858` `jsonSummaryReport`, and at runtime: `files`/`dependencies`
keys absent). The code comment states this is intentional ("counts only … no per-file or
per-dependency detail"). The original objection stands as a philosophical disagreement: a
machine asking for `--format json` arguably always wants the full record, and `--summary`
is a human ergonomic. Today a consumer must choose between summary-shaped findings and the
file list — they cannot get findings + machine detail in one trimmed call. Verdict: OPEN,
but it is a deliberate stance rather than a regression.

**Tally:** ADDRESSED 6 (1,2,4,5,6 + the summary-deps half of 8) · PARTIAL 3 (3, 8, 9) ·
STILL OPEN 2 (7, 10).

---

## Fresh assessment

### Genuinely good
- **Markdown is PR-ready.** The violations table with discrete License/Location/Detail
  columns drops straight into a PR comment and reads well. Findings + rollups + grouped
  tables are all coherent.
- **JSON is LLM-friendly where it counts.** `findings.riskLevel`/`worstCategory` and
  structured `violationDetails[]` mean a model branches on enums, not prose. `schema`
  versioning (`license-tool/report/v1`) is present and a good signal.
- **Risk + license breakdown on every group**, in every format, with depth control. This
  was the headline ask and it lands.
- **Byte-stable output** (sorted maps, pre-sorted slices) — important for diffing audit
  runs in CI and for caching.

### Still weak / confusing / suspect data
- **No problems-only filter (item 7).** Biggest usability gap for both audiences.
- **Group risk is local-only and can mislead.** With `policy.allow: [MIT]` and an AGPL↔Apache
  hard-incompatibility flagged at repo level, the `src/util` (Apache) group still prints
  `[risk: low]`. A reader scanning groups sees "low" next to a license that is part of a
  repo-level hard conflict. Group risk should reflect policy violations attributed to that
  group, not just the intrinsic license category.
- **`unknown` / missing-header rendered as `[risk: none]`.** Files with no header are an
  audit *liability* (unknown obligations), but the category group prints `risk: none`. "none"
  reads as "nothing to worry about." `unknown` would be more honest than `none`.
- **`.license-tool.yaml` counted as a missing-header "source file."** The config file itself
  lands in `sourceMissing` and generates a `missing-header` violation. That inflates
  `sourceTotal`/`sourceMissing` (9/2 where a human would say 8/1) and produces a violation
  for the tool's own config. The tool should exclude its own config (and arguably data files
  like YAML/JSON) from header expectations by default.
- **`none` vs `(no-header)` token drift** in the findings line (item 9 residual).
- **`--summary` JSON can't also carry detail (item 10).** No way to get findings + machine
  file records in one trimmed response.
- **Dependency resolution returned 0** on a repo with a `go.mod require` — could not verify
  the dep path end-to-end; may be environment-specific, but worth a smoke test in CI with a
  populated module cache.

### New problems / regressions
- No outright regression observed; the full (non-summary) JSON shape is preserved as
  intended. The new concerns above (config-as-source-file, local-only group risk,
  `risk: none` for unknowns) are pre-existing semantics now made more visible by the
  richer output, not breakage introduced by the grouping work.

---

## Prioritized next steps

Quick wins
1. **Add `--only missing,copyleft,violations` (item 7).** Highest value/effort ratio; makes
   the report actionable for large trees and cheap for LLMs to consume.
2. **Exclude the tool's own `.license-tool.yaml` (and ideally data files) from header
   expectations.** Stops the tool flagging its own config and fixes inflated source counts.
3. **Unify the missing-header token to `(no-header)` in the `findings` line** (drop `none`).
   One-line consistency fix.
4. **Render unknown/missing-header group risk as `unknown`, not `none`.** Avoids a false
   "all clear" read.

Bigger bets
5. **Make group risk policy-aware.** Roll attributed policy violations (allow-list, hard
   incompatibilities) into each group's risk so a group containing a conflicting license
   doesn't read `low`. Most likely to change an engineer's triage decision.
6. **Add `--group-by dependency` (item 8) and verify the resolver in CI.** Dependencies are
   half the license-risk surface; they should be groupable by license/ecosystem like source.
7. **Reconsider JSON `--summary` (item 10):** either always emit `files`/`dependencies` for
   `--format json` (treat `--summary` as human-only), or add `--fields`/`--include-files` so a
   machine can request findings + detail in one call.
