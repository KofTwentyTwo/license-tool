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

func TestIsCodingPragma(t *testing.T) {
	assert.True(t, IsCodingPragma("# -*- coding: utf-8 -*-"))
	assert.True(t, IsCodingPragma("# coding=latin-1"))
	assert.True(t, IsCodingPragma("# vim: set fileencoding=utf-8 :"))
	assert.False(t, IsCodingPragma("# just a comment"))
	assert.False(t, IsCodingPragma("not a hash line"))
}

func TestHasBOM(t *testing.T) {
	assert.True(t, hasBOM([]byte{0xEF, 0xBB, 0xBF, 'x'}))
	assert.False(t, hasBOM([]byte{0xEF, 0xBB}))
	assert.False(t, hasBOM([]byte("abc")))
}
