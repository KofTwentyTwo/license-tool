package detect

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/KofTwentyTwo/license-tool/internal/model"
)

// --- file-type fixtures -----------------------------------------------------

// blockFT is a /* ... */ block-comment type with no preserve-first rules.
func blockFT() model.FileType {
	return model.FileType{
		Name:         "C",
		Extensions:   []string{".c"},
		CommentStyle: model.CommentStyle{Block: true, Open: "/*", Close: "*/"},
	}
}

// lineFT is a "// " line-comment type with a package-decl (Before) rule, which
// PreserveBoundary must never consume.
func lineFT() model.FileType {
	return model.FileType{
		Name:         "Go",
		Extensions:   []string{".go"},
		CommentStyle: model.CommentStyle{Block: false, LinePrefix: "// "},
		PreserveFirst: []model.PreserveRule{
			{Kind: model.PreservePackageDecl, Before: true},
		},
	}
}

// hashFT is a "# " line-comment type that preserves shebang, BOM and coding pragma.
func hashFT() model.FileType {
	return model.FileType{
		Name:         "Python",
		Extensions:   []string{".py"},
		CommentStyle: model.CommentStyle{Block: false, LinePrefix: "# "},
		PreserveFirst: []model.PreserveRule{
			{Kind: model.PreserveBOM, Before: false},
			{Kind: model.PreserveShebang, Before: false},
			{Kind: model.PreserveCodingPragma, Before: false},
		},
	}
}

// cFT is a block-comment type (C/C++ shape) that lists NO shebang rule -- only BOM.
// It exists to prove the universal-shebang fix: PreserveBoundary must consume a leading
// "#!" even though this type never lists PreserveShebang.
func cFT() model.FileType {
	return model.FileType{
		Name:         "C",
		Extensions:   []string{".c"},
		CommentStyle: model.CommentStyle{Block: true, Open: "/*", Close: "*/"},
		PreserveFirst: []model.PreserveRule{
			{Kind: model.PreserveBOM, Before: false},
		},
	}
}

// phpFT is a block-comment type that preserves a (universal) shebang then the <?php
// open tag, mirroring the builtin PHP table ordering.
func phpFT() model.FileType {
	return model.FileType{
		Name:         "PHP",
		Extensions:   []string{".php"},
		CommentStyle: model.CommentStyle{Block: true, Open: "/*", Close: "*/"},
		PreserveFirst: []model.PreserveRule{
			{Kind: model.PreserveBOM, Before: false},
			{Kind: model.PreserveShebang, Before: false},
			{Kind: model.PreservePHPOpen, Before: false},
		},
	}
}

// xmlFT is a block-comment type that preserves an XML declaration and PHP open tag.
func xmlFT() model.FileType {
	return model.FileType{
		Name:         "XMLish",
		Extensions:   []string{".xml"},
		CommentStyle: model.CommentStyle{Block: true, Open: "<!--", Close: "-->"},
		PreserveFirst: []model.PreserveRule{
			{Kind: model.PreserveXMLDecl, Before: false},
			{Kind: model.PreservePHPOpen, Before: false},
		},
	}
}

// --- Detect: top-level recognizers and guards -------------------------------

func TestDetect(t *testing.T) {
	// Verbatim Unlicense text: licensecheck recognizes it at 100% but it matches no
	// curated fingerprint phrase, so it exercises the licensecheck backstop path.
	const unlicenseText = `This is free and unencumbered software released into the public domain.

Anyone is free to copy, modify, publish, use, compile, sell, or distribute this software, either in source code form or as a compiled binary, for any purpose, commercial or non-commercial, and by any means.

In jurisdictions that recognize copyright laws, the author or authors of this software dedicate any and all copyright interest in the software to the public domain. We make this dedication for the benefit of the public at large and to the detriment of our heirs and
successors. We intend this dedication to be an overt act of relinquishment in perpetuity of all present and future rights to this software under copyright law.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.

For more information, please refer to <http://unlicense.org/>`

	tests := []struct {
		name         string
		content      string
		ft           model.FileType
		wantPresent  bool
		wantSPDX     string
		wantHolder   string
		wantYear     string
		wantVia      bool
		skipIdentity bool // assert only Present + SPDXID (holder/year incidental)
	}{
		{
			name:        "skip type never carries a header",
			content:     "// SPDX-License-Identifier: MIT\n",
			ft:          model.FileType{Name: "JSON", Skip: true},
			wantPresent: false,
		},
		{
			name:        "no leading comment means no header",
			content:     "package main\n\nfunc main() {}\n",
			ft:          lineFT(),
			wantPresent: false,
		},
		{
			name: "sentinel is highest confidence",
			content: "// " + Sentinel + "\n" +
				"// Copyright (C) 2021 Acme Inc.\n" +
				"\npackage main\n",
			ft:          lineFT(),
			wantPresent: true,
			wantVia:     true,
			wantHolder:  "Acme Inc",
			wantYear:    "2021",
		},
		{
			name: "spdx tag sets id and holder/year",
			content: "/*\n" +
				" * SPDX-FileCopyrightText: 2019-2024 Globex Corporation\n" +
				" * SPDX-License-Identifier: Apache-2.0\n" +
				" */\n\npackage body\n",
			ft:          blockFT(),
			wantPresent: true,
			wantSPDX:    "Apache-2.0",
			wantHolder:  "Globex Corporation",
			wantYear:    "2019-2024",
		},
		{
			// GPL-unique wording: the run "terms of the GNU General Public License as"
			// is absent from AGPL's header (its "Affero" splits it), so this matches
			// GPL-3.0-or-later via the standard-header recognizer. We deliberately omit
			// the AGPL-shared "This program is free software..." opening sentence so
			// AGPL (checked first) does not claim the match.
			name: "standard header match wins over fingerprint",
			content: "/*\n" +
				" * Copyright (C) 2020 Initech\n" +
				" * Distributed under the terms of the GNU General Public License as\n" +
				" * published by, see the upstream project for the full grant text.\n" +
				" */\n\npackage body\n",
			ft:          blockFT(),
			wantPresent: true,
			wantSPDX:    "GPL-3.0-or-later",
			wantHolder:  "Initech",
			wantYear:    "2020",
		},
		{
			name: "curated fingerprint match (MIT, no standard header)",
			content: "// Copyright 2022 Wonka\n" +
				"// Permission is hereby granted, free of charge, to any person obtaining a copy\n" +
				"// of this software and associated documentation files.\n" +
				"\npackage main\n",
			ft:          lineFT(),
			wantPresent: true,
			wantSPDX:    "MIT",
			wantHolder:  "Wonka",
			wantYear:    "2022",
		},
		{
			name:         "licensecheck backstop recognizes verbatim Unlicense text",
			content:      "/*\n" + unlicenseText + "\n*/\n\npackage body\n",
			ft:           blockFT(),
			wantPresent:  true,
			wantSPDX:     "Unlicense",
			skipIdentity: true, // Unlicense body mentions "copyright interest" incidentally
		},
		{
			name: "non-license doc comment is rejected, never edited",
			content: "// Package widget renders widgets for the dashboard.\n" +
				"// It exposes a single Render entry point.\n" +
				"\npackage widget\n",
			ft:          lineFT(),
			wantPresent: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Detect([]byte(tc.content), tc.ft)
			require.NoError(t, err)
			assert.Equal(t, tc.wantPresent, got.Present, "Present")
			if !tc.wantPresent {
				assert.Equal(t, model.DetectedHeader{}, got, "absent header is zero value")
				return
			}
			assert.Equal(t, tc.wantSPDX, got.SPDXID, "SPDXID")
			assert.Greater(t, got.EndByte, got.StartByte, "non-empty span")
			if tc.skipIdentity {
				return
			}
			assert.Equal(t, tc.wantHolder, got.Holder, "Holder")
			assert.Equal(t, tc.wantYear, got.Year, "Year")
			assert.Equal(t, tc.wantVia, got.ViaSentinel, "ViaSentinel")
		})
	}
}

// TestDetectSpanCoversCommentRegion confirms the reported span begins at the
// comment opener and includes the trailing blank line separating it from the body.
func TestDetectSpanCoversCommentRegion(t *testing.T) {
	content := []byte("// " + Sentinel + "\n// Copyright 2021 Acme\n\npackage main\n")
	got, err := Detect(content, lineFT())
	require.NoError(t, err)
	require.True(t, got.Present)
	assert.Equal(t, 0, got.StartByte)
	// Span ends after the blank line, at "package".
	assert.Equal(t, "package main\n", string(content[got.EndByte:]))
}

// TestDetectPreserveBoundaryError covers Detect's preserve-boundary error guard via
// the preserveBoundaryFn seam. The real PreserveBoundary never errors, so the guard
// is otherwise unreachable; injecting an error confirms Detect propagates it and
// returns the zero header.
func TestDetectPreserveBoundaryError(t *testing.T) {
	boom := errors.New("synthetic preserve-boundary failure")
	orig := preserveBoundaryFn
	t.Cleanup(func() { preserveBoundaryFn = orig })
	preserveBoundaryFn = func([]byte, model.FileType) (int, error) {
		return 0, boom
	}

	got, err := Detect([]byte("// "+Sentinel+"\npackage main\n"), lineFT())
	require.ErrorIs(t, err, boom)
	assert.Equal(t, model.DetectedHeader{}, got)
}

// --- PreserveBoundary --------------------------------------------------------

func TestPreserveBoundary(t *testing.T) {
	tests := []struct {
		name    string
		content string
		ft      model.FileType
		// wantRest is the content from the returned boundary to EOF.
		wantRest string
	}{
		{
			name:     "no prefixes leaves boundary at zero",
			content:  "package main\n",
			ft:       lineFT(),
			wantRest: "package main\n",
		},
		{
			name:     "shebang is consumed",
			content:  "#!/usr/bin/env python\nprint(1)\n",
			ft:       hashFT(),
			wantRest: "print(1)\n",
		},
		{
			name:     "shebang then coding pragma stack",
			content:  "#!/usr/bin/env python\n# -*- coding: utf-8 -*-\nprint(1)\n",
			ft:       hashFT(),
			wantRest: "print(1)\n",
		},
		{
			name:     "coding pragma with equals form",
			content:  "# coding=utf-8\nprint(1)\n",
			ft:       hashFT(),
			wantRest: "print(1)\n",
		},
		{
			name:     "BOM is consumed first",
			content:  "\uFEFF#!/bin/sh\necho hi\n",
			ft:       hashFT(),
			wantRest: "echo hi\n",
		},
		{
			name:     "xml declaration is consumed",
			content:  "<?xml version=\"1.0\"?>\n<root/>\n",
			ft:       xmlFT(),
			wantRest: "<root/>\n",
		},
		{
			name:     "php open tag is consumed",
			content:  "<?php\n// body\n",
			ft:       xmlFT(),
			wantRest: "// body\n",
		},
		{
			// Regression: a block-comment type that does NOT list PreserveShebang must
			// still have its leading "#!" consumed, so the header lands below the shebang.
			name:     "shebang consumed on block type without a shebang rule",
			content:  "#!/usr/bin/tcc -run\nint main(void){return 0;}\n",
			ft:       cFT(),
			wantRest: "int main(void){return 0;}\n",
		},
		{
			// Regression: PHP CLI script -- the universal shebang AND the <?php open tag
			// are both consumed, in file order, so the header lands after line 2.
			name:     "php shebang then php-open both consumed",
			content:  "#!/usr/bin/env php\n<?php\necho 'hi';\n",
			ft:       phpFT(),
			wantRest: "echo 'hi';\n",
		},
		{
			// The universal rule is inert without a shebang: a block-comment file whose
			// first line is ordinary code keeps the boundary at zero.
			name:     "no shebang leaves block-type boundary at zero",
			content:  "int main(void){return 0;}\n",
			ft:       cFT(),
			wantRest: "int main(void){return 0;}\n",
		},
		{
			name:     "package decl (Before rule) is never consumed",
			content:  "package main\nfunc main(){}\n",
			ft:       lineFT(),
			wantRest: "package main\nfunc main(){}\n",
		},
		{
			name:     "non-prefix line stops consumption immediately",
			content:  "print(1)\n#!/not/a/shebang/now\n",
			ft:       hashFT(),
			wantRest: "print(1)\n#!/not/a/shebang/now\n",
		},
		{
			name:     "final unterminated shebang line",
			content:  "#!/bin/sh",
			ft:       hashFT(),
			wantRest: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			content := []byte(tc.content)
			pos, err := PreserveBoundary(content, tc.ft)
			require.NoError(t, err)
			assert.Equal(t, tc.wantRest, string(content[pos:]))
		})
	}
}

// --- FingerprintLicense ------------------------------------------------------

func TestFingerprintLicense(t *testing.T) {
	const unlicenseText = `This is free and unencumbered software released into the public domain.

Anyone is free to copy, modify, publish, use, compile, sell, or distribute this software, either in source code form or as a compiled binary, for any purpose, commercial or non-commercial, and by any means.

In jurisdictions that recognize copyright laws, the author or authors of this software dedicate any and all copyright interest in the software to the public domain. We make this dedication for the benefit of the public at large and to the detriment of our heirs and
successors. We intend this dedication to be an overt act of relinquishment in perpetuity of all present and future rights to this software under copyright law.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.

For more information, please refer to <http://unlicense.org/>`

	tests := []struct {
		name   string
		text   string
		wantOK bool
		wantID string
	}{
		{
			name:   "curated AGPL phrase",
			text:   "Licensed under the GNU Affero General Public License as published by the Free Software Foundation.",
			wantOK: true,
			wantID: "AGPL-3.0-or-later",
		},
		{
			name:   "curated Apache phrase, case-insensitive",
			text:   "LICENSED UNDER THE APACHE LICENSE, VERSION 2.0",
			wantOK: true,
			wantID: "Apache-2.0",
		},
		{
			name:   "licensecheck backstop for verbatim Unlicense",
			text:   unlicenseText,
			wantOK: true,
			wantID: "Unlicense",
		},
		{
			name:   "ordinary prose mentioning a license name does not match",
			text:   "This file talks about the MIT license but is not a license notice.",
			wantOK: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			id, ok := FingerprintLicense(tc.text)
			assert.Equal(t, tc.wantOK, ok)
			if tc.wantOK {
				assert.Equal(t, tc.wantID, id)
			}
		})
	}
}

// --- leadingCommentRegion / block & line regions ----------------------------

func TestLeadingCommentRegionBlock(t *testing.T) {
	cs := model.CommentStyle{Block: true, Open: "/*", Close: "*/"}
	ft := model.FileType{CommentStyle: cs}

	t.Run("block with trailing LF newline", func(t *testing.T) {
		content := []byte("/* hello\n * world\n */\nbody\n")
		s, e, text, ok := leadingCommentRegion(content, 0, ft)
		require.True(t, ok)
		assert.Equal(t, 0, s)
		assert.Equal(t, "body\n", string(content[e:]))
		assert.Contains(t, text, "hello")
		assert.Contains(t, text, "world")
	})

	t.Run("block with leading blank lines and indentation", func(t *testing.T) {
		content := []byte("\n  \n   /* indented header */\nbody\n")
		s, e, _, ok := leadingCommentRegion(content, 0, ft)
		require.True(t, ok)
		// Leading blank lines (4 bytes) are skipped and the block's indentation (3
		// spaces) is also stepped past, so the span starts at the "/*" opener.
		assert.Equal(t, 7, s)
		assert.Equal(t, byte('/'), content[s])
		assert.Equal(t, "body\n", string(content[e:]))
	})

	t.Run("block with CRLF terminator after close", func(t *testing.T) {
		content := []byte("/* hi */\r\nbody\r\n")
		_, e, _, ok := leadingCommentRegion(content, 0, ft)
		require.True(t, ok)
		assert.Equal(t, "body\r\n", string(content[e:]))
	})

	t.Run("block ending exactly at EOF (no trailing newline)", func(t *testing.T) {
		content := []byte("/* only a comment */")
		_, e, _, ok := leadingCommentRegion(content, 0, ft)
		require.True(t, ok)
		assert.Equal(t, len(content), e)
	})

	t.Run("not a block comment", func(t *testing.T) {
		content := []byte("code();\n")
		_, _, _, ok := leadingCommentRegion(content, 0, ft)
		assert.False(t, ok)
	})

	t.Run("unterminated block comment", func(t *testing.T) {
		content := []byte("/* never closed\nmore text\n")
		_, _, _, ok := leadingCommentRegion(content, 0, ft)
		assert.False(t, ok)
	})
}

func TestLeadingCommentRegionLine(t *testing.T) {
	cs := model.CommentStyle{Block: false, LinePrefix: "// "}
	ft := model.FileType{CommentStyle: cs}

	t.Run("run of comment lines with tolerated blank between", func(t *testing.T) {
		content := []byte("// line one\n//\n// line three\nbody\n")
		s, e, text, ok := leadingCommentRegion(content, 0, ft)
		require.True(t, ok)
		assert.Equal(t, 0, s)
		assert.Equal(t, "body\n", string(content[e:]))
		assert.Contains(t, text, "line one")
		assert.Contains(t, text, "line three")
	})

	t.Run("blank line then comment is still one region", func(t *testing.T) {
		content := []byte("// header part A\n\n// header part B\ncode\n")
		_, e, text, ok := leadingCommentRegion(content, 0, ft)
		require.True(t, ok)
		assert.Equal(t, "code\n", string(content[e:]))
		assert.Contains(t, text, "part A")
		assert.Contains(t, text, "part B")
	})

	t.Run("non-comment line ends the run", func(t *testing.T) {
		content := []byte("// header\ncode();\n// trailing comment\n")
		_, e, _, ok := leadingCommentRegion(content, 0, ft)
		require.True(t, ok)
		assert.Equal(t, "code();\n// trailing comment\n", string(content[e:]))
	})

	t.Run("no comment at all", func(t *testing.T) {
		content := []byte("code();\nmore();\n")
		_, _, _, ok := leadingCommentRegion(content, 0, ft)
		assert.False(t, ok)
	})

	t.Run("final comment line without trailing newline", func(t *testing.T) {
		content := []byte("// just one line")
		_, e, _, ok := leadingCommentRegion(content, 0, ft)
		require.True(t, ok)
		assert.Equal(t, len(content), e)
	})
}

// TestLineCommentRegionEmptyPrefixFallback drives the prefix-normalization branch
// where the configured LinePrefix is whitespace-only: TrimRight yields "", so the
// fallback TrimSpace path runs. With an empty prefix every line matches as a comment.
func TestLineCommentRegionEmptyPrefixFallback(t *testing.T) {
	cs := model.CommentStyle{Block: false, LinePrefix: "   "}
	content := []byte("anything\nat all\n")
	s, e, _, ok := lineCommentRegion(content, 0, cs)
	require.True(t, ok)
	assert.Equal(t, 0, s)
	assert.Equal(t, len(content), e)
}

// --- spdxIdentifierTag -------------------------------------------------------

func TestSpdxIdentifierTag(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		wantID string
		wantOK bool
	}{
		{
			name:   "simple id with trailing newline",
			text:   "SPDX-License-Identifier: MIT\nmore",
			wantID: "MIT",
			wantOK: true,
		},
		{
			name:   "id at end of text without newline",
			text:   "SPDX-License-Identifier: Apache-2.0",
			wantID: "Apache-2.0",
			wantOK: true,
		},
		{
			name:   "multi-license expression returned as-is",
			text:   "spdx-license-identifier: (MIT OR Apache-2.0)\n",
			wantID: "(MIT OR Apache-2.0)",
			wantOK: true,
		},
		{
			name:   "no tag present",
			text:   "just a normal comment",
			wantOK: false,
		},
		{
			name:   "tag present but empty value",
			text:   "SPDX-License-Identifier:   \nnext line",
			wantOK: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			id, ok := spdxIdentifierTag(tc.text)
			assert.Equal(t, tc.wantOK, ok)
			if tc.wantOK {
				assert.Equal(t, tc.wantID, id)
			}
		})
	}
}

// --- matchStandardHeader / runMatches ---------------------------------------

func TestMatchStandardHeader(t *testing.T) {
	t.Run("matches GPL standard header run", func(t *testing.T) {
		// GPL-unique runs only: every 8-token run here contains "gnu general" (no
		// "affero"), and the text stops before the AGPL-shared "...the Free Software
		// Foundation, either version 3..." tail, so AGPL (checked first) shares no run.
		comment := "Distributed under the terms of the GNU General Public License as " +
			"published by, see the upstream project for the full grant text."
		id, ok := matchStandardHeader(comment)
		require.True(t, ok)
		assert.Equal(t, "GPL-3.0-or-later", id)
	})

	t.Run("matches MPL standard header run", func(t *testing.T) {
		comment := "This Source Code Form is subject to the terms of the Mozilla Public " +
			"License, if a copy of the was not distributed with this file You can obtain one."
		id, ok := matchStandardHeader(comment)
		require.True(t, ok)
		assert.Equal(t, "MPL-2.0", id)
	})

	t.Run("too few significant tokens", func(t *testing.T) {
		_, ok := matchStandardHeader("short comment here")
		assert.False(t, ok)
	})

	t.Run("enough tokens but no header run match", func(t *testing.T) {
		comment := "the quick brown fox jumps over the lazy dog and then keeps running along"
		_, ok := matchStandardHeader(comment)
		assert.False(t, ok)
	})
}

func TestRunMatches(t *testing.T) {
	// A header with fewer than standardHeaderMatchTokens significant tokens never matches.
	short := "one two three"
	assert.False(t, runMatches(" one two three ", short))
}

// --- extractHolder -----------------------------------------------------------

func TestExtractHolder(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{
			name: "REUSE FileCopyrightText line",
			text: "SPDX-FileCopyrightText: 2024 Stark Industries",
			want: "Stark Industries",
		},
		{
			name: "conventional Copyright with (C) and year",
			text: "Copyright (C) 2020 Wayne Enterprises",
			want: "Wayne Enterprises",
		},
		{
			name: "Copyright with year range and trailing period",
			text: "Copyright 2018-2022 Umbrella Corp.",
			want: "Umbrella Corp",
		},
		{
			name: "lowercase (c) marker",
			text: "copyright (c) 2019 Cyberdyne Systems",
			want: "Cyberdyne Systems",
		},
		{
			name: "no copyright line at all",
			text: "This is free software with no holder line.",
			want: "",
		},
		{
			name: "copyright keyword but empty holder after decoration",
			text: "Copyright (C) 2021",
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, extractHolder(tc.text))
		})
	}
}

// --- extractYear -------------------------------------------------------------

func TestExtractYear(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{
			name: "single year on copyright line",
			text: "Copyright 2021 Acme",
			want: "2021",
		},
		{
			name: "year range on copyright line",
			text: "Copyright (C) 2019-2024 Globex",
			want: "2019-2024",
		},
		{
			name: "no copyright keyword but year present (fallback path)",
			text: "(C) 2017 Some Holder\nCopyRiGhT marker on another line",
			want: "2017",
		},
		{
			name: "no year anywhere",
			text: "Copyright Acme with no digits",
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, extractYear(tc.text))
		})
	}
}

// --- small helpers -----------------------------------------------------------

func TestHasRule(t *testing.T) {
	ft := lineFT()
	assert.True(t, hasRule(ft, model.PreservePackageDecl, true))
	assert.False(t, hasRule(ft, model.PreservePackageDecl, false))
	assert.False(t, hasRule(ft, model.PreserveShebang, false))
}

func TestIsCodingPragma(t *testing.T) {
	assert.True(t, isCodingPragma("# -*- coding: utf-8 -*-"))
	assert.True(t, isCodingPragma("# coding=latin-1"))
	assert.False(t, isCodingPragma("# just a comment"))
	assert.False(t, isCodingPragma("not a hash line"))
}

func TestStripBlockInner(t *testing.T) {
	in := "\n * line one\r\n * line two\n"
	out := stripBlockInner(in)
	assert.Equal(t, "\nline one\nline two\n", out)
}

func TestNormalizeWhitespace(t *testing.T) {
	assert.Equal(t, "a b c", normalizeWhitespace("  a\t b\n\n c  "))
	assert.Equal(t, "", normalizeWhitespace("   \n\t "))
}

func TestSignificantTokens(t *testing.T) {
	// Bracketed/angled/braced placeholders are dropped entirely (including nested),
	// punctuation becomes a separator, and digits are kept as word runes.
	got := significantTokens("Copyright [year <nested>] Foo-Bar v2, {drop me} End")
	assert.Equal(t, []string{"copyright", "foo", "bar", "v2", "end"}, got)
}

func TestSignificantTokensUnbalancedCloser(t *testing.T) {
	// A stray closing bracket with depth already zero must not underflow depth.
	got := significantTokens("abc ] def")
	assert.Equal(t, []string{"abc", "def"}, got)
}

func TestToLowerRune(t *testing.T) {
	assert.Equal(t, 'a', toLowerRune('A'))
	assert.Equal(t, 'z', toLowerRune('z'))
	assert.Equal(t, '5', toLowerRune('5'))
}

func TestIsWordRune(t *testing.T) {
	assert.True(t, isWordRune('a'))
	assert.True(t, isWordRune('Z'))
	assert.True(t, isWordRune('0'))
	assert.False(t, isWordRune('-'))
	assert.False(t, isWordRune(' '))
}

func TestStripCopyrightDecoration(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"colon then copyright symbol then year", ": © 2021 Holder Name", "Holder Name"},
		{"plain holder no decoration", " Just A Name ", "Just A Name"},
		{"only a year leaves empty", " 2020 ", ""},
		{"trailing period trimmed", "(c) 1999 Holder.", "Holder"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, stripCopyrightDecoration(tc.in))
		})
	}
}

func TestFindYearSpan(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"single year", "in 2021 something", "2021"},
		{"year range", "from 2019-2024 here", "2019-2024"},
		{"range with non-four-digit tail falls back to single", "2020-99 partial", "2020"},
		{"three-digit run is skipped", "year 999 then 2022", "2022"},
		{"five-digit run is skipped", "12345 then 2001", "2001"},
		{"no digits", "no year here", ""},
		{"only non-year digit runs", "12 345 6", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, findYearSpan(tc.in))
		})
	}
}

func TestIsYearToken(t *testing.T) {
	assert.True(t, isYearToken("2021"))
	assert.True(t, isYearToken("2019-2024"))
	assert.True(t, isYearToken("2021,"))
	assert.False(t, isYearToken("Acme"))
	assert.False(t, isYearToken(""))
	assert.False(t, isYearToken("999"))
}

func TestIsDigit(t *testing.T) {
	assert.True(t, isDigit('0'))
	assert.True(t, isDigit('9'))
	assert.False(t, isDigit('a'))
}

func TestLineEndOffset(t *testing.T) {
	content := []byte("ab\ncd")
	assert.Equal(t, 3, lineEndOffset(content, 0))
	assert.Equal(t, len(content), lineEndOffset(content, 3))
}

func TestConsumeTrailingBlankLines(t *testing.T) {
	content := []byte("  \n\t\nreal\n")
	pos := consumeTrailingBlankLines(content, 0)
	assert.Equal(t, "real\n", string(content[pos:]))

	// All-blank to EOF returns end of content.
	allBlank := []byte("  \n\n")
	assert.Equal(t, len(allBlank), consumeTrailingBlankLines(allBlank, 0))
}
