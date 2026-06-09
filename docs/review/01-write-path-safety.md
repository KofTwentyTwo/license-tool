# Write-Path Safety Review — license-tool

Scope: the safety-critical header rewrite path.
`internal/detect`, `internal/render`, `internal/header`, `internal/filetype`,
`internal/applier`, `internal/gitutil` (plus their tests).

Reviewer focus: any path where `apply` could mutate or delete non-header
content; LF/CRLF and BOM handling; off-by-one in byte spans; detection
false positives/negatives; non-idempotent (stacking) paths; temp-then-rename
crash safety; symlink/encoding edge cases. Verdict on each stated SAFETY
INVARIANT in `render.go`.

## Overall assessment

The architecture is sound and unusually careful for this class of tool: a
single byte-splice entry point (`render.Splice`), span-exact replacement,
EOL/ BOM/trailing-newline preservation, atomic temp-then-rename, a dirty-tree
gate, and symlinks excluded upstream in `enumerate`. The stated render
invariants mostly hold. The real exposure is concentrated in **detection**,
where two behaviors can cause `apply` to absorb and delete a non-license
leading comment that merely sits next to (or mentions) a license signal. Those
are the findings to fix before trusting `--write` on arbitrary repos.

No crash-corruption or partial-write bug was found; the atomic write is
correct. No panic path was found (out-of-range spans are refused).

## Severity-sorted summary

| # | Severity | Area | Finding |
|---|----------|------|---------|
| 1 | High | detect | A leading non-license comment sitting one blank line below a managed header is absorbed into the header span and DELETED on replace (line-comment types). |
| 2 | Medium | detect | `SPDX-License-Identifier:` matched anywhere in a leading comment marks it managed — a doc comment that *mentions* the tag is a false positive and gets replaced. |
| 3 | Medium | applier | `AtomicWrite` on a symlinked managed file (`LICENSE`) follows it for mode but `os.Rename` replaces the symlink with a regular file. Source files are safe (excluded in enumerate); `LICENSE`/`LICENSES/*` are not. |
| 4 | Low | render | `normalizeEOL`'s "input is LF-only" invariant is asserted, not enforced; a CR inside a vendored `StandardHeader` would yield `\r\r\n` on CRLF files. |
| 5 | Low | detect | `extractYear` fallback scans the whole comment, so a year inside license-body prose can be reported as the copyright year for fingerprint-only matches. |
| 6 | Low | detect | Block-comment detection spans only the first `/* */`; a header authored as two adjacent blocks is partially replaced (cosmetic stacking, not data loss). |
| 7 | Info | gitutil | `ls-files` output is newline-split; paths with embedded newlines (core.quotepath off + literal newline) are mishandled. Extremely rare; documented for completeness. |

---

## Finding 1 — Adjacent leading comment absorbed and deleted on replace (High)

`internal/detect/detect.go:290` (`lineCommentRegion`) and `:238`
(`leadingCommentRegion` → `consumeTrailingBlankLines`).

`lineCommentRegion` tolerates blank lines *between* comment lines and folds
them into one region:

```go
case strings.TrimSpace(body) == "":
    // Blank line: tolerate it only between comment lines...
    pendingBlank++
...
case strings.HasPrefix(trimmed, prefix):
    for i := 0; i < pendingBlank; i++ { lines = append(lines, "") }
    pendingBlank = 0
    ...
    end = lineEnd
```

This is intentional for a header split by a blank line (test at
`detect_test.go:554`, "blank line then comment is still one region"). But it
does not distinguish a *continuation of the header* from an *unrelated leading
doc comment*. Consider a Rust/Go-`//` or Python-`#` file:

```
// SPDX-License-Identifier: MIT
//
// Some hand-written file-level doc comment that is NOT part of the license.

func ...
```

or worse, two visually separate blocks:

```
// SPDX-License-Identifier: MIT

// Package foo does X. Authored by the team; please keep this.
package foo
```

Both comment blocks plus the blank line between them become a single detected
span (`StartByte..EndByte`). `render.replaceSpan` then swaps that **entire**
span for the new header, so on a relicense/refresh the second, non-license
comment is silently deleted. This is exactly the "false positive deletes a real
doc comment" hazard the package doc warns against — here triggered not by a
prose license-name but by adjacency to a genuine header.

Failure scenario: any repo whose convention is "license header, blank line,
file doc comment" loses the doc comment on the first `apply --write` that
performs a replace (e.g. a year refresh or relicense).

Fix options:
- Bound the header region to the *contiguous* comment run (stop at the first
  blank line), and only fold an interior blank line when the following comment
  also carries a license signal; or
- When replacing, recompute the minimal span that actually contains the
  license signal (sentinel/tag/fingerprint lines) rather than the whole
  tolerated run, so trailing non-license comment lines are preserved.

The block-comment path is not affected by the blank-line tolerance (it spans a
single `/* */`), so this is specific to line-comment types — which is most of
the table (Rust, Python, Shell, Ruby, SQL, YAML, etc.).

## Finding 2 — `SPDX-License-Identifier:` matched anywhere marks a comment managed (Medium)

`internal/detect/detect.go:130` and `:339` (`spdxIdentifierTag`).

```go
const tag = "spdx-license-identifier:"
lower := strings.ToLower(commentText)
idx := strings.Index(lower, tag)
```

Any leading comment whose text *contains* the tag substring — anywhere, not
only as a line-leading REUSE tag — sets `Present = true`. A file that documents
SPDX usage, a code comment explaining the convention, a template/example, or a
generated doc comment that quotes `SPDX-License-Identifier: <id>` is treated as
a managed header and its whole comment block is replaced on `apply`. This is
realistic inside a licensing tool's own repository, in docs-as-code, and in
test fixtures.

The package doc claims "mere mention of a license name in prose never
qualifies" — true for the fingerprint path, but the SPDX-tag path has no such
guard. Combined with Finding 1, a stray tag mention can also drag an adjacent
comment into the deleted span.

Fix: require the tag to be the start of a (decoration-stripped) comment line,
i.e. match `^\s*(\*\s*)?SPDX-License-Identifier:` per line rather than
`strings.Index` over the whole region. The detector already splits to lines for
`extractHolder`; do the same here.

Severity is Medium rather than High because a true REUSE tag in prose is
uncommon and the sentinel/standard-header paths are not affected.

## Finding 3 — AtomicWrite replaces a symlinked managed file (Medium)

`internal/applier/applier.go:283` (`AtomicWrite`).

```go
if fi, err := os.Stat(path); err == nil {  // follows symlink -> target's mode
    mode = fi.Mode().Perm()
}
tmp, _ := os.CreateTemp(dir, ".license-tool-*")
...
return os.Rename(tmpName, path)             // replaces the link, not the target
```

`os.Stat` follows a symlink, so the mode is read from the *target*; `os.Rename`
then drops the new file at the *link path*, replacing the symlink with a regular
file. For source files this never happens because `enumerate` skips symlinks
(`enumerate.go:158`, `:207`, with tests), so the source-rewrite path is safe.
But `writeManaged` (`applier.go:241`) calls `AtomicWrite` on
`<root>/LICENSE` and `<root>/LICENSES/<id>.txt` with no symlink check. A repo
that symlinks `LICENSE` to a shared file would have the link clobbered (and the
shared target left stale), or worse, if the temp lands in a different dir than
the link target, the link is silently converted.

Severity Medium: it does not corrupt content (the intended bytes are written),
but it can break an intentional symlink layout and is a surprise mutation of a
non-regular file.

Fix: in `AtomicWrite` (or `writeManaged`), `os.Lstat` the path and refuse /
warn when it is a symlink, mirroring the source-file policy; or resolve and
write through to the link target deliberately if that is the desired semantics.
Document whichever is chosen.

## Finding 4 — `normalizeEOL` LF-only precondition is unenforced (Low)

`internal/render/render.go:320` (`normalizeEOL`) and `:72` (`Header`).

```go
// normalizeEOL ... s must not already contain CR characters; Header guarantees LF-only output.
func normalizeEOL(s, eol string) string {
    if eol == "\n" { return s }
    return strings.ReplaceAll(s, "\n", eol)
}
```

`Header` builds the body from `in.License.StandardHeader` via
`strings.TrimRight(notice, "\n")` and `strings.Split(notice, "\n")` — it never
strips `\r`. If any vendored `StandardHeader` (or a future user-supplied
notice) contained CRLF, the CRs survive into the LF "canonical" header, and on
a CRLF file `normalizeEOL` turns each `\n` into `\r\n`, producing `\r\r\n`.
That is a malformed line ending spliced into the user's file.

Today the SPDX snapshot is almost certainly LF-only, so this is latent, hence
Low. But the safety story rests on an asserted invariant rather than an
enforced one.

Fix: normalize the assembled body to LF in `Header`/`plaintextHeader`
(`strings.ReplaceAll(s, "\r\n", "\n")` then strip stray `\r`) before wrapping,
so the precondition `normalizeEOL` relies on is actually true regardless of
source data.

## Finding 5 — `extractYear` can report a license-body year as the copyright year (Low)

`internal/detect/detect.go:425` (`extractYear`).

```go
for _, line := range strings.Split(commentText, "\n") {
    if !strings.Contains(lower, "copyright") { continue }
    if y := findYearSpan(line); y != "" { return y }
}
// Fall back to any copyright-bearing fragment even if the keyword case differed.
if y := findYearSpan(commentText); y != "" { return y }   // scans EVERYTHING
```

The fallback scans the entire comment for the first 4-digit run. For a
fingerprint-only legacy header (no `Copyright` line, e.g. a bare GNU notice
that contains "version 3" / a URL / "1989, 1991"), the first 4-digit token in
the license boilerplate is returned as the holder's copyright year. This only
feeds the report's holder/year fields and the re-rendered header's year when
the policy is "explicit/derived", so it is not destructive, but it can write an
incorrect year. Low severity; worth tightening the fallback to require a
copyright-context token near the digits.

## Finding 6 — Block detection spans only the first comment block (Low / cosmetic)

`internal/detect/detect.go:259` (`blockCommentRegion`): "WHY only one block: a
license header is one comment block; a second block below is file content."
Reasonable, but a header authored as two adjacent `/* */` blocks (e.g. a
copyright block then a separate license block, a real legacy pattern in
C/Java) is only partially detected; replace swaps the first and leaves the
second, producing a stacked/contradictory notice. Not data loss, but it
violates the "no stacked notice" intent for that input shape. Low.

## Finding 7 — `git ls-files` newline split (Info)

`internal/gitutil/gitutil.go:51`: `strings.Split(out, "\n")`. With default git
quoting this is safe, but a tracked path containing a literal newline (possible
with `core.quotepath=off` and an exotic filename) would split into bogus paths.
Vanishingly rare; noted only for completeness. If hardening is desired, use
`ls-files -z` and split on NUL.

---

## SAFETY INVARIANT verification (render.go:17-25)

1. "Bytes outside the header region are never altered (BOM, shebang, XML/PHP
   prolog, pragma, EOL, trailing-newline survive)."
   **Upheld in render.** `insertAt`/`replaceSpan` only touch `[start,end)` or
   the computed insertion offset; BOM/CRLF/no-trailing-newline are preserved
   (verified by code trace and by `TestSplicePreservesBOM`,
   `TestSplicePreservesCRLF`, `TestSplicePreservesNoTrailingNewline`,
   `TestSpliceNeverAltersBytesOutsideHeaderRegion`). The caveat is upstream: if
   `detect` reports a span that *includes* a non-license comment (Findings 1/2),
   render faithfully deletes exactly that wrongly-sized span. The invariant
   holds for render given a correct span; the span can be wrong.

2. "Header placed after preserve-first / before package decl."
   **Upheld.** `PreserveBoundary` (header.go) is well-tested across BOM,
   shebang (universal), xml-decl, php-open, pragma, go:build, @charset,
   doctype, and the Before (package) rule is never consumed.

3. "Replacement uses the detected header's exact byte span; no stacking."
   **Upheld at the byte level** (single span swap, idempotent no-op when equal,
   out-of-range refused — `TestReplaceRejectsOutOfRangeSpan`,
   `TestSpliceIdempotent`). CRLF replace idempotency also holds: the detector
   absorbs the CRLF terminator and trailing blank line so the re-detected span
   equals the freshly normalized CRLF header (traced: `blockCommentRegion`
   handles `\r\n` at `:280`, `consumeTrailingBlankLines` folds the blank). The
   "no stacking" guarantee can still be defeated by Finding 6's two-block input.

## Test-quality notes

Tests are substantive, not coverage-padding: assertions check byte prefixes,
ordering (header-before-package, after-shebang), exact spans, idempotency
(byte-equality on second apply), and real `go/build` constraint evaluation
(`goFileMatches`) to prove a build tag is not voided. Error seams (`tmpWrite`,
`tmpClose`, `chmodFn`, `detectFn`, `preserveBoundaryFn`, `run`) are used to
drive otherwise-unreachable failure branches honestly. Atomic-write mode
preservation, dirty-tree gating, and per-file error isolation are all
exercised against real git repos.

Gaps that the 100%-line target does not catch (all behavioral, not line
coverage):
- No test inserts a license header *above an unrelated leading comment* and
  then relicenses, which would surface Finding 1. The existing
  "blank line then comment is still one region" test (`detect_test.go:554`)
  actually documents the buggy behavior as intended.
- No test for an `SPDX-License-Identifier:` string appearing in a non-header
  doc comment (Finding 2).
- No test for `AtomicWrite` onto a symlink (Finding 3).
- No CRLF in a vendored `StandardHeader` (Finding 4) — currently impossible to
  trigger from data, so untested by construction.
