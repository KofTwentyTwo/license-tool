// Package render produces the canonical header text for a file and the top-level
// LICENSE / LICENSES/<id>.txt content, and performs the safety-critical byte-level
// file mutation that splices a header into (or out of) a source file.
//
// The header is rendered once as REUSE tags plus (optionally) the standard notice
// block from holder/year/style, then re-wrapped into the file's comment syntax
// (block or line) honoring its preserve-first ordering. The same package owns the
// mutation that inserts the rendered header at the correct offset when absent, or
// replaces a detected header span when relicensing.
//
// WHY mutation lives here, next to rendering: inserting and replacing a header is
// the inverse of rendering it, and both must agree byte-for-byte on the header
// region's shape, line endings, and the blank-line separator. Keeping them in one
// package removes the risk of the producer and the splicer drifting apart, which on
// a tool that rewrites source files is a correctness-and-safety hazard.
//
// SAFETY INVARIANTS (enforced by Splice/Insert/Replace and exercised by tests):
//
//   - Bytes outside the header region are never altered. A UTF-8 BOM, a shebang, an
//     XML/PHP prolog, an encoding pragma, the file's existing line endings (LF or
//     CRLF), and the presence/absence of a trailing newline all survive unchanged.
//   - The header is placed after preserve-first constructs the rule marks AFTER and
//     before constructs the rule marks BEFORE (a package/module declaration).
//   - Replacement uses the detected header's exact byte span; only that span is
//     swapped, so relicensing leaves no stacked or contradictory notice behind.
package render

import (
	"errors"
	"fmt"
	"strings"

	"github.com/KofTwentyTwo/license-tool/internal/gitutil"
	managedheader "github.com/KofTwentyTwo/license-tool/internal/header"
	"github.com/KofTwentyTwo/license-tool/internal/model"
)

// errNotImplemented is retained for the one strategy (git year) that depends on a
// frozen-but-stubbed sibling package; every other path is fully implemented.
var errNotImplemented = errors.New("render: not implemented")

// HeaderInput is everything needed to render one file's header.
type HeaderInput struct {
	// License is the resolved license (text + standard header + id + category).
	License model.License
	// Holder is the copyright holder text.
	Holder string
	// Year is the already-resolved year string (e.g. "2021-2026").
	Year string
	// Style selects which parts (REUSE tags, notice block) to emit.
	Style model.HeaderStyle
	// FileType supplies comment delimiters and preserve-first rules for wrapping.
	FileType model.FileType
}

// Header renders the comment-wrapped header block for a single file, including the
// trailing blank line separating it from file content but NOT the preserve-first
// prefixes (the applier/Splice splices the result in at the correct offset). The
// output uses LF line endings; the applier normalizes to the file's existing
// endings.
//
// The plaintext content is assembled per style:
//
//   - StyleReuse: the two REUSE tags only.
//   - StyleNotice: the license's StandardHeader only (when the license defines one).
//   - StyleReusePlusNotice: REUSE tags, a blank separator, then the StandardHeader.
//
// The sentinel is always embedded so subsequent runs can replace this block with
// the highest confidence. When a style asks for a notice the license does not
// define, the notice is silently omitted (the REUSE tags still carry the license
// identity), which keeps the renderer license-agnostic.
func Header(in HeaderInput) (string, error) {
	body := plaintextHeader(in)
	if body == "" {
		return "", fmt.Errorf("render: empty header for license %q style %q", in.License.SPDXID, in.Style)
	}
	if in.FileType.Skip {
		return "", fmt.Errorf("render: file type %q is uncommentable (skip)", in.FileType.Name)
	}
	wrapped := wrapComment(body, in.FileType.CommentStyle)
	// A single blank line separates the header from file content. The header text
	// ends without a trailing newline; the join below adds exactly one LF after the
	// comment block, then one blank line, then the splice point continues content.
	return wrapped + "\n\n", nil
}

// plaintextHeader assembles the un-wrapped header lines (sentinel + REUSE tags
// and/or notice) joined with LF, without a trailing newline.
func plaintextHeader(in HeaderInput) string {
	var lines []string

	// The sentinel rides as the first line so detection finds it before parsing any
	// tags. It is comment-wrapped along with everything else.
	lines = append(lines, "SPDX-License-Tool: "+managedheader.Sentinel)

	emitReuse := in.Style == model.StyleReuse || in.Style == model.StyleReusePlusNotice
	emitNotice := in.Style == model.StyleNotice || in.Style == model.StyleReusePlusNotice

	if emitReuse {
		lines = append(lines, reuseTagLines(in.License, in.Holder, in.Year)...)
	}

	if emitNotice {
		notice := strings.TrimRight(in.License.StandardHeader, "\n")
		if notice != "" {
			if emitReuse {
				lines = append(lines, "")
			}
			// The copyright attribution leads the notice block so a reader sees who
			// holds copyright before the grant text. Empty holder/year are omitted to
			// avoid a dangling "Copyright" line.
			if attribution := copyrightLine(in.Holder, in.Year); attribution != "" {
				lines = append(lines, attribution, "")
			}
			lines = append(lines, strings.Split(notice, "\n")...)
		}
	}

	// Only the sentinel with nothing substantive (no REUSE tags and no notice) means
	// the requested style produced no usable header, e.g. notice-only style on a
	// license that defines no standard header. Signal emptiness so Header errors
	// rather than writing a meaningless sentinel-only comment.
	if len(lines) <= 1 {
		return ""
	}
	return strings.Join(lines, "\n")
}

// copyrightLine builds a "Copyright (c) <year> <holder>" attribution line, omitting
// absent parts. Returns empty when both year and holder are empty.
func copyrightLine(holder, year string) string {
	parts := []string{"Copyright (c)"}
	if year != "" {
		parts = append(parts, year)
	}
	if holder != "" {
		parts = append(parts, holder)
	}
	if len(parts) == 1 {
		return ""
	}
	return strings.Join(parts, " ")
}

// reuseTagLines returns the two canonical REUSE tag lines.
func reuseTagLines(license model.License, holder, year string) []string {
	copyright := strings.TrimSpace(strings.TrimSpace(year) + " " + strings.TrimSpace(holder))
	return []string{
		"SPDX-FileCopyrightText: " + copyright,
		"SPDX-License-Identifier: " + license.SPDXID,
	}
}

// REUSETags renders just the REUSE tag lines (SPDX-FileCopyrightText and
// SPDX-License-Identifier) as plain text, before comment wrapping. The two lines
// are LF-joined without a trailing newline.
func REUSETags(license model.License, holder, year string) string {
	return strings.Join(reuseTagLines(license, holder, year), "\n")
}

// wrapComment wraps the LF-joined plaintext body in cs's comment syntax. Block
// styles emit the opener on its own line, each body line, then the closer on its
// own line. Line styles prefix every body line; a blank body line gets the prefix
// trimmed of trailing whitespace so we never emit trailing spaces. The returned
// string has no trailing newline (the caller adds the separator).
func wrapComment(body string, cs model.CommentStyle) string {
	bodyLines := strings.Split(body, "\n")
	var out []string
	if cs.Block {
		out = append(out, cs.Open)
		for _, l := range bodyLines {
			// One leading space indents the body under the block opener for the common
			// "/*\n  text\n*/" shape; a blank line stays blank (no trailing space).
			if l == "" {
				out = append(out, "")
			} else {
				out = append(out, "  "+l)
			}
		}
		out = append(out, cs.Close)
	} else {
		prefix := cs.LinePrefix
		trimmedPrefix := strings.TrimRight(prefix, " \t")
		for _, l := range bodyLines {
			if l == "" {
				out = append(out, trimmedPrefix)
			} else {
				out = append(out, prefix+l)
			}
		}
	}
	return strings.Join(out, "\n")
}

// LicenseFile renders the full top-level LICENSE file body (the verbatim license
// text) for the given license. The text is returned exactly as vendored, with a
// single trailing newline so the file is POSIX-clean.
func LicenseFile(license model.License) (string, error) {
	if license.Text == "" {
		return "", fmt.Errorf("render: no vendored text for license %q", license.SPDXID)
	}
	return ensureTrailingNewline(license.Text), nil
}

// LicensesEntry renders the LICENSES/<id>.txt body for the REUSE layout (the
// verbatim license text under its SPDX id). It is byte-identical to LicenseFile;
// REUSE simply stores the same verbatim text under a per-id path.
func LicensesEntry(license model.License) (string, error) {
	return LicenseFile(license)
}

// ensureTrailingNewline returns s with exactly one trailing LF.
func ensureTrailingNewline(s string) string {
	return strings.TrimRight(s, "\n") + "\n"
}

// Mutation surface ------------------------------------------------------------
//
// Splice produces the new bytes for a file. It is the single safety-critical entry
// point the applier drives; Insert and Replace are exposed for callers that already
// know which operation applies. All three preserve a BOM, the file's line endings,
// and its trailing-newline state exactly, and never touch bytes outside the header
// region.

// Splice computes the new content for one file. When detected.Present is true it
// REPLACES the detected header span (relicense / refresh); otherwise it INSERTS the
// rendered header at the correct offset after preserve-first prefixes. headerLF is
// the LF-normalized header produced by Header. The returned action is "replace",
// "insert", or "none" (when the existing bytes already match the desired header).
func Splice(content []byte, ft model.FileType, headerLF string, detected model.DetectedHeader) (newContent []byte, action string) {
	eol := detectEOL(content)
	header := normalizeEOL(headerLF, eol)

	if detected.Present {
		return replaceSpan(content, header, detected.StartByte, detected.EndByte)
	}
	return insertAt(content, ft, header, eol)
}

// Insert places header at the correct offset after preserve-first prefixes in a
// file that has no managed header. It is Splice's absent-header path, exposed for
// direct use.
func Insert(content []byte, ft model.FileType, headerLF string) (newContent []byte, action string) {
	eol := detectEOL(content)
	return insertAt(content, ft, normalizeEOL(headerLF, eol), eol)
}

// Replace swaps the byte span [start,end) for header (relicense / refresh), exposed
// for direct use. start/end come from a DetectedHeader.
func Replace(content []byte, headerLF string, start, end int) (newContent []byte, action string) {
	eol := detectEOL(content)
	return replaceSpan(content, normalizeEOL(headerLF, eol), start, end)
}

// insertAt computes the preserve-first boundary, then inserts header (already
// EOL-normalized) there. The header string already ends with a blank-line
// separator; we re-normalize that separator to eol so an inserted header on a CRLF
// file does not introduce a lone LF.
func insertAt(content []byte, ft model.FileType, header string, eol string) ([]byte, string) {
	at := managedheader.PreserveBoundary(content, ft)
	prefix := content[:at]
	rest := content[at:]

	// If everything before the insertion point ends without a newline (e.g. a
	// shebang with no trailing newline, or a BOM-only prefix), the header would be
	// glued onto that line. Guard by ensuring the prefix ends with an EOL when it is
	// non-empty and not already newline-terminated.
	var b []byte
	b = append(b, prefix...)
	if len(prefix) > 0 && !endsWithNewline(prefix) {
		b = append(b, eol...)
	}
	b = append(b, []byte(header)...)
	b = append(b, rest...)
	return b, "insert"
}

// replaceSpan swaps content[start:end) for header. The detected span's trailing
// blank-line separator (if the detector included it) is part of [start,end); the
// rendered header carries its own separator, so the result keeps exactly one.
//
// Guards: an out-of-range span is treated as a no-op insert refusal — we never
// slice outside the buffer. When the existing span already equals the new header,
// the action is "none" so apply is idempotent and the diff is empty.
func replaceSpan(content []byte, header string, start, end int) ([]byte, string) {
	if start < 0 || end > len(content) || start > end {
		// Refuse to mutate on a malformed span rather than risk corruption.
		return append([]byte(nil), content...), "none"
	}
	if string(content[start:end]) == header {
		return append([]byte(nil), content...), "none"
	}
	var b []byte
	b = append(b, content[:start]...)
	b = append(b, []byte(header)...)
	b = append(b, content[end:]...)
	return b, "replace"
}

// EOL handling ----------------------------------------------------------------

// detectEOL returns the dominant line ending of content: "\r\n" if the first line
// break is a CRLF, else "\n". Empty or newline-free content defaults to LF.
//
// WHY first-break wins rather than a majority vote: we are reinserting a header at
// the top of the file, and matching the top-of-file convention is what keeps the
// edit invisible to a CRLF-configured editor and to git's autocrlf.
func detectEOL(content []byte) string {
	for i := 0; i < len(content); i++ {
		if content[i] == '\n' {
			if i > 0 && content[i-1] == '\r' {
				return "\r\n"
			}
			return "\n"
		}
	}
	return "\n"
}

// normalizeEOL rewrites every LF in s (which is produced with LF endings) to eol.
// s must not already contain CR characters; Header guarantees LF-only output.
func normalizeEOL(s, eol string) string {
	if eol == "\n" {
		return s
	}
	return strings.ReplaceAll(s, "\n", eol)
}

// endsWithNewline reports whether b ends with an LF (covers CRLF too, since CRLF
// ends in LF).
func endsWithNewline(b []byte) bool {
	return len(b) > 0 && b[len(b)-1] == '\n'
}

// Year resolution -------------------------------------------------------------

// NewYearResolver returns a model.YearResolver for spec. For YearGit it consults
// git history (via internal/gitutil) for the first-commit year; for the explicit
// kinds it formats the stored values; for current it uses the injected nowYear.
func NewYearResolver(spec model.YearSpec) model.YearResolver {
	return yearResolver{spec: spec}
}

// yearResolver is the concrete model.YearResolver returned by NewYearResolver.
type yearResolver struct {
	spec model.YearSpec
}

// Resolve implements model.YearResolver.
//
//   - YearCurrent: the injected nowYear, as "YYYY".
//   - YearExplicit: the stored Start, as "YYYY".
//   - YearRange: "Start-End" (collapsed to a single year when Start == End).
//   - YearGit: "<first-commit-year>-<nowYear>" from gitutil.FirstCommitYear,
//     collapsed to a single year when the first commit is the current year.
//
// WHY nowYear is injected: "current" must be deterministic in tests and stable
// within a single run, so the caller owns the clock rather than this package.
func (r yearResolver) Resolve(repoPath string, nowYear int) (string, error) {
	switch r.spec.Kind {
	case model.YearCurrent:
		return fmt.Sprintf("%d", nowYear), nil
	case model.YearExplicit:
		return fmt.Sprintf("%d", r.spec.Start), nil
	case model.YearRange:
		return formatRange(r.spec.Start, r.spec.End), nil
	case model.YearGit:
		first, err := gitutil.FirstCommitYear(repoPath)
		if err != nil {
			return "", fmt.Errorf("render: resolve git year: %w", err)
		}
		return formatRange(first, nowYear), nil
	default:
		return "", errNotImplemented
	}
}

// formatRange renders a start-end year range, collapsing equal or inverted bounds
// to a single year so we never emit "2026-2026" or a backwards range.
func formatRange(start, end int) string {
	if end <= start {
		return fmt.Sprintf("%d", start)
	}
	return fmt.Sprintf("%d-%d", start, end)
}
