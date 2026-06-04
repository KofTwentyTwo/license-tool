package header

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/KofTwentyTwo/license-tool/internal/model"
)

func TestPreserveBoundary(t *testing.T) {
	hashFT := model.FileType{
		PreserveFirst: []model.PreserveRule{
			{Kind: model.PreserveBOM, Before: false},
			{Kind: model.PreserveShebang, Before: false},
			{Kind: model.PreserveCodingPragma, Before: false},
		},
	}
	xmlFT := model.FileType{
		PreserveFirst: []model.PreserveRule{
			{Kind: model.PreserveBOM, Before: false},
			{Kind: model.PreserveXMLDecl, Before: false},
			{Kind: model.PreservePHPOpen, Before: false},
		},
	}
	goFT := model.FileType{
		PreserveFirst: []model.PreserveRule{
			{Kind: model.PreserveBOM, Before: false},
			{Kind: model.PreserveGoBuildConstraint, Before: false},
		},
	}
	cssFT := model.FileType{
		PreserveFirst: []model.PreserveRule{
			{Kind: model.PreserveBOM, Before: false},
			{Kind: model.PreserveCSSCharset, Before: false},
		},
	}
	markupFT := model.FileType{
		PreserveFirst: []model.PreserveRule{
			{Kind: model.PreserveBOM, Before: false},
			{Kind: model.PreserveXMLDecl, Before: false},
			{Kind: model.PreserveDoctype, Before: false},
		},
	}
	beforeFT := model.FileType{
		PreserveFirst: []model.PreserveRule{
			{Kind: model.PreservePackageDecl, Before: true},
		},
	}

	tests := []struct {
		name    string
		content string
		ft      model.FileType
		want    string
	}{
		{
			name:    "no prefixes leaves boundary at zero",
			content: "package main\n",
			ft:      hashFT,
			want:    "package main\n",
		},
		{
			name:    "shebang is consumed universally",
			content: "#!/bin/sh\necho hi\n",
			ft:      model.FileType{},
			want:    "echo hi\n",
		},
		{
			name:    "final unterminated shebang line",
			content: "#!/bin/sh",
			ft:      model.FileType{},
			want:    "",
		},
		{
			name:    "BOM is consumed first",
			content: "\uFEFF#!/bin/sh\necho hi\n",
			ft:      hashFT,
			want:    "echo hi\n",
		},
		{
			name:    "BOM is not consumed without rule",
			content: "\uFEFFpackage main\n",
			ft:      beforeFT,
			want:    "\uFEFFpackage main\n",
		},
		{
			name:    "xml declaration is consumed",
			content: "<?xml version=\"1.0\"?>\n<root/>\n",
			ft:      xmlFT,
			want:    "<root/>\n",
		},
		{
			name:    "php open tag is consumed",
			content: "<?php\n// body\n",
			ft:      xmlFT,
			want:    "// body\n",
		},
		{
			name:    "coding pragma with colon form is consumed",
			content: "# -*- coding: utf-8 -*-\nprint(1)\n",
			ft:      hashFT,
			want:    "print(1)\n",
		},
		{
			name:    "coding pragma with equals form is consumed",
			content: "# coding=utf-8\nprint(1)\n",
			ft:      hashFT,
			want:    "print(1)\n",
		},
		{
			name:    "go build constraint is consumed with blank separator",
			content: "//go:build linux\n\npackage main\n",
			ft:      goFT,
			want:    "package main\n",
		},
		{
			name:    "go build constraint after BOM is consumed",
			content: "\uFEFF//go:build linux\n\npackage main\n",
			ft:      goFT,
			want:    "package main\n",
		},
		{
			name:    "go build constraint after shebang is consumed",
			content: "#!/usr/bin/env go\n//go:build linux\n\npackage main\n",
			ft:      goFT,
			want:    "package main\n",
		},
		{
			name:    "go build constraint with CRLF is consumed",
			content: "//go:build linux\r\n\r\npackage main\r\n",
			ft:      goFT,
			want:    "package main\r\n",
		},
		{
			name:    "legacy go build constraint is consumed",
			content: "// +build linux\n\npackage main\n",
			ft:      goFT,
			want:    "package main\n",
		},
		{
			name:    "combined go build constraints are consumed",
			content: "//go:build linux\n// +build linux\n\npackage main\n",
			ft:      goFT,
			want:    "package main\n",
		},
		{
			name:    "go build constraint tolerates missing blank separator",
			content: "//go:build linux\npackage main\n",
			ft:      goFT,
			want:    "package main\n",
		},
		{
			name:    "go build rule does not consume ordinary line comment",
			content: "// ordinary comment\npackage main\n",
			ft:      goFT,
			want:    "// ordinary comment\npackage main\n",
		},
		{
			name:    "go-looking line is not consumed without rule",
			content: "//go:build linux\n\npackage main\n",
			ft:      model.FileType{},
			want:    "//go:build linux\n\npackage main\n",
		},
		{
			name:    "css charset is consumed",
			content: "@charset \"UTF-8\";\nbody {}\n",
			ft:      cssFT,
			want:    "body {}\n",
		},
		{
			name:    "css charset after BOM is consumed",
			content: "\uFEFF@charset \"UTF-8\";\nbody {}\n",
			ft:      cssFT,
			want:    "body {}\n",
		},
		{
			name:    "css charset is case insensitive",
			content: "@CHARSET \"UTF-8\";\nbody {}\n",
			ft:      cssFT,
			want:    "body {}\n",
		},
		{
			name:    "css-looking line is not consumed without rule",
			content: "@charset \"UTF-8\";\nbody {}\n",
			ft:      model.FileType{},
			want:    "@charset \"UTF-8\";\nbody {}\n",
		},
		{
			name:    "doctype is consumed",
			content: "<!DOCTYPE html>\n<html></html>\n",
			ft:      markupFT,
			want:    "<html></html>\n",
		},
		{
			name:    "doctype after BOM is consumed",
			content: "\uFEFF<!DOCTYPE html>\n<html></html>\n",
			ft:      markupFT,
			want:    "<html></html>\n",
		},
		{
			name:    "doctype is case insensitive",
			content: "<!doctype html>\n<html></html>\n",
			ft:      markupFT,
			want:    "<html></html>\n",
		},
		{
			name:    "xml declaration then doctype are consumed",
			content: "<?xml version=\"1.0\"?>\n<!DOCTYPE svg>\n<svg/>\n",
			ft:      markupFT,
			want:    "<svg/>\n",
		},
		{
			name:    "doctype-looking line is not consumed without rule",
			content: "<!DOCTYPE html>\n<html></html>\n",
			ft:      model.FileType{},
			want:    "<!DOCTYPE html>\n<html></html>\n",
		},
		{
			name:    "coding pragma is not consumed without rule",
			content: "# coding=utf-8\nprint(1)\n",
			ft:      model.FileType{},
			want:    "# coding=utf-8\nprint(1)\n",
		},
		{
			name:    "before rule is never consumed",
			content: "package main\nfunc main(){}\n",
			ft:      beforeFT,
			want:    "package main\nfunc main(){}\n",
		},
		{
			name:    "non-prefix line stops consumption",
			content: "#!/bin/sh\necho hi\n# coding=utf-8\n",
			ft:      hashFT,
			want:    "echo hi\n# coding=utf-8\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			content := []byte(tc.content)
			got := PreserveBoundary(content, tc.ft)
			assert.Equal(t, tc.want, string(content[got:]))
		})
	}
}

func TestHasRule(t *testing.T) {
	ftBOMAfter := model.FileType{
		PreserveFirst: []model.PreserveRule{
			{Kind: model.PreserveShebang, Before: false},
			{Kind: model.PreserveBOM, Before: false},
		},
	}
	ftPackageBefore := model.FileType{
		PreserveFirst: []model.PreserveRule{
			{Kind: model.PreservePackageDecl, Before: true},
		},
	}
	cases := []struct {
		name   string
		ft     model.FileType
		kind   model.PreserveKind
		before bool
		want   bool
	}{
		{"matches kind and before", ftBOMAfter, model.PreserveBOM, false, true},
		{"kind present but before mismatches", ftBOMAfter, model.PreserveBOM, true, false},
		{"kind absent entirely", ftPackageBefore, model.PreserveBOM, false, false},
		{"empty rule list", model.FileType{}, model.PreserveShebang, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, HasRule(c.ft, c.kind, c.before))
		})
	}
}

func TestLineEndOffset(t *testing.T) {
	content := []byte("a\nbc")
	assert.Equal(t, 2, LineEndOffset(content, 0))
	assert.Equal(t, len(content), LineEndOffset(content, 2))
	assert.Equal(t, len(content), LineEndOffset(content, len(content)))
}

func TestGoBuildConstraintBoundary(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    string
		wantOK  bool
	}{
		{
			name:    "go build form",
			content: "//go:build linux\n\npackage main\n",
			want:    "package main\n",
			wantOK:  true,
		},
		{
			name:    "only one blank separator is consumed",
			content: "//go:build linux\n\n\npackage main\n",
			want:    "\npackage main\n",
			wantOK:  true,
		},
		{
			name:    "legacy plus build form",
			content: "// +build linux\n\npackage main\n",
			want:    "package main\n",
			wantOK:  true,
		},
		{
			name:    "ordinary line comment",
			content: "// not a constraint\npackage main\n",
			want:    "// not a constraint\npackage main\n",
			wantOK:  false,
		},
		{
			name:    "empty content",
			content: "",
			want:    "",
			wantOK:  false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			content := []byte(c.content)
			got, ok := GoBuildConstraintBoundary(content, 0)
			assert.Equal(t, c.wantOK, ok)
			assert.Equal(t, c.want, string(content[got:]))
		})
	}
}

func TestIsCodingPragma(t *testing.T) {
	assert.True(t, IsCodingPragma("# -*- coding: utf-8 -*-"))
	assert.True(t, IsCodingPragma("# coding=latin-1"))
	assert.True(t, IsCodingPragma("# vim: set fileencoding=utf-8 :"))
	assert.False(t, IsCodingPragma("# just a comment"))
	assert.False(t, IsCodingPragma("not a hash line"))
}

func TestIsCSSCharset(t *testing.T) {
	assert.True(t, IsCSSCharset("@charset \"UTF-8\";"))
	assert.True(t, IsCSSCharset(" @CHARSET \"UTF-8\";"))
	assert.False(t, IsCSSCharset("body { color: black; }"))
}

func TestIsDoctype(t *testing.T) {
	assert.True(t, IsDoctype("<!DOCTYPE html>"))
	assert.True(t, IsDoctype(" <!doctype html>"))
	assert.False(t, IsDoctype("<html></html>"))
}

func TestHasBOM(t *testing.T) {
	assert.True(t, hasBOM([]byte{0xEF, 0xBB, 0xBF, 'x'}))
	assert.False(t, hasBOM([]byte{0xEF, 0xBB}))
	assert.False(t, hasBOM([]byte("abc")))
}
