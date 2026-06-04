package render

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/KofTwentyTwo/license-tool/internal/filetype"
	managedheader "github.com/KofTwentyTwo/license-tool/internal/header"
	"github.com/KofTwentyTwo/license-tool/internal/model"
	"github.com/KofTwentyTwo/license-tool/internal/spdx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeFile writes content to path for tests, returning any error so callers can
// assert on it with require.NoError.
func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

// mustLicense fetches a curated license from the frozen spdx package, failing the
// test if it is missing (which would mean the vendored snapshot regressed).
func mustLicense(t *testing.T, id string) model.License {
	t.Helper()
	lic, ok := spdx.Lookup(id)
	require.Truef(t, ok, "spdx.Lookup(%q)", id)
	return lic
}

// ftFor pulls a real FileType from the frozen filetype table.
func ftFor(t *testing.T, sample string) model.FileType {
	t.Helper()
	ft, ok := filetype.Lookup(sample)
	require.Truef(t, ok, "filetype.Lookup(%q)", sample)
	return ft
}

func TestREUSETags(t *testing.T) {
	mit := mustLicense(t, "MIT")
	cases := []struct {
		name   string
		holder string
		year   string
		want   string
	}{
		{
			name:   "holder and year",
			holder: "Kingsrook, LLC",
			year:   "2021-2026",
			want:   "SPDX-FileCopyrightText: 2021-2026 Kingsrook, LLC\nSPDX-License-Identifier: MIT",
		},
		{
			name:   "year only",
			holder: "",
			year:   "2026",
			want:   "SPDX-FileCopyrightText: 2026\nSPDX-License-Identifier: MIT",
		},
		{
			name:   "holder only",
			holder: "Acme",
			year:   "",
			want:   "SPDX-FileCopyrightText: Acme\nSPDX-License-Identifier: MIT",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, REUSETags(mit, c.holder, c.year))
		})
	}
}

func TestHeaderStylesPlaintextContent(t *testing.T) {
	agpl := mustLicense(t, "AGPL-3.0-or-later")
	mit := mustLicense(t, "MIT")
	goFT := ftFor(t, "x.go")

	cases := []struct {
		name        string
		in          HeaderInput
		mustContain []string
		mustNotHave []string
	}{
		{
			name: "reuse only emits tags and sentinel, no notice",
			in: HeaderInput{
				License: agpl, Holder: "Kingsrook, LLC", Year: "2026",
				Style: model.StyleReuse, FileType: goFT,
			},
			mustContain: []string{
				managedheader.Sentinel,
				"SPDX-FileCopyrightText: 2026 Kingsrook, LLC",
				"SPDX-License-Identifier: AGPL-3.0-or-later",
			},
			mustNotHave: []string{"This program is free software"},
		},
		{
			name: "notice only emits the notice block, not the REUSE id tag",
			in: HeaderInput{
				License: agpl, Holder: "Kingsrook, LLC", Year: "2026",
				Style: model.StyleNotice, FileType: goFT,
			},
			mustContain: []string{
				managedheader.Sentinel,
				"Copyright (c) 2026 Kingsrook, LLC",
				"This program is free software",
				"GNU Affero General Public License",
			},
			mustNotHave: []string{"SPDX-License-Identifier:"},
		},
		{
			name: "reuse+notice emits both",
			in: HeaderInput{
				License: agpl, Holder: "Kingsrook, LLC", Year: "2021-2026",
				Style: model.StyleReusePlusNotice, FileType: goFT,
			},
			mustContain: []string{
				managedheader.Sentinel,
				"SPDX-FileCopyrightText: 2021-2026 Kingsrook, LLC",
				"SPDX-License-Identifier: AGPL-3.0-or-later",
				"This program is free software",
			},
		},
		{
			name: "notice style on a license without a standard header omits notice gracefully",
			in: HeaderInput{
				License: mit, Holder: "Acme", Year: "2026",
				Style: model.StyleReusePlusNotice, FileType: goFT,
			},
			mustContain: []string{
				"SPDX-License-Identifier: MIT",
			},
			// MIT defines no StandardHeader, so no notice body appears.
			mustNotHave: []string{"This program is free software", "Permission is hereby granted"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Header(c.in)
			require.NoError(t, err)
			for _, s := range c.mustContain {
				assert.Containsf(t, got, s, "expected header to contain %q", s)
			}
			for _, s := range c.mustNotHave {
				assert.NotContainsf(t, got, s, "expected header NOT to contain %q", s)
			}
		})
	}
}

func TestHeaderCommentWrapping(t *testing.T) {
	mit := mustLicense(t, "MIT")
	cases := []struct {
		name       string
		sampleFile string
		blockOpen  string
		blockClose string
		linePrefix string
		isBlock    bool
	}{
		{name: "Go block", sampleFile: "x.go", blockOpen: "/*", blockClose: "*/", isBlock: true},
		{name: "Java block", sampleFile: "X.java", blockOpen: "/*", blockClose: "*/", isBlock: true},
		{name: "XML block", sampleFile: "pom.xml", blockOpen: "<!--", blockClose: "-->", isBlock: true},
		{name: "Shell line", sampleFile: "x.sh", linePrefix: "# ", isBlock: false},
		{name: "Python line", sampleFile: "x.py", linePrefix: "# ", isBlock: false},
		{name: "Rust line", sampleFile: "x.rs", linePrefix: "// ", isBlock: false},
		{name: "SQL line", sampleFile: "x.sql", linePrefix: "-- ", isBlock: false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ft := ftFor(t, c.sampleFile)
			got, err := Header(HeaderInput{
				License: mit, Holder: "Acme", Year: "2026",
				Style: model.StyleReuse, FileType: ft,
			})
			require.NoError(t, err)
			// Always LF, always a trailing blank-line separator.
			assert.True(t, strings.HasSuffix(got, "\n\n"), "header must end with a blank-line separator")
			assert.NotContains(t, got, "\r", "Header output must be LF-only")

			lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
			if c.isBlock {
				assert.Equal(t, c.blockOpen, lines[0], "first line is the block opener")
				assert.Equal(t, c.blockClose, lines[len(lines)-1], "last line is the block closer")
			} else {
				for _, l := range lines {
					assert.Truef(t, strings.HasPrefix(l, strings.TrimRight(c.linePrefix, " ")),
						"every line carries the line prefix; got %q", l)
				}
			}
		})
	}
}

func TestHeaderRejectsSkipType(t *testing.T) {
	mit := mustLicense(t, "MIT")
	jsonFT := ftFor(t, "x.json")
	require.True(t, jsonFT.Skip, "JSON must be a skip type in the frozen table")
	_, err := Header(HeaderInput{
		License: mit, Holder: "Acme", Year: "2026",
		Style: model.StyleReuse, FileType: jsonFT,
	})
	assert.Error(t, err, "rendering a header for an uncommentable type must error")
}

func TestLicenseFileAndEntry(t *testing.T) {
	for _, id := range []string{"MIT", "Apache-2.0", "AGPL-3.0-or-later"} {
		t.Run(id, func(t *testing.T) {
			lic := mustLicense(t, id)
			body, err := LicenseFile(lic)
			require.NoError(t, err)
			assert.NotEmpty(t, body)
			assert.True(t, strings.HasSuffix(body, "\n"), "LICENSE body ends with a newline")
			assert.False(t, strings.HasSuffix(body, "\n\n"), "exactly one trailing newline")
			assert.Contains(t, body, strings.SplitN(lic.Text, "\n", 2)[0], "verbatim text preserved")

			entry, err := LicensesEntry(lic)
			require.NoError(t, err)
			assert.Equal(t, body, entry, "LICENSES/<id>.txt is byte-identical to LICENSE")
		})
	}
}

func TestLicenseFileEmptyText(t *testing.T) {
	_, err := LicenseFile(model.License{SPDXID: "FAKE"})
	assert.Error(t, err, "a license with no vendored text cannot render a LICENSE file")
}

func TestYearResolver(t *testing.T) {
	const now = 2026
	cases := []struct {
		name string
		spec model.YearSpec
		want string
	}{
		{"current", model.YearSpec{Kind: model.YearCurrent}, "2026"},
		{"explicit", model.YearSpec{Kind: model.YearExplicit, Start: 2019}, "2019"},
		{"range", model.YearSpec{Kind: model.YearRange, Start: 2019, End: 2026}, "2019-2026"},
		{"range collapses equal bounds", model.YearSpec{Kind: model.YearRange, Start: 2026, End: 2026}, "2026"},
		{"range collapses inverted bounds", model.YearSpec{Kind: model.YearRange, Start: 2026, End: 2019}, "2026"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := NewYearResolver(c.spec).Resolve("", now)
			require.NoError(t, err)
			assert.Equal(t, c.want, got)
		})
	}
}

// --- Mutation: insertion, preserve-first ordering, line endings, BOM ---

// makeHeaderLF renders a small known header for mutation tests so assertions can
// match exact substrings without depending on full license text.
func makeHeaderLF(t *testing.T, sampleFile string) (model.FileType, string) {
	t.Helper()
	mit := mustLicense(t, "MIT")
	ft := ftFor(t, sampleFile)
	h, err := Header(HeaderInput{
		License: mit, Holder: "Acme", Year: "2026",
		Style: model.StyleReuse, FileType: ft,
	})
	require.NoError(t, err)
	return ft, h
}

func TestSpliceInsertGoBeforePackage(t *testing.T) {
	ft, header := makeHeaderLF(t, "x.go")
	content := []byte("package main\n\nfunc main() {}\n")

	out, action := Splice(content, ft, header, model.DetectedHeader{Present: false})
	got := string(out)

	assert.Equal(t, "insert", action)
	// Header must come before the package declaration, which must survive intact.
	idxHeader := strings.Index(got, "SPDX-License-Identifier: MIT")
	idxPkg := strings.Index(got, "package main")
	require.NotEqual(t, -1, idxHeader)
	require.NotEqual(t, -1, idxPkg)
	assert.Less(t, idxHeader, idxPkg, "header precedes the package declaration")
	assert.True(t, strings.HasSuffix(got, "func main() {}\n"), "trailing content preserved exactly")
}

func TestSpliceInsertAfterShebang(t *testing.T) {
	ft, header := makeHeaderLF(t, "x.sh")
	content := []byte("#!/usr/bin/env bash\nset -e\necho hi\n")

	out, action := Splice(content, ft, header, model.DetectedHeader{Present: false})
	got := string(out)

	assert.Equal(t, "insert", action)
	assert.True(t, strings.HasPrefix(got, "#!/usr/bin/env bash\n"), "shebang must remain the first line")
	idxShebang := strings.Index(got, "#!/usr/bin/env bash")
	idxHeader := strings.Index(got, "SPDX-License-Identifier: MIT")
	assert.Less(t, idxShebang, idxHeader, "header is placed after the shebang")
}

func TestSpliceInsertAfterXMLDecl(t *testing.T) {
	ft, header := makeHeaderLF(t, "pom.xml")
	content := []byte("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<project></project>\n")

	out, action := Splice(content, ft, header, model.DetectedHeader{Present: false})
	got := string(out)

	assert.Equal(t, "insert", action)
	assert.True(t, strings.HasPrefix(got, "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n"),
		"xml declaration must remain first")
	idxDecl := strings.Index(got, "<?xml")
	idxHeader := strings.Index(got, "SPDX-License-Identifier: MIT")
	assert.Less(t, idxDecl, idxHeader, "header is placed after the xml declaration")
	assert.Contains(t, got, "<!--", "header uses XML block comment syntax")
}

func TestSpliceInsertAfterPHPOpen(t *testing.T) {
	ft, header := makeHeaderLF(t, "x.php")
	content := []byte("<?php\necho 'hi';\n")

	out, action := Splice(content, ft, header, model.DetectedHeader{Present: false})
	got := string(out)

	assert.Equal(t, "insert", action)
	assert.True(t, strings.HasPrefix(got, "<?php\n"), "php open tag must remain first")
	idxOpen := strings.Index(got, "<?php")
	idxHeader := strings.Index(got, "SPDX-License-Identifier: MIT")
	assert.Less(t, idxOpen, idxHeader)
}

// TestSpliceShebangPreservedForBlockCommentTypes is the regression guard for the
// universal-shebang fix. Before the fix, a leading "#!" was only preserved when the
// file type explicitly listed PreserveShebang; a BLOCK-comment type either lacked the
// rule (C/C++) or ordered it after php-open (PHP), so the header was spliced ABOVE the
// shebang and the script stopped being executable. The fix preserves any leading "#!"
// universally, so on every block-comment type the shebang must stay line 1 with the
// header inserted strictly after it.
func TestSpliceShebangPreservedForBlockCommentTypes(t *testing.T) {
	cases := []struct {
		name       string
		sampleFile string
		content    string
		// wantPrefix is the exact byte prefix that must lead the output (the shebang,
		// plus any prolog such as <?php, all before the header).
		wantPrefix string
	}{
		{
			// PHP CLI script: "#!" line 1, "<?php" line 2, header after both. PHP is a
			// block-comment type whose rule list orders shebang before php-open.
			name:       "php shebang then php-open",
			sampleFile: "x.php",
			content:    "#!/usr/bin/env php\n<?php\necho 'hi';\n",
			wantPrefix: "#!/usr/bin/env php\n<?php\n",
		},
		{
			// JavaScript is a block-comment type that DOES list PreserveShebang: a node
			// CLI shebang must stay line 1 with the /* */ header after it.
			name:       "javascript block-comment type with shebang",
			sampleFile: "cli.js",
			content:    "#!/usr/bin/env node\nconsole.log('hi');\n",
			wantPrefix: "#!/usr/bin/env node\n",
		},
		{
			// C/C++ is a block-comment type that does NOT list PreserveShebang at all.
			// This is the case the universal rule exists for: preservation must NOT be
			// gated on the type listing the rule. (binfmt_misc lets a "#!" launch any
			// interpreter, so a shebang on a .c source is legal and must survive.)
			name:       "c block-comment type without a shebang rule",
			sampleFile: "x.c",
			content:    "#!/usr/bin/tcc -run\nint main(void){return 0;}\n",
			wantPrefix: "#!/usr/bin/tcc -run\n",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ft, header := makeHeaderLF(t, c.sampleFile)
			out, action := Splice([]byte(c.content), ft, header, model.DetectedHeader{Present: false})
			got := string(out)

			assert.Equal(t, "insert", action)
			assert.True(t, strings.HasPrefix(got, c.wantPrefix),
				"the shebang (and any prolog) must remain the file's leading bytes; got %q", got)
			idxShebang := strings.Index(got, "#!")
			idxHeader := strings.Index(got, "SPDX-License-Identifier: MIT")
			require.GreaterOrEqual(t, idxHeader, 0, "header must be present")
			assert.Equal(t, 0, idxShebang, "the shebang must start at byte 0 (line 1)")
			assert.Less(t, idxShebang, idxHeader, "the header must be inserted after the shebang")
			assert.Contains(t, got, ft.CommentStyle.Open,
				"the inserted header uses the type's block-comment open delimiter")
		})
	}
}

// TestSpliceNonShebangBlockCommentUnaffected proves the universal rule is inert when
// there is no shebang: a block-comment file whose first line is ordinary code must get
// the header at the very top (byte 0), exactly as before the fix. This pins the "only
// consumed when the line actually starts with #!" safety claim.
func TestSpliceNonShebangBlockCommentUnaffected(t *testing.T) {
	ft, header := makeHeaderLF(t, "x.c")
	content := []byte("int main(void){return 0;}\n")

	out, action := Splice(content, ft, header, model.DetectedHeader{Present: false})
	got := string(out)

	assert.Equal(t, "insert", action)
	assert.True(t, strings.HasPrefix(got, ft.CommentStyle.Open),
		"with no shebang the header leads at byte 0; got %q", got)
	idxHeader := strings.Index(got, "SPDX-License-Identifier: MIT")
	idxBody := strings.Index(got, "int main")
	assert.Less(t, idxHeader, idxBody, "header precedes the first real line when there is no shebang")
}

func TestSpliceInsertAfterCodingPragma(t *testing.T) {
	ft, header := makeHeaderLF(t, "x.py")
	content := []byte("#!/usr/bin/env python3\n# -*- coding: utf-8 -*-\nprint('hi')\n")

	out, action := Splice(content, ft, header, model.DetectedHeader{Present: false})
	got := string(out)

	assert.Equal(t, "insert", action)
	assert.True(t, strings.HasPrefix(got, "#!/usr/bin/env python3\n# -*- coding: utf-8 -*-\n"),
		"shebang and coding pragma must both remain, in order, before the header")
	idxPragma := strings.Index(got, "coding: utf-8")
	idxHeader := strings.Index(got, "SPDX-License-Identifier: MIT")
	assert.Less(t, idxPragma, idxHeader, "header is placed after the coding pragma")
}

func TestSpliceInsertAfterCodingEqualsPragma(t *testing.T) {
	ft, header := makeHeaderLF(t, "x.py")
	content := []byte("# coding=utf-8\nprint('hi')\n")

	out, action := Splice(content, ft, header, model.DetectedHeader{Present: false})
	got := string(out)

	assert.Equal(t, "insert", action)
	assert.True(t, strings.HasPrefix(got, "# coding=utf-8\n"),
		"coding pragma must remain before the header")
	idxPragma := strings.Index(got, "coding=utf-8")
	idxHeader := strings.Index(got, "SPDX-License-Identifier: MIT")
	assert.Less(t, idxPragma, idxHeader, "header is placed after the coding pragma")
}

func TestSplicePreservesBOM(t *testing.T) {
	ft, header := makeHeaderLF(t, "x.go")
	content := append([]byte{0xEF, 0xBB, 0xBF}, []byte("package main\n")...)

	out, _ := Splice(content, ft, header, model.DetectedHeader{Present: false})

	require.GreaterOrEqual(t, len(out), 3)
	assert.Equal(t, []byte{0xEF, 0xBB, 0xBF}, out[:3], "BOM must lead the file untouched")
	// The header must come after the BOM and before the package line.
	rest := string(out[3:])
	idxHeader := strings.Index(rest, "SPDX-License-Identifier: MIT")
	idxPkg := strings.Index(rest, "package main")
	assert.Less(t, idxHeader, idxPkg)
}

func TestSplicePreservesCRLF(t *testing.T) {
	ft, header := makeHeaderLF(t, "x.go")
	content := []byte("package main\r\n\r\nfunc main() {}\r\n")

	out, action := Splice(content, ft, header, model.DetectedHeader{Present: false})
	got := string(out)

	assert.Equal(t, "insert", action)
	// The inserted header region must use CRLF, and no lone LF must be introduced.
	assert.NotContains(t, strings.ReplaceAll(got, "\r\n", ""), "\n",
		"every newline in a CRLF file must remain CRLF")
	assert.True(t, strings.HasSuffix(got, "func main() {}\r\n"), "trailing content and its CRLF preserved")
}

func TestSplicePreservesNoTrailingNewline(t *testing.T) {
	ft, header := makeHeaderLF(t, "x.go")
	content := []byte("package main\n\nfunc main() {}") // no trailing newline

	out, _ := Splice(content, ft, header, model.DetectedHeader{Present: false})
	got := string(out)

	assert.False(t, strings.HasSuffix(got, "\n"), "absence of a trailing newline must be preserved")
	assert.True(t, strings.HasSuffix(got, "func main() {}"))
}

// --- Mutation: replace / relicense / idempotency ---

func TestSpliceReplaceRelicense(t *testing.T) {
	goFT := ftFor(t, "x.go")
	agpl := mustLicense(t, "AGPL-3.0-or-later")

	// Build a file that already carries an AGPL header (insert it first).
	agplHeader, err := Header(HeaderInput{
		License: agpl, Holder: "Kingsrook, LLC", Year: "2021",
		Style: model.StyleReusePlusNotice, FileType: goFT,
	})
	require.NoError(t, err)
	base := []byte("package main\n\nfunc main() {}\n")
	withAGPL, _ := Splice(base, goFT, agplHeader, model.DetectedHeader{Present: false})

	// The detected span is exactly the header region we just inserted: from offset 0
	// to the start of the package declaration.
	pkgIdx := strings.Index(string(withAGPL), "package main")
	require.Positive(t, pkgIdx)
	detected := model.DetectedHeader{Present: true, StartByte: 0, EndByte: pkgIdx, ViaSentinel: true}

	// Relicense to Apache-2.0.
	apache := mustLicense(t, "Apache-2.0")
	apacheHeader, err := Header(HeaderInput{
		License: apache, Holder: "Kingsrook, LLC", Year: "2026",
		Style: model.StyleReusePlusNotice, FileType: goFT,
	})
	require.NoError(t, err)

	out, action := Splice(withAGPL, goFT, apacheHeader, detected)
	got := string(out)

	assert.Equal(t, "replace", action)
	assert.Contains(t, got, "SPDX-License-Identifier: Apache-2.0", "new license written")
	assert.NotContains(t, got, "SPDX-License-Identifier: AGPL-3.0-or-later", "old license id removed")
	assert.NotContains(t, got, "GNU Affero General Public License", "old notice block fully removed (no stacking)")
	assert.True(t, strings.HasSuffix(got, "func main() {}\n"), "code body untouched by relicense")
	// Exactly one header: the Apache id must appear once.
	assert.Equal(t, 1, strings.Count(got, "SPDX-License-Identifier:"), "no stacked headers")
}

func TestSpliceIdempotent(t *testing.T) {
	goFT := ftFor(t, "x.go")
	mit := mustLicense(t, "MIT")
	header, err := Header(HeaderInput{
		License: mit, Holder: "Acme", Year: "2026",
		Style: model.StyleReuse, FileType: goFT,
	})
	require.NoError(t, err)

	base := []byte("package main\n")
	once, _ := Splice(base, goFT, header, model.DetectedHeader{Present: false})

	pkgIdx := strings.Index(string(once), "package main")
	detected := model.DetectedHeader{Present: true, StartByte: 0, EndByte: pkgIdx, ViaSentinel: true}

	// Re-applying the identical header over the detected span is a no-op.
	twice, action := Splice(once, goFT, header, detected)
	assert.Equal(t, "none", action, "re-applying an identical header is a no-op")
	assert.Equal(t, string(once), string(twice), "idempotent: bytes unchanged on second apply")
}

func TestReplaceRejectsOutOfRangeSpan(t *testing.T) {
	goFT := ftFor(t, "x.go")
	_, header := makeHeaderLF(t, "x.go")
	content := []byte("package main\n")

	// A span past the end of the buffer must be refused, not panic or corrupt.
	out, action := Splice(content, goFT, header,
		model.DetectedHeader{Present: true, StartByte: 0, EndByte: 9999})
	assert.Equal(t, "none", action)
	assert.Equal(t, string(content), string(out), "malformed span leaves content untouched")
}

func TestInsertAndReplaceDirectAPIs(t *testing.T) {
	goFT := ftFor(t, "x.go")
	_, header := makeHeaderLF(t, "x.go")

	insOut, insAction := Insert([]byte("package main\n"), goFT, header)
	assert.Equal(t, "insert", insAction)
	assert.Contains(t, string(insOut), "SPDX-License-Identifier: MIT")

	repOut, repAction := Replace([]byte("OLDOLDOLDpackage main\n"), header, 0, 9)
	assert.Equal(t, "replace", repAction)
	assert.True(t, strings.HasSuffix(string(repOut), "package main\n"))
	assert.Contains(t, string(repOut), "SPDX-License-Identifier: MIT")
}

// --- Additional branch coverage ---

func TestHeaderEmptyBodyErrors(t *testing.T) {
	// A license with no StandardHeader rendered in notice-only style yields no body
	// content beyond the sentinel; with no REUSE tags and no notice the renderer must
	// refuse rather than emit a sentinel-only comment.
	mit := mustLicense(t, "MIT") // MIT has no StandardHeader
	goFT := ftFor(t, "x.go")
	// StyleNotice with a license that defines no notice => no tags, no notice.
	_, err := Header(HeaderInput{
		License: mit, Holder: "Acme", Year: "2026",
		Style: model.StyleNotice, FileType: goFT,
	})
	assert.Error(t, err, "notice-only style on a no-notice license has no body and must error")
}

func TestCopyrightLineOmittedWhenNoYearOrHolder(t *testing.T) {
	// AGPL notice with neither holder nor year must not emit a dangling "Copyright"
	// attribution line.
	agpl := mustLicense(t, "AGPL-3.0-or-later")
	goFT := ftFor(t, "x.go")
	got, err := Header(HeaderInput{
		License: agpl, Holder: "", Year: "",
		Style: model.StyleNotice, FileType: goFT,
	})
	require.NoError(t, err)
	assert.NotContains(t, got, "Copyright (c)", "no attribution line when holder and year are empty")
	assert.Contains(t, got, "This program is free software", "notice body still emitted")
}

func TestInsertAfterShebangNoTrailingNewline(t *testing.T) {
	// A shebang that is the entire file (no trailing newline) exercises both the
	// advanceLineIf no-newline branch and the prefix-EOL guard in insertAt.
	ft, header := makeHeaderLF(t, "x.sh")
	content := []byte("#!/usr/bin/env bash") // no newline at all

	out, action := Splice(content, ft, header, model.DetectedHeader{Present: false})
	got := string(out)

	assert.Equal(t, "insert", action)
	assert.True(t, strings.HasPrefix(got, "#!/usr/bin/env bash\n"),
		"a newline is inserted after a newline-less shebang before the header")
	idxShebang := strings.Index(got, "#!/usr/bin/env bash")
	idxHeader := strings.Index(got, "SPDX-License-Identifier: MIT")
	assert.Less(t, idxShebang, idxHeader)
}

func TestInsertWhenShebangAbsent(t *testing.T) {
	// A shell file whose first line is NOT a shebang exercises advanceLineIf's
	// no-match branch: the boundary stays at 0 and the header leads the file.
	ft, header := makeHeaderLF(t, "x.sh")
	content := []byte("set -e\necho hi\n")

	out, action := Splice(content, ft, header, model.DetectedHeader{Present: false})
	got := string(out)

	assert.Equal(t, "insert", action)
	idxHeader := strings.Index(got, "SPDX-License-Identifier: MIT")
	idxBody := strings.Index(got, "set -e")
	assert.Less(t, idxHeader, idxBody, "header leads when there is no shebang to preserve")
}

func TestYearResolverGit(t *testing.T) {
	// gitutil.FirstCommitYear shells out to git; build a tiny real repo so the git
	// year strategy is exercised end to end.
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(cmd.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e",
		)
		out, err := cmd.CombinedOutput()
		require.NoErrorf(t, err, "git %v: %s", args, out)
	}
	run("init")
	require.NoError(t, writeFile(filepath.Join(dir, "f.txt"), "x"))
	run("add", ".")
	// Pin the commit date so the first-commit year is deterministic.
	commitDate := "2019-01-01T00:00:00 +0000"
	cmd := exec.Command("git", "commit", "-m", "init", "--date", commitDate)
	cmd.Dir = dir
	cmd.Env = append(cmd.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e",
		"GIT_COMMITTER_DATE="+commitDate,
	)
	out, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "git commit: %s", out)

	now := time.Now().Year()
	got, err := NewYearResolver(model.YearSpec{Kind: model.YearGit}).Resolve(dir, now)
	require.NoError(t, err)
	want := formatRangeForTest(2019, now)
	assert.Equal(t, want, got, "git year is first-commit-year..now")
}

// formatRangeForTest mirrors the production range formatting for the assertion
// above without reaching into unexported state in a brittle way.
func formatRangeForTest(start, end int) string {
	if end <= start {
		return strconv.Itoa(start)
	}
	return strconv.Itoa(start) + "-" + strconv.Itoa(end)
}

func TestYearResolverGitErrorOutsideRepo(t *testing.T) {
	// A non-repo path must surface an error rather than a guessed year.
	_, err := NewYearResolver(model.YearSpec{Kind: model.YearGit}).Resolve(t.TempDir(), 2026)
	assert.Error(t, err, "git year on a non-repo must error, never guess")
}

func TestYearResolverUnknownKind(t *testing.T) {
	// An out-of-range YearKind is a programmer error; it must error, not panic.
	_, err := NewYearResolver(model.YearSpec{Kind: model.YearKind(99)}).Resolve("", 2026)
	assert.Error(t, err)
}

// --- White-box helper coverage: blank-line line-wrapping, hasRule miss, EOF guard ---

// TestWrapCommentLineStyleBlankLines drives wrapComment directly so the
// blank-body-line branch of the line-comment path is exercised deterministically:
// a blank input line must become the line prefix trimmed of trailing whitespace
// (no trailing spaces emitted), while non-blank lines keep the full prefix.
func TestWrapCommentLineStyleBlankLines(t *testing.T) {
	cases := []struct {
		name string
		body string
		cs   model.CommentStyle
		want string
	}{
		{
			name: "hash prefix blank line trims trailing space",
			body: "first\n\nsecond",
			cs:   model.CommentStyle{Block: false, LinePrefix: "# "},
			// The middle blank line becomes a bare "#" with no trailing space.
			want: "# first\n#\n# second",
		},
		{
			name: "slash prefix blank line trims trailing space",
			body: "a\n\nb",
			cs:   model.CommentStyle{Block: false, LinePrefix: "// "},
			want: "// a\n//\n// b",
		},
		{
			name: "leading and trailing blank lines",
			body: "\nmid\n",
			cs:   model.CommentStyle{Block: false, LinePrefix: "-- "},
			want: "--\n-- mid\n--",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := wrapComment(c.body, c.cs)
			assert.Equal(t, c.want, got)
			// A blank body line must never carry trailing whitespace.
			for _, l := range strings.Split(got, "\n") {
				assert.Equal(t, strings.TrimRight(l, " \t"), l,
					"no line may carry trailing whitespace, got %q", l)
			}
		})
	}
}

// TestWrapCommentBlankLineThroughHeader confirms the same blank-line behavior end
// to end: a line-commented file rendered with StyleReusePlusNotice on a license
// that defines a standard header produces a blank separator line between the REUSE
// tags and the notice, which must wrap to a bare (whitespace-free) prefix.
func TestWrapCommentBlankLineThroughHeader(t *testing.T) {
	agpl := mustLicense(t, "AGPL-3.0-or-later")
	shFT := ftFor(t, "x.sh")
	require.False(t, shFT.CommentStyle.Block, "shell must be a line-comment type")

	got, err := Header(HeaderInput{
		License: agpl, Holder: "Acme", Year: "2026",
		Style: model.StyleReusePlusNotice, FileType: shFT,
	})
	require.NoError(t, err)

	prefix := strings.TrimRight(shFT.CommentStyle.LinePrefix, " \t")
	// The blank separator between tags and notice must appear as a bare prefix line.
	assert.Contains(t, got, "\n"+prefix+"\n", "blank separator wraps to a bare prefix line")
	for _, l := range strings.Split(strings.TrimRight(got, "\n"), "\n") {
		assert.Equal(t, strings.TrimRight(l, " \t"), l,
			"no wrapped line may carry trailing whitespace, got %q", l)
	}
}

func TestSpliceNeverAltersBytesOutsideHeaderRegion(t *testing.T) {
	// Safety invariant: for an insert, the entire original content must appear
	// verbatim and contiguously somewhere in the output (only the header is added).
	ft, header := makeHeaderLF(t, "x.go")
	content := []byte("package main\n\n// pre-existing comment kept\nvar X = 1\n")

	out, _ := Splice(content, ft, header, model.DetectedHeader{Present: false})
	// Everything from the package decl onward must be byte-identical and contiguous.
	pkgIdx := strings.Index(string(out), "package main")
	require.NotEqual(t, -1, pkgIdx)
	assert.Equal(t, string(content), string(out[pkgIdx:]),
		"all original bytes from the insertion point survive unchanged")
}
