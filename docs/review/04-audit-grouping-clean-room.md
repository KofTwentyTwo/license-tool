# Clean-room review: `audit --summary` and `--group-by`

Reviewer: independent, no prior context. Evaluated `feature/GH-35-audit-summary-reports`
built to `/tmp/cr-lt` from `cmd/license-tool`. Exercised against this repo, a hand-built
mixed-license repo (`/tmp/cr-mix`: MIT/Apache-2.0/AGPL/BSD + unlicensed `vendor/` + a
markdown file), and a 96-file tree (`/tmp/cr-big`). All output below is pasted from real
runs.

Lens: a compliance engineer scanning a repo, and an LLM/dashboard consuming the JSON.

---

## TL;DR verdict

The two features work and compose cleanly across all three formats, output is
deterministic, and the code is well-factored (`group.go` is pure, `findings.go` is pure).
But the views are **shallow**: they answer "how many files have license X" and stop. They
do not answer the questions a compliance engineer actually asks — *what's my coverage %,
where is my copyleft exposure, which group is the problem, why did policy fail*. The two
biggest gaps are:

1. **The grouped/summary views carry no risk signal.** A group is just `key (count)`.
   There is no per-group license/category, no copyleft flag, no "this group is the
   violation." You can group by `directory` but each group then hides which licenses live
   in it — the one cross-tab a human wants (license × directory) is impossible.
2. **`policy-violation` is opaque everywhere.** The top-level verdict says
   `FAIL (1: policy-violation)` and the JSON emits `"violations": ["policy-violation"]`
   with zero detail about *which license* tripped *which rule*. For an LLM this is nearly
   useless — it can see AGPL is present and that policy failed, but cannot state the causal
   link with confidence.

Plus a pile of quick wins: lexical directory sort (`pkg10` before `pkg2`), no totals/
percentages anywhere, the rich `findings:` block exists **only** in text (Markdown and JSON
don't get it), no "problems only" filter, and `(none)` is overloaded as both "no header"
and "empty section."

---

## What works well (credit where due)

- **Composition is clean.** `--summary` × `--group-by` × `--format` all combine sensibly.
  Summary+group gives per-group counts only; full+group nests files. No crashes, no weird
  interactions.
- **Determinism.** Keys and files are sorted; JSON map keys come out stable; empty
  aggregates render `{}`/`[]` not `null` (`nonNilCounts`/`nonNilStrings`). Good for diffing
  reports in CI.
- **The `findings:` text block is genuinely good** — coverage, license mix, unknown count,
  copyleft list, deps resolution, policy cause on one screen. The problem is only that it's
  trapped in the text renderer.
- **Skipped files are handled honestly** — never grouped, surfaced as a trailing
  `(skipped: N)` so they don't pollute the license buckets, and `(none)` is a deliberate
  sentinel (documented in code) so missing headers stay visible.
- **JSON `--summary` trims correctly and keeps a separate DTO** (`jsonSummaryReport`) so the
  full schema stays byte-stable. Schema is versioned (`license-tool/report/v1`).

---

## Data quality

### Right info present, but no totals or percentages — anywhere
`licenseCounts` sums to 9 but the tool never tells you that, nor "78% headered." A
compliance reader has to add the columns by hand.

```
$ /tmp/cr-lt audit . --deps=false --format json | jq 'has("totals"), (.licenseCounts|add)'
false
9
```

There is no `totals` / `coverage` object in JSON, and the count tables in text/markdown have
no total row and no percentage column.

### The verdict cause is invisible
This is the worst data-quality gap. The mix repo has AGPL files and fails policy:

```
$ /tmp/cr-lt audit . --deps=false
  policy: FAIL (1: policy-violation)
...
policy violations:
  policy-violation
```

```
$ /tmp/cr-lt audit . --deps=false --format json | jq '.violations'
[ "policy-violation" ]
```

`policy-violation` is a bare token. It does not name the offending license
(`AGPL-3.0-or-later`), the rule (deny? copyleft-not-allowed?), or the files. The *per-file*
`missing-header` tokens are present on file records, but the repo-level policy reason is a
black box. An LLM asked "why did this fail and what do I do" can only guess.

### Grouped views have no risk dimension
Every group is `key (count)`. When you group by `directory`, you lose all license
information for that group:

```
$ /tmp/cr-lt audit . --group-by directory --summary
source files by directory:
  scripts (1)
  src (4)
  vendor (2)
  web (2)
```

`src` holds both MIT (`src/core`) and Apache (`src/util`) — invisible here. `web` is all
AGPL (the risky one) — also invisible. The group that matters for compliance (where's the
copyleft?) is indistinguishable from the rest. There is no copyleft/risk flag per group, no
secondary breakdown.

### Directory grouping is top-segment only — collapses real structure
`groupKey`→`topDir` takes only the first path segment, so `src/core` and `src/util` both
become `src`. On a deep monorepo this collapses everything under a handful of top dirs and
the view is nearly content-free. There's no depth control (`--group-by directory:2`) and no
full-path option.

### No multi-dimension grouping, no filtering to problems
```
$ /tmp/cr-lt audit . --group-by license,directory
report: unknown group-by dimension "license,directory" ...
$ /tmp/cr-lt audit --help | grep -iE 'only|filter|problem'
(nothing)
```
You cannot cross-tab (license × directory is the killer compliance view), and you cannot say
"show me only the files that are missing headers / unknown / copyleft." On the 96-file tree
the full grouped view is 125 lines of mostly-fine files; the 24 problem files are buried.

### Dependencies are second-class
`--group-by` only ever touches **source files**. Dependencies — arguably the higher
compliance risk — cannot be grouped at all (by license, by ecosystem, by resolution).
`--summary` drops the dep list entirely, leaving only `dependencies: N (resolved R,
unresolved U)` in the text findings; the Markdown/JSON summary don't even carry that.

### `(none)` is overloaded
`(none)` is the missing-header license bucket **and** the empty-section placeholder
(`renderCountSection(..., "(none)")`). In `--group-by license` a real bucket `(none) (2)`
sits next to potential `(none)` placeholders elsewhere — ambiguous for both humans and
parsers. A distinct token like `(no-header)` for the bucket would disambiguate.

---

## Display / UX (text)

- **Lexical directory sort is wrong for humans.** On the big tree:
  ```
  source files by directory:
    pkg1 (8)
    pkg10 (8)
    pkg11 (8)
    pkg12 (8)
    pkg2 (8)
    ...
  ```
  `pkg10`–`pkg12` jump ahead of `pkg2`. Natural sort, or sort-by-count-desc, would read far
  better. There is **no sort option** at all — everything is alpha-by-key.
- **No sort-by-count.** The thing a human scans for is "which license/dir dominates" — that
  wants count-descending. Alpha order buries it.
- **Counts columns use a fixed `%-24s`** which is fine for short SPDX ids but `network-
  copyleft` and `JavaScript/TypeScript` nearly fill it; longer keys would misalign. Minor.
- **No color / no visual hierarchy.** Output is plain (no ANSI even to a TTY-ish pipe). For
  a compliance tool, flagging copyleft/violations in red and headings in bold would make the
  risk pop. At minimum the `(none)` and copyleft rows deserve a marker (`!`).
- **Large repos dump everything.** Full grouped view on 96 files = 125 lines. With no
  `--summary`-by-default-over-N and no problems filter, the signal-to-noise is low. The
  flat list under `--summary`-off is unbounded.
- **The findings block is the best part of the text output** and should be promoted/ported,
  not buried below the rollups.

---

## Machine / LLM usefulness (JSON)

The schema is clean and stable, which is the right foundation. Gaps that specifically hurt
LLM/dashboard use:

- **No totals/derived fields.** Every consumer re-derives `total`, `headered`, `coverage`,
  `copyleftCount`. The text renderer already computes all of this in `buildFindings` — it
  just isn't in the JSON. Ship a `findings`/`summary` object in JSON.
- **`violations` is a flat string array of opaque tokens.** `["policy-violation"]` cannot be
  reasoned over. An LLM can't tell the user *which* license or rule without inventing it.
  Make each violation an object: `{rule, severity, license, files:[...], message}`.
- **Risk flags absent.** There is `categoryCounts` with `network-copyleft: 2`, but no
  boolean/severity the consumer can branch on. A `riskLevel` per license or per group, or a
  top-level `hasCopyleft`/`worstCategory`, would let a dashboard color a badge without a
  hardcoded category list.
- **Groups carry no breakdown.** `groups[]` is `{key, count, files[]}`. For an LLM doing
  "summarize compliance by directory" the group needs its own `licenseCounts`/`categoryCounts`
  /`copyleft` so it doesn't have to re-aggregate `files[]` (and under `--summary`, `files`
  is gone, so re-aggregation is *impossible*).
- **Summary-trimming the JSON is the wrong default for machines.** Stripping `files`/
  `dependencies` under `--summary` is fine for text/markdown (human ergonomics), but a JSON
  consumer usually wants *complete* data and will do its own projection. Recommend: JSON
  ignores `--summary` for trimming (always complete), or gates trimming behind an explicit
  `--json-compact`. At minimum, when groups are summary-trimmed, still emit per-group
  aggregate counts so the data isn't lossy.
- Good: `omitempty` on optional file fields keeps records tight; `hasHeader` boolean is
  clean; resolution is a string enum.

---

## Markdown

- **Paste-into-PR quality is decent** — proper tables, code-fenced paths, per-group `###`
  headings, a `_Skipped (uneditable): N_` note. A reviewer can read it.
- **But Markdown is missing the findings block entirely.** The header jumps from
  `**Result:** FAIL` straight to `## By SPDX id`. None of coverage / copyleft / unknown /
  *why it failed* appears. The single most useful thing to paste into a PR ("this PR
  introduces AGPL, here's the exposure") is absent.
- **No summary/TL;DR up top, no totals in the tables.** A `> 78% headered, 2 copyleft files,
  policy FAIL` callout would make it a real report.
- Violations render as a bare bullet list of the same opaque tokens.

---

## Surprising / confusing / wrong

- **`policy: FAIL (1: policy-violation)` with no cause** — surprising for a compliance tool
  whose whole job is to explain exposure. (The default policy fails on the AGPL present in
  `web/js`, but you'd never know that from the output.)
- **`GPL-3.0-only` is a recognized SPDX id the tool refuses to render** a header for
  (`apply` errors: "recognized ... but license-tool cannot render it"). Limited template set;
  worth documenting which ids are renderable, since it surprised me mid-test.
- **`apply` refuses on a dirty git tree** (sensible) but this makes scripting a multi-license
  scaffold awkward — every subtree apply needs an intervening commit. Not an audit issue, but
  noted while building fixtures.
- **Directory grouping silently collapsing `src/core`+`src/util` into `src`** is easy to
  misread as "all of src is one license."

---

## Prioritized improvements

### Quick wins (small, high leverage)

1. **Expand `policy-violation` into a structured cause.** *Rationale: the verdict is the
   headline and it's currently unexplainable; biggest ROI for both humans and LLMs.*
   ```
   policy violations:
     copyleft-not-allowed: AGPL-3.0-or-later (2 files: web/js/app.js, web/js/lib.js)
   ```
   JSON: `"violations":[{"rule":"copyleft-not-allowed","license":"AGPL-3.0-or-later","severity":"high","files":["web/js/app.js","web/js/lib.js"]}]`

2. **Put `findings` in JSON and Markdown, not just text.** *Rationale: the best content
   already exists (`buildFindings`); it's a renderer gap, near-zero new logic.*
   JSON: `"findings":{"sourceTotal":9,"headered":7,"missing":2,"coverage":0.78,"copyleft":["AGPL-3.0-or-later"],"unknown":0}`

3. **Add totals + percentages to every count table.** *Rationale: stops every consumer
   re-summing; one line per table.*
   ```
   by category:
     network-copyleft   2  (22%)
     permissive         5  (56%)
     unknown            2  (22%)
     total              9
   ```

4. **Natural-sort (or offer `--sort count|name`) for groups/counts.** *Rationale: `pkg10`
   before `pkg2` is visibly wrong; count-desc surfaces the dominant license first.*

5. **Rename the missing-header bucket from `(none)` to `(no-header)`.** *Rationale: removes
   the overload with the empty-section placeholder; trivial.*

6. **Mark risk in text output** (e.g. trailing `  !copyleft` or a leading `*` on copyleft/
   none rows; respect `NO_COLOR`). *Rationale: a compliance reader should see risk without
   reading every line.*

### Bigger bets (more design, more value)

7. **Per-group breakdown / risk flag.** Every `Group` carries its own `licenseCounts`,
   `categoryCounts`, and a `copyleft`/`riskLevel`. *Rationale: makes `--group-by directory`
   actually useful (which dirs hold copyleft) and lets JSON groups stand alone under
   `--summary`.*
   ```
   source files by directory:
     web (2)   AGPL-3.0-or-later 2   [COPYLEFT]
     src (4)   MIT 2, Apache-2.0 2
   ```

8. **Multi-dimension / cross-tab grouping** (`--group-by directory,license`). *Rationale:
   "which directories introduce which licenses" is the central compliance question and is
   currently unanswerable.*

9. **A `--only` / `--filter` problems mode** (`--only missing,unknown,copyleft,violations`).
   *Rationale: on real repos the problem files are a tiny fraction; surfacing only them is
   what a reviewer and an LLM both want.*

10. **Group dependencies too** (`--group-deps-by license|ecosystem|resolution`), and keep
    a dep rollup in `--summary`. *Rationale: deps are often the bigger compliance risk and
    are currently entirely ungroupable and dropped from summaries.*

11. **Directory grouping depth control** (`--group-by directory:2` or full-path mode).
    *Rationale: top-segment-only collapses monorepos into meaningless buckets.*

12. **Decouple JSON from `--summary` trimming** (always-complete JSON, or `--json-compact`).
    *Rationale: machines want full data + their own projection; lossy summary JSON forces
    a second full run.*

### Specifically valuable for LLM consumption

- **Structured violations** (#1) — turns "it failed somehow" into an actionable,
  attributable fact the model can repeat verbatim.
- **A top-level `findings`/`totals` object** (#2, #3) — lets the model state coverage and
  risk without arithmetic it might get wrong.
- **Per-group aggregates that survive `--summary`** (#7, #12) — so the model can summarize a
  grouped report without the (now-stripped) file list.
- **An explicit `riskLevel`/`worstCategory` field** — so the model branches on a value
  instead of pattern-matching category strings against memorized copyleft knowledge.
