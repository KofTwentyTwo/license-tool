# Code Review — Audit / Reporting Half (config, enumerate, spdx, policy, resolve, report)

Reviewer: automated deep review. Scope: correctness of policy verdicts, exit codes, config
layering, enumeration filtering, SPDX normalization, and deterministic output. Lead bias:
a *false pass* (something that should fail CI but does not) is the worst outcome here.

## Summary (severity-sorted)

| # | Severity | Area | File:Line | One-line |
|---|----------|------|-----------|----------|
| 1 | High | enumerate | enumerate.go:268-282 | `**` include globs silently never match (uses `filepath.Match`); excludes DO support `**`, so the two filters disagree |
| 2 | High | resolve | spdxnorm.go:89-98 | Alias table **guesses** a license version/clause it cannot know (`bsd`→BSD-2-Clause, `gnu lesser general public license`→LGPL-3.0-or-later), violating the documented "never guess" invariant |
| 3 | Medium | resolve | spdxnorm.go:95-97, 102 | Aliases resolve to EPL-2.0 / AGPL etc. that pass `spdx.Validate` but are NOT in the curated set, so `policy.Classify` returns Unknown — a resolved dep can carry a license the policy layer treats as unknown without anyone noticing |
| 4 | Medium | resolve | gradle.go:51, 94-109 | Gradle dep regex requires a literal `group:artifact:version`; versionless / BOM-managed coordinates are silently dropped (not reported unresolved), so a Gradle dependency can be invisible to the audit |
| 5 | Medium | report | report.go:378-387 | `Check` only ever returns 0/1/4; the unresolved-dependency gate works only if `unresolved-dependency` is in `fail_on`, and the default `fail_on` set omits it — unresolved deps are a default *pass* |
| 6 | Low | resolve | npm.go:56-84 | Only **direct** declared deps are resolved; transitive deps installed under nested `node_modules` are never audited (documented, but a real coverage gap for a CI gate) |
| 7 | Low | config | config.go:456-461 | Flag excludes append while flag includes replace — correct per spec, but there is no test that a flag exclude *adds to* (rather than replaces) repo/user excludes through the flag layer specifically |
| 8 | Low | enumerate | enumerate.go:328-334 | Non-git walk reads only the **root** `.gitignore`; nested/global ignores are not honored (documented), so the git and non-git paths can enumerate different sets |
| 9 | Info | policy | policy.go:277-286 | Incompatibility table omits AGPL/GPL-3 vs GPL-2-only-family pairs that the prose claims to cover; heterogeneity still flags them, so no false pass, but the curated table is narrower than its doc comment |

No critical (provably wrong hard-fail/hard-pass with default config) issue was found in the
policy verdict logic itself; the policy package is solid. The dangerous items are #2 (a guessed
license can launder a copyleft dep into a wrong-but-resolved verdict) and #1 (include scoping
silently mis-filters).

---

## Details

### 1. (High) `**` include globs silently match nothing — include/exclude filters disagree

`internal/enumerate/enumerate.go:268-282`:

```go
func matchesIncludes(rel string, includes []string) bool {
	if len(includes) == 0 {
		return true
	}
	base := filepath.Base(rel)
	for _, pat := range includes {
		if ok, _ := filepath.Match(pat, rel); ok {
			return true
		}
		if ok, _ := filepath.Match(pat, base); ok {
			return true
		}
	}
	return false
}
```

Includes are matched with the standard library `filepath.Match`, which has **no `**`
support**: `**` collapses to a single `*`, and `*` never matches a path separator. So an
include pattern of `src/**` matches `src/x.go` but **not** `src/sub/x.go`, and `**/generated/**`
matches nothing at any depth. The config layer explicitly accepts and round-trips these
patterns — `internal/config/config_test.go:173-175,535-543` use `src/**`, `**/generated/**`,
`repo/**`, `flags/**` as first-class include values — so a user who writes the documented
recursive include will silently get an empty or wrong file set.

The asymmetry is the real trap: **excludes** are compiled with `go-gitignore`
(`enumerate.go:116-117`, `ignore.CompileIgnoreLines`), which *does* implement `**`. So
`exclude: ["**/vendor/**"]` works as written while `include: ["src/**"]` does not. Two filters
on the same config, one honoring `**` and one not.

The test `TestMatchesIncludes` (enumerate_test.go:51-71) only exercises `*.go` and `src/*.go`
and even *asserts* the non-spanning behavior (`"full-path glob does not span dirs" ... false`) —
so this is locked-in behavior, not an oversight in coverage. That makes it coverage-padding
relative to the actual user contract: 100% lines, zero `**` include cases.

Fix: match includes with the same `go-gitignore`/doublestar engine used for excludes (or
`github.com/bmatcuk/doublestar`), so include and exclude glob semantics are identical. At
minimum, document loudly that includes are shell-glob (single-segment) not gitignore-glob, and
reject/normalize `**` in include patterns at config load so the user gets an error instead of a
silent empty set.

---

### 2. (High) Alias table guesses a license it cannot actually determine — "never guess" violated

`internal/resolve/spdxnorm.go:81-103`:

```go
var spdxAliases = map[string]string{
	...
	"bsd":                                "BSD-2-Clause",
	"bsd license":                        "BSD-2-Clause",
	...
	"gnu lesser general public license":  "LGPL-3.0-or-later",
	...
}
```

The package contract (resolve.go:1-15, spdxnorm.go:11-20) is emphatic that the tool **never
guesses** a license. These two entries do exactly that:

- `"bsd"` / `"bsd license"` → `BSD-2-Clause`. The bare string "BSD" does not state the clause
  count. It could be 2-clause, 3-clause, or 4-clause. Picking BSD-2-Clause is a guess, and it
  can be the *wrong* license for policy purposes (a 4-clause/advertising-clause BSD is a
  materially different obligation).
- `"gnu lesser general public license"` → `LGPL-3.0-or-later`. The name carries **no version**.
  SPDX has `LGPL-2.0-*`, `LGPL-2.1-*`, `LGPL-3.0-*` (both `-only` and `-or-later`). Mapping the
  versionless name to `3.0-or-later` invents both a version and the "or-later" grant. That is a
  guess with legal weight: it can turn an LGPL-2.1-only dependency into a reported
  LGPL-3.0-or-later, and `-or-later` vs `-only` is exactly the distinction the incompatibility
  table (policy.go:277-286) cares about.

Because `EvaluateDependency` checks the resolved id against deny/allow, a wrong guess here can
produce a *wrong policy verdict* (pass a denied license, or fail an allowed one). Contrast the
conservative entries in the same table (`apache 2.0`→Apache-2.0) which are genuinely
unambiguous and SPDX-documented equivalences — those are fine.

Fix: delete the two ambiguous aliases. A versionless/clauseless name must stay **unresolved**
with a reason ("declared license %q is ambiguous (no version/clause); not guessed"), which is
the honest answer the package promises everywhere else.

---

### 3. (Medium) Aliases can resolve to a valid-but-uncurated id that policy then can't classify

`spdxnorm.go:95-97,102` map to `EPL-2.0`, `EPL-1.0`, `AGPL-3.0-or-later`, etc.
`internal/spdx/data/licenses/` ships only 14 curated detail files (no `EPL-*.json`), while the
full index used by `Validate` contains EPL. So for an EPL dependency:

- `normalizeSPDX("Eclipse Public License 2.0")` → `EPL-2.0`, `spdx.Validate("EPL-2.0")` is true →
  the dep is marked **Resolved** with id `EPL-2.0`.
- But `policy.Classify("EPL-2.0")` calls `spdx.Lookup`, which fails (uncurated) →
  `CategoryUnknown`.

`EvaluateDependency` (policy.go:109-152) only consults deny/allow for resolved deps, and
unknown-category does not feed any dependency-level signal, so an EPL dep with no allow-list
slips through as a clean resolved pass even though the tool cannot actually categorize it.
This is internally consistent (deps aren't classified), but it means "resolved" can quietly mean
"valid SPDX string we can't reason about". Worth either (a) curating EPL so the alias targets are
all classifiable, or (b) noting in the report when a resolved id is outside the curated set.

---

### 4. (Medium) Gradle coordinates without a literal version are silently dropped

`internal/resolve/gradle.go:51`:

```go
var gradleDepRE = regexp.MustCompile(`["']([\w.\-]+:[\w.\-]+:[\w.\-${}]+)["']`)
```

The regex requires three colon-separated segments, i.e. `group:artifact:version`. Real Gradle
builds frequently declare dependencies **without** an inline version when the version comes from
a platform/BOM or version catalog: `implementation 'com.google.guava:guava'`,
`implementation libs.guava`, or `implementation(platform("..."))`. None of these match, so the
dependency is not even emitted as `Unresolved` — it is **invisible** to the audit.

The package's stated value (gradle.go:18-23) is to "surface that a Gradle project's dependency
licenses are unknown, not silently treat the project as license-free." A versionless dep is
silently dropped, partially defeating that. The comment at scanGradleManifest:99-101 ("always
yields at least three parts; no short-coordinate guard is needed") is true only because the regex
pre-filters to 3-part matches — but that pre-filter is itself the silent drop.

Fix: also match 2-segment coordinates (`group:artifact`) and emit them as Unresolved with
version empty and a reason. Not high severity because Gradle is detect-only anyway, but a
dropped dependency is a worse failure mode than a listed-unresolved one.

---

### 5. (Medium) Unresolved dependencies are a *pass* under the default policy

`internal/config/config.go:98-104` — `Defaults()` sets `FailOn` to
`{missing-header, unknown-license, policy-violation}` and deliberately omits
`unresolved-dependency`.

`report.Build` (report.go:158-162) correctly folds an `unresolved-dependency` condition for every
non-resolved dep, and `Check` (report.go:378-387) gates on `Report.Passed`. But `Passed`
(policy.go:315-326) only trips for conditions in `fail_on`. So with the default config, a repo
full of unresolved Gradle/Maven/npm dependencies **passes** `check` (exit 0).

This is arguably intentional (resolution is best-effort), and it is consistent across the code,
but it is a sharp edge for a tool teams "trust to block bad licenses": the most common real-world
gap (unresolvable third-party licenses) is off by default. No code bug, but flag it in docs and
consider whether `unresolved-dependency` belongs in the shipped default for `check`. There is no
test asserting that the default `fail_on` excludes it, so the default is also untested intent.

---

### 6. (Low) npm resolves only direct declared dependencies

`internal/resolve/npm.go:56-84` builds the dep set from the **root** package.json's
`dependencies`+`devDependencies` only (`declaredDependencyNames`). Transitive packages that exist
under `node_modules` (including nested `node_modules/<a>/node_modules/<b>`) are never enumerated.
For a license CI gate, transitive licenses are usually the *point*. This is documented
(npm.go:14-19 frames node_modules as authoritative for the *declared* set), but "authoritative
and complete" in the Resolve doc comment (lines 53-55) overstates it — it is complete for direct
deps only. Consider walking installed `node_modules/*/package.json` for full coverage, or soften
the comment.

---

### 7. (Low) Flag-exclude accumulation lacks a dedicated test

`config.go:456-461`: flag excludes append (`cfg.Excludes = append(cfg.Excludes, flags.Exclude...)`),
flag includes replace. This matches the documented contract (excludes accumulate, include is the
highest-precedence *replacing* layer). `TestResolveLayering` (config_test.go:514-522) does verify
the three-layer exclude *union* including the flag layer, so this is covered — downgrade to info.
The include replace-semantics are well tested (lines 534-561). No action required; listed for
completeness.

---

### 8. (Low) Non-git walk honors only the root `.gitignore`

`enumerate.go:328-334` compiles a single matcher from `<root>/.gitignore`. Nested `.gitignore`
files, the global excludes file, and `.git/info/exclude` are ignored on the walk path. This is
explicitly documented (enumerate.go:321-327) and the git path defers to `git ls-files` which gets
it right. The risk is only that auditing a non-git checkout yields a *different* (larger) file set
than the same tree in git. Acceptable for v1; documented.

---

### 9. (Info) Incompatibility table narrower than its doc comment

`policy.go:266-286`: the prose says strong/network copyleft "cannot be combined into a single
permissive redistribution" and that GPL-2.0-only has no upgrade path to the v3 family. The table
covers GPL-2.0-only × {Apache, GPL-3-or-later, AGPL-3, LGPL-3} and the copyleft×Apache pairs, but
omits e.g. AGPL-3 × GPL-3 or GPL-3 × LGPL-2.1. Because `EvaluateRepo` also runs the heterogeneity
check (distinct categories > 1), most such pairs still surface as a `policy-violation`, so there
is **no false pass**. This is only a doc/coverage mismatch in the curated table, not a verdict
bug. The function logic (symmetric, self-pair guard, empty guard) is correct.

---

## Things checked and found correct (no finding)

- **Config precedence** (flags > repo > user > defaults) is implemented correctly: lower layers
  seed `cfg`, each higher layer overrides only non-empty fields; `manage_license_file` uses
  `*bool` so explicit `false` is distinguishable from absent (config.go:370-372). `KnownFields(true)`
  rejects typo'd keys. Empty-document handling returns defaults (decodeYAML:330-342).
- **Excludes accumulate, includes replace** across layers — matches spec and is tested with `**`
  patterns at every layer.
- **Year/style parsing** is strict: 4-digit clamp rejects `21`/`20260`, range end<start rejected,
  unknown style/year/fail_on tokens are hard errors (no silent CI weakening).
- **Explicit `--config` missing = error; discovered missing = silent** (config.go:137-151). Correct.
- **SPDX two-surface split** (`Validate` permissive over full index, `Lookup` curated) is sound;
  `IDs()` correctly excludes deprecated ids from the picker; load is lazy/once/read-only so
  concurrent callers are safe. AGPL header override is verbatim-body-preserving.
- **Policy `Passed`** is order-independent and correctly returns true for empty `fail_on`; per-file
  and repo violations both feed the verdict via `report.Build`.
- **`EvaluateDependency`** correctly treats `resolved-but-no-id` as unresolved (policy.go:125-132)
  rather than passing — a good defensive guard against a resolver lying.
- **Determinism**: every renderer sorts via `sortedCounts`/`sortedFiles`/`sortedDeps`; JSON relies
  on encoding/json's sorted map keys plus pre-sorted slices; repo violation tokens are
  sorted+deduped. `Build` preserves input file order but counts are order-independent. Verified no
  map-iteration order leaks into output.
- **Enumerate skip ladder** (symlink → non-regular → unknown-type → uncommentable → binary) is in a
  defensible order; `IsBinary` tolerates a truncated trailing rune at the 8 KiB boundary; staged
  deletions (ls-files lists a path absent on disk) are skipped, not fatal.
- **`normalizeSPDX`** correctly rejects npm `SEE LICENSE`/`UNLICENSED` sentinels and compound
  expressions (one-id-per-dep invariant).

## Test-quality note

The policy, spdxnorm, and config layering tests are **substantive** (real layering with `**`
globs, stacked-violation ordering assertions, category-sort assertions, alias branch coverage).
The one coverage-padding pattern that masks a real bug is `TestMatchesIncludes`
(enumerate_test.go:51-71): it asserts the broken single-segment behavior as if it were the
contract, so 100% line coverage coexists with finding #1. Recommend adding `**` include cases —
they will fail today and should drive the fix.
