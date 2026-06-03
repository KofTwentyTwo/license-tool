// Package detect identifies an existing, managed license header in a source file's
// leading region. It steps past preserve-first prefixes (shebang, xml-decl, BOM,
// etc.), then recognizes a license header by, in order of confidence: our own
// sentinel, any SPDX-License-Identifier tag, a known SPDX standardLicenseHeader,
// or a curated phrase fingerprint (e.g. "GNU Affero General Public License").
//
// Anything ambiguous (a non-license leading doc comment) is reported as "no
// managed header" and never edited. The detection result carries the byte span of
// the header so apply can replace exactly that region.
//
// WHY this is safety-critical: a false positive here means apply deletes a normal
// doc comment thinking it is a license header. Every recognizer therefore demands a
// positive license signal inside the leading comment region; mere mention of a
// license name in prose never qualifies, and detection is confined to the contiguous
// comment block(s) at the top of the file (after preserve-first prefixes), so file
// content below the header is never scanned or matched.
package detect

import (
	"strings"

	"github.com/google/licensecheck"

	"github.com/KofTwentyTwo/license-tool/internal/model"
	"github.com/KofTwentyTwo/license-tool/internal/spdx"
)

// Sentinel is the marker license-tool writes into headers it manages, giving the
// highest-confidence signal that a leading comment is safe to replace.
const Sentinel = "license-tool:managed"

// fingerprintCoverPercent is the minimum licensecheck coverage of the comment
// region required to treat a phrase match as a license header. WHY high: the
// fingerprint path is the least-certain recognizer, so we demand that most of the
// comment's words be recognized license text rather than a stray matching phrase
// inside a doc comment.
const fingerprintCoverPercent = 75.0

// standardHeaderMatchTokens is the minimum number of consecutive significant words
// from a known SPDX standardLicenseHeader that must appear in the comment region for
// a standard-header match. WHY a run rather than a ratio: SPDX headers carry
// placeholder lines (e.g. "[name of copyright owner]") that legitimately differ, so
// we anchor on a long verbatim run of the immutable wording instead.
const standardHeaderMatchTokens = 8

// curatedStandardHeaderIDs is the curated rendering set whose model.License carries a
// usable per-file standardLicenseHeader. WHY enumerated here rather than queried: the
// frozen spdx package exposes Lookup but not a listing API, and this set is small and
// stable (it mirrors the curated rendering set in internal/spdx). Lookup gates each
// entry, so an id absent from the snapshot is simply skipped.
var curatedStandardHeaderIDs = []string{
	"AGPL-3.0-or-later", "AGPL-3.0-only", "GPL-3.0-or-later", "GPL-2.0-only",
	"LGPL-3.0-or-later", "Apache-2.0", "MPL-2.0",
}

// curatedFingerprints maps an immutable license phrase to its SPDX id. These cover
// legacy, non-SPDX-tagged headers (the bare GNU notice, the Apache "Licensed under"
// blurb) that predate this tool. WHY hand-curated in addition to licensecheck: these
// short, unambiguous phrases are cheap to match exactly and document precisely which
// legacy wordings we recognize; licensecheck is the broader backstop.
//
// Each phrase is matched case-insensitively against the whitespace-normalized comment
// text. Phrases are chosen to be present only in an actual license notice, never in
// ordinary prose that merely names a license.
var curatedFingerprints = []struct {
	phrase string
	spdxID string
}{
	{"gnu affero general public license as published by the free software foundation", "AGPL-3.0-or-later"},
	{"under the terms of the gnu affero general public license", "AGPL-3.0-or-later"},
	{"under the terms of the gnu general public license", "GPL-3.0-or-later"},
	{"under the terms of the gnu lesser general public license", "LGPL-3.0-or-later"},
	{"licensed under the apache license, version 2.0", "Apache-2.0"},
	{"mozilla public license, v. 2.0", "MPL-2.0"},
	{"permission is hereby granted, free of charge, to any person obtaining a copy", "MIT"},
	{"redistribution and use in source and binary forms, with or without", "BSD-3-Clause"},
	{"permission to use, copy, modify, and/or distribute this software for any", "ISC"},
}

// Detect scans content for a managed license header, given the file's FileType
// (for comment delimiters and preserve-first rules). The returned DetectedHeader
// reports presence, the byte span, the matched SPDX id / holder / year if any, and
// whether the match came via the sentinel.
//
// The span (StartByte inclusive, EndByte exclusive) covers exactly the leading
// comment block(s) recognized as a license header, including any blank lines between
// adjacent comment blocks but excluding the preserve-first prefixes and the file
// content that follows. When no header is found, Present is false and the offsets
// are zero.
func Detect(content []byte, ft model.FileType) (model.DetectedHeader, error) {
	// Uncommentable types (JSON and friends) can never carry a header; report absence
	// rather than risk matching incidental text.
	if ft.Skip {
		return model.DetectedHeader{}, nil
	}

	// Step past the preserve-first prefixes the header would sit AFTER. Detection of an
	// existing header begins here so a shebang or XML declaration is never mistaken for
	// (or absorbed into) the header span.
	boundary, err := PreserveBoundary(content, ft)
	if err != nil {
		return model.DetectedHeader{}, err
	}

	// Identify the contiguous leading comment region starting at the boundary. If there
	// is no comment there, there is no managed header.
	start, end, commentText, ok := leadingCommentRegion(content, boundary, ft)
	if !ok {
		return model.DetectedHeader{}, nil
	}

	header := model.DetectedHeader{StartByte: start, EndByte: end}

	// Recognizers in descending confidence. Each requires a positive license signal;
	// the first that fires wins and sets the header's identity fields.
	if strings.Contains(commentText, Sentinel) {
		header.Present = true
		header.ViaSentinel = true
	}

	if id, ok := spdxIdentifierTag(commentText); ok {
		header.Present = true
		if header.SPDXID == "" {
			header.SPDXID = id
		}
	}

	if !header.Present {
		if id, ok := matchStandardHeader(commentText); ok {
			header.Present = true
			header.SPDXID = id
		}
	}

	if !header.Present {
		if id, ok := FingerprintLicense(commentText); ok {
			header.Present = true
			header.SPDXID = id
		}
	}

	if !header.Present {
		// Not a license header: a foreign leading doc comment. Preserve and report.
		return model.DetectedHeader{}, nil
	}

	header.Holder = extractHolder(commentText)
	header.Year = extractYear(commentText)
	return header, nil
}

// PreserveBoundary returns the byte offset in content after all leading
// preserve-first prefixes that the header is placed AFTER (shebang, xml-decl,
// php-open, BOM, coding pragma). This is where an absent header is inserted (or
// just before a package declaration when the type lists one as Before).
//
// Only the prefixes the FileType actually lists are consumed, and only when they
// appear contiguously from the current offset. A BOM is consumed first (it is byte 0
// by definition); the remaining AFTER rules are matched line by line in file order
// regardless of the rule list's order, since a real file may carry, e.g., a shebang
// then a coding pragma. Rules marked Before (package declarations) are never consumed
// here: the header precedes them.
func PreserveBoundary(content []byte, ft model.FileType) (int, error) {
	pos := 0

	// A UTF-8 BOM, when allowed, is always the very first bytes (U+FEFF). Written as an
	// escape so the BOM byte sequence does not appear literally in this source file.
	const utf8BOM = "\uFEFF"
	if hasRule(ft, model.PreserveBOM, false) && strings.HasPrefix(string(content), utf8BOM) {
		pos += len(utf8BOM)
	}

	allowShebang := hasRule(ft, model.PreserveShebang, false)
	allowXML := hasRule(ft, model.PreserveXMLDecl, false)
	allowPHP := hasRule(ft, model.PreservePHPOpen, false)
	allowPragma := hasRule(ft, model.PreserveCodingPragma, false)

	// Consume contiguous preserve-AFTER lines in file order. We loop because several may
	// stack (shebang then coding pragma). We stop at the first line that is not a
	// consumable prefix; that is the header insertion point.
	for pos < len(content) {
		lineEnd := lineEndOffset(content, pos)
		lineText := string(content[pos:lineEnd])
		trimmed := strings.TrimRight(lineText, "\r\n")
		consumed := false

		switch {
		case allowShebang && strings.HasPrefix(trimmed, "#!"):
			consumed = true
		case allowXML && strings.HasPrefix(strings.TrimSpace(trimmed), "<?xml"):
			consumed = true
		case allowPHP && strings.HasPrefix(strings.TrimSpace(trimmed), "<?php"):
			consumed = true
		case allowPragma && isCodingPragma(trimmed):
			consumed = true
		}

		if !consumed {
			break
		}
		pos = lineEnd
	}

	return pos, nil
}

// FingerprintLicense attempts to map a block of header text to an SPDX id using the
// curated phrase fingerprints (for recognizing legacy, non-SPDX headers such as the
// bare GNU AGPL notice). Returns the id and true on a confident match.
//
// Two layers: an exact hand-curated phrase scan over whitespace-normalized text
// (cheap, documents the recognized legacy wordings), then a licensecheck.Scan
// backstop that only counts when it covers most of the text. WHY the coverage gate:
// licensecheck reports the matched fraction, and a high fraction means the text IS a
// license rather than prose that happens to contain a licensed phrase.
func FingerprintLicense(headerText string) (string, bool) {
	normalized := normalizeWhitespace(strings.ToLower(headerText))
	for _, fp := range curatedFingerprints {
		if strings.Contains(normalized, fp.phrase) {
			return fp.spdxID, true
		}
	}

	cov := licensecheck.Scan([]byte(headerText))
	if cov.Percent >= fingerprintCoverPercent && len(cov.Match) > 0 {
		return cov.Match[0].ID, true
	}
	return "", false
}

// leadingCommentRegion finds the contiguous comment block(s) at offset start and
// returns their byte span plus the extracted comment text (delimiters stripped,
// joined by newlines). For block-comment types it spans the first block comment; for
// line-comment types it spans the run of consecutive comment lines (blank lines
// between comment lines are included so a header split by a blank line is one region).
//
// The bool is false when there is no comment at start, in which case no header is
// possible. Leading blank lines before the comment are tolerated and included in the
// span so an inserted-then-detected header round-trips.
func leadingCommentRegion(content []byte, start int, ft model.FileType) (int, int, string, bool) {
	// Skip blank lines between the preserve boundary and the first comment.
	pos := start
	for pos < len(content) {
		lineEnd := lineEndOffset(content, pos)
		if strings.TrimSpace(string(content[pos:lineEnd])) != "" {
			break
		}
		pos = lineEnd
	}
	regionStart := pos

	var s, e int
	var text string
	var ok bool
	if ft.CommentStyle.Block {
		s, e, text, ok = blockCommentRegion(content, regionStart, ft.CommentStyle)
	} else {
		s, e, text, ok = lineCommentRegion(content, regionStart, ft.CommentStyle)
	}
	if !ok {
		return 0, 0, "", false
	}

	// Extend the span through any trailing blank lines so the blank separator between
	// the header and the file body belongs to the header region. WHY: the rendered
	// replacement header carries exactly one trailing blank line, so folding existing
	// blanks into the replaced span makes re-applying byte-idempotent (extra blanks
	// collapse) instead of accumulating a new blank line on every run.
	e = consumeTrailingBlankLines(content, e)
	return s, e, text, ok
}

// consumeTrailingBlankLines advances from pos over any run of blank (empty or
// whitespace-only) lines, returning the offset of the first non-blank line (or end
// of content). It never consumes a line with non-blank content.
func consumeTrailingBlankLines(content []byte, pos int) int {
	for pos < len(content) {
		lineEnd := lineEndOffset(content, pos)
		if strings.TrimSpace(string(content[pos:lineEnd])) != "" {
			return pos
		}
		pos = lineEnd
	}
	return pos
}

// blockCommentRegion spans a single leading block comment (Open ... Close) and
// returns its inner text. WHY only one block: a license header is one comment block;
// a second block below is file content and must not be absorbed.
func blockCommentRegion(content []byte, start int, cs model.CommentStyle) (int, int, string, bool) {
	rest := string(content[start:])
	trimmedLeading := strings.TrimLeft(rest, " \t")
	if !strings.HasPrefix(trimmedLeading, cs.Open) {
		return 0, 0, "", false
	}

	// Real start including any indentation we trimmed for the prefix test.
	openOffset := start + (len(rest) - len(trimmedLeading))
	afterOpen := openOffset + len(cs.Open)
	closeIdx := strings.Index(string(content[afterOpen:]), cs.Close)
	if closeIdx < 0 {
		// Unterminated block comment: not a recognizable header; do not guess a span.
		return 0, 0, "", false
	}

	inner := string(content[afterOpen : afterOpen+closeIdx])
	end := afterOpen + closeIdx + len(cs.Close)
	// Absorb a single trailing newline so the span ends cleanly at a line boundary.
	if end < len(content) && content[end] == '\n' {
		end++
	} else if end+1 < len(content) && content[end] == '\r' && content[end+1] == '\n' {
		end += 2
	}
	return openOffset, end, stripBlockInner(inner), true
}

// lineCommentRegion spans the run of consecutive line comments (each beginning with
// the LinePrefix, ignoring leading whitespace) starting at start, tolerating blank
// lines between comment lines, and returns the joined comment text with prefixes
// stripped. The run ends at the first non-blank, non-comment line.
func lineCommentRegion(content []byte, start int, cs model.CommentStyle) (int, int, string, bool) {
	prefix := strings.TrimRight(cs.LinePrefix, " \t")
	if prefix == "" {
		prefix = strings.TrimSpace(cs.LinePrefix)
	}

	pos := start
	end := start
	var lines []string
	pendingBlank := 0
	sawComment := false

	for pos < len(content) {
		lineEnd := lineEndOffset(content, pos)
		raw := string(content[pos:lineEnd])
		body := strings.TrimRight(raw, "\r\n")
		trimmed := strings.TrimLeft(body, " \t")

		switch {
		case strings.HasPrefix(trimmed, prefix):
			// A comment line: flush any tolerated blank lines into the region, then record.
			for i := 0; i < pendingBlank; i++ {
				lines = append(lines, "")
			}
			pendingBlank = 0
			text := strings.TrimPrefix(trimmed, prefix)
			text = strings.TrimPrefix(text, " ")
			lines = append(lines, text)
			end = lineEnd
			sawComment = true
		case strings.TrimSpace(body) == "":
			// Blank line: tolerate it only between comment lines, do not extend the span yet.
			pendingBlank++
		default:
			// Non-comment content ends the leading comment run.
			pos = len(content)
			continue
		}
		pos = lineEnd
	}

	if !sawComment {
		return 0, 0, "", false
	}
	return start, end, strings.Join(lines, "\n"), true
}

// spdxIdentifierTag extracts the SPDX id from an "SPDX-License-Identifier:" line, if
// present. This is the universal REUSE tag and a definitive license signal.
func spdxIdentifierTag(commentText string) (string, bool) {
	const tag = "spdx-license-identifier:"
	lower := strings.ToLower(commentText)
	idx := strings.Index(lower, tag)
	if idx < 0 {
		return "", false
	}
	rest := commentText[idx+len(tag):]
	if nl := strings.IndexAny(rest, "\r\n"); nl >= 0 {
		rest = rest[:nl]
	}
	id := strings.TrimSpace(rest)
	if id == "" {
		return "", false
	}
	// A multi-license expression is reported as-is; the first token is the primary id.
	if fields := strings.Fields(id); len(fields) > 0 {
		return id, true
	}
	return id, true
}

// matchStandardHeader checks the comment text against every curated SPDX
// standardLicenseHeader, matching on a long run of consecutive significant words so
// that placeholder differences (year, holder, program name) do not defeat the match
// and so ordinary prose cannot trip it.
func matchStandardHeader(commentText string) (string, bool) {
	commentTokens := significantTokens(commentText)
	if len(commentTokens) < standardHeaderMatchTokens {
		return "", false
	}
	commentJoined := " " + strings.Join(commentTokens, " ") + " "

	for _, id := range curatedStandardHeaderIDs {
		lic, ok := spdx.Lookup(id)
		if !ok || lic.StandardHeader == "" {
			continue
		}
		if runMatches(commentJoined, lic.StandardHeader) {
			return id, true
		}
	}
	return "", false
}

// runMatches reports whether commentJoined (space-delimited, space-bracketed,
// significant tokens) contains any consecutive run of standardHeaderMatchTokens
// significant words drawn from header. Placeholder tokens are dropped from header
// before runs are formed, so the matched run is from the license's immutable wording.
func runMatches(commentJoined, header string) bool {
	tokens := significantTokens(header)
	if len(tokens) < standardHeaderMatchTokens {
		return false
	}
	for i := 0; i+standardHeaderMatchTokens <= len(tokens); i++ {
		run := " " + strings.Join(tokens[i:i+standardHeaderMatchTokens], " ") + " "
		if strings.Contains(commentJoined, run) {
			return true
		}
	}
	return false
}

// extractHolder pulls the copyright holder from a REUSE "SPDX-FileCopyrightText:" or
// a conventional "Copyright" line. The year and leading "(C)"/"(c)" markers are
// stripped so the returned holder is just the name. Returns "" when none is found.
func extractHolder(commentText string) string {
	for _, line := range strings.Split(commentText, "\n") {
		lower := strings.ToLower(line)
		var rest string
		switch {
		case strings.Contains(lower, "spdx-filecopyrighttext:"):
			rest = line[strings.Index(lower, "spdx-filecopyrighttext:")+len("spdx-filecopyrighttext:"):]
		case strings.Contains(lower, "copyright"):
			rest = line[strings.Index(lower, "copyright")+len("copyright"):]
		default:
			continue
		}
		holder := stripCopyrightDecoration(rest)
		if holder != "" {
			return holder
		}
	}
	return ""
}

// extractYear pulls a 4-digit year or YYYY-YYYY range from a copyright line. Returns
// "" when none is present.
func extractYear(commentText string) string {
	for _, line := range strings.Split(commentText, "\n") {
		lower := strings.ToLower(line)
		if !strings.Contains(lower, "copyright") {
			continue
		}
		if y := findYearSpan(line); y != "" {
			return y
		}
	}
	// Fall back to any copyright-bearing fragment even if the keyword case differed.
	if y := findYearSpan(commentText); y != "" {
		return y
	}
	return ""
}

// --- small parsing helpers (WHY package-local: avoid pulling regexp for a few
// fixed-shape extractions; keeps detection allocation-light and easy to audit). ---

// hasRule reports whether ft lists a preserve rule of the given kind and direction.
func hasRule(ft model.FileType, kind model.PreserveKind, before bool) bool {
	for _, r := range ft.PreserveFirst {
		if r.Kind == kind && r.Before == before {
			return true
		}
	}
	return false
}

// lineEndOffset returns the offset just past the newline terminating the line at pos
// (or len(content) for the final unterminated line).
func lineEndOffset(content []byte, pos int) int {
	for i := pos; i < len(content); i++ {
		if content[i] == '\n' {
			return i + 1
		}
	}
	return len(content)
}

// isCodingPragma reports whether trimmed is an encoding pragma like
// "# -*- coding: utf-8 -*-" or "# vim: ..." style coding declaration.
func isCodingPragma(trimmed string) bool {
	t := strings.TrimSpace(trimmed)
	if !strings.HasPrefix(t, "#") {
		return false
	}
	lower := strings.ToLower(t)
	return strings.Contains(lower, "coding:") || strings.Contains(lower, "coding=")
}

// stripBlockInner removes leading per-line decoration ("*" continuation markers and
// surrounding whitespace) commonly used inside /* ... */ blocks, returning the inner
// text as joined lines so phrase matching sees the prose, not the box drawing.
func stripBlockInner(inner string) string {
	lines := strings.Split(inner, "\n")
	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		t := strings.TrimRight(ln, "\r")
		t = strings.TrimSpace(t)
		t = strings.TrimPrefix(t, "*")
		t = strings.TrimSpace(t)
		out = append(out, t)
	}
	return strings.Join(out, "\n")
}

// normalizeWhitespace collapses all runs of whitespace to single spaces and trims, so
// phrase matching is insensitive to wrapping and indentation differences.
func normalizeWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// significantTokens lowercases, strips punctuation, drops placeholder tokens (bracketed
// like "[yyyy]" or "<year>"), and returns the remaining words. WHY drop punctuation
// and placeholders: SPDX headers and real headers differ in punctuation and in the
// holder/year placeholders, so matching on bare significant words is robust.
func significantTokens(s string) []string {
	var b strings.Builder
	depth := 0
	for _, r := range s {
		switch r {
		case '[', '<', '{':
			depth++
			b.WriteByte(' ')
			continue
		case ']', '>', '}':
			if depth > 0 {
				depth--
			}
			b.WriteByte(' ')
			continue
		}
		if depth > 0 {
			continue
		}
		if isWordRune(r) {
			b.WriteRune(toLowerRune(r))
		} else {
			b.WriteByte(' ')
		}
	}
	return strings.Fields(b.String())
}

// isWordRune reports whether r is part of a word (letter or digit).
func isWordRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

// toLowerRune lowercases an ASCII letter; other runes pass through (only word runes
// reach this in practice).
func toLowerRune(r rune) rune {
	if r >= 'A' && r <= 'Z' {
		return r + ('a' - 'A')
	}
	return r
}

// stripCopyrightDecoration cleans a copyright line remainder down to the holder name:
// removes leading ":", "(c)"/"(C)", years and year ranges, and surrounding punctuation.
func stripCopyrightDecoration(rest string) string {
	rest = strings.TrimSpace(rest)
	rest = strings.TrimPrefix(rest, ":")
	rest = strings.TrimSpace(rest)
	// Drop a leading "(c)" / "(C)" marker.
	for _, marker := range []string{"(c)", "(C)", "©"} {
		rest = strings.TrimSpace(strings.TrimPrefix(rest, marker))
	}
	// Drop a leading year or year range token.
	fields := strings.Fields(rest)
	if len(fields) > 0 && isYearToken(fields[0]) {
		fields = fields[1:]
	}
	holder := strings.TrimSpace(strings.Join(fields, " "))
	holder = strings.TrimRight(holder, ".")
	return strings.TrimSpace(holder)
}

// findYearSpan returns the first 4-digit year or YYYY-YYYY range found in s.
func findYearSpan(s string) string {
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		if !isDigit(runes[i]) {
			continue
		}
		j := i
		for j < len(runes) && isDigit(runes[j]) {
			j++
		}
		if j-i != 4 {
			i = j
			continue
		}
		// Possible range: YYYY-YYYY.
		if j < len(runes) && runes[j] == '-' {
			k := j + 1
			start := k
			for k < len(runes) && isDigit(runes[k]) {
				k++
			}
			if k-start == 4 {
				return string(runes[i:k])
			}
		}
		return string(runes[i:j])
	}
	return ""
}

// isYearToken reports whether tok is a 4-digit year or a YYYY-YYYY range, possibly
// with trailing punctuation.
func isYearToken(tok string) bool {
	tok = strings.Trim(tok, ".,;:")
	return findYearSpan(tok) == tok && tok != ""
}

// isDigit reports whether r is an ASCII digit.
func isDigit(r rune) bool { return r >= '0' && r <= '9' }
