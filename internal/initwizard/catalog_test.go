package initwizard

import (
	"testing"

	"github.com/KofTwentyTwo/license-tool/internal/config"
	"github.com/KofTwentyTwo/license-tool/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCatalogContainsExactlySupportedLanguageFamiliesInStableOrder(t *testing.T) {
	got := Catalog()
	require.Len(t, got, 15)

	want := []LanguageFamily{
		LanguageTypeScriptJavaScript,
		LanguagePython,
		LanguageJava,
		LanguageGo,
		LanguageC,
		LanguageCPlusPlus,
		LanguageCSharp,
		LanguagePHP,
		LanguageRuby,
		LanguageSwift,
		LanguageKotlin,
		LanguageRust,
		LanguageShell,
		LanguagePowerShell,
		LanguageR,
	}
	for i, family := range want {
		assert.Equal(t, family, got[i].Language)
		assert.NotEmpty(t, got[i].Path)
		assert.NotEmpty(t, got[i].Source)
	}
}

func TestCatalogReturnsACopy(t *testing.T) {
	first := Catalog()
	require.NotEmpty(t, first)
	first[0].Language = LanguagePython

	second := Catalog()
	assert.Equal(t, LanguageTypeScriptJavaScript, second[0].Language)
}

func TestSelectSamplePrefersDetectedCatalogFamiliesInCatalogOrder(t *testing.T) {
	got := SelectSample([]string{
		"cmd/license-tool/main.go",
		"web/src/app.ts",
	})

	assert.Equal(t, LanguageTypeScriptJavaScript, got.Language)
	assert.Equal(t, "example.ts", got.Path)
}

func TestSelectSampleFallsBackToCWhenNoCatalogLanguageIsDetected(t *testing.T) {
	got := SelectSample([]string{
		"README.md",
		"package.json",
		"styles/app.css",
	})

	assert.Equal(t, LanguageC, got.Language)
	assert.Equal(t, "example.c", got.Path)
}

func TestSelectSampleSplitsCAndCPlusPlusExtensions(t *testing.T) {
	cases := []struct {
		name string
		path string
		want LanguageFamily
	}{
		{name: "c source", path: "src/main.c", want: LanguageC},
		{name: "c header", path: "include/project.h", want: LanguageC},
		{name: "cpp source", path: "src/main.cpp", want: LanguageCPlusPlus},
		{name: "cpp header", path: "include/project.hpp", want: LanguageCPlusPlus},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SelectSample([]string{tc.path})
			assert.Equal(t, tc.want, got.Language)
		})
	}
}

func TestSelectSampleDetectsCSharp(t *testing.T) {
	got := SelectSample([]string{"src/Program.cs"})

	assert.Equal(t, LanguageCSharp, got.Language)
	assert.Equal(t, "Example.cs", got.Path)
}

func TestBuildSourcePreviewAppliesGeneratedHeaderToSelectedSample(t *testing.T) {
	sample := SelectSample([]string{"cmd/license-tool/main.go"})
	preview, err := BuildSourcePreview(SourcePreviewInput{
		Config: model.Config{
			License: "MIT",
			Holder:  "Acme, Inc.",
			Style:   model.StyleReuse,
		},
		Sample:       sample,
		ResolvedYear: "2026",
	})
	require.NoError(t, err)

	assert.Equal(t, LanguageGo, preview.Language)
	assert.Equal(t, "example.go", preview.Path)
	assert.Contains(t, preview.Content, "SPDX-FileCopyrightText: 2026 Acme, Inc.")
	assert.Contains(t, preview.Content, "SPDX-License-Identifier: MIT")
	assert.Contains(t, preview.Content, "package main")
	assert.Less(t,
		indexOf(t, preview.Content, "SPDX-License-Identifier: MIT"),
		indexOf(t, preview.Content, "package main"),
	)
}

func TestBuildSourcePreviewUsesCSharpFileType(t *testing.T) {
	sample := SelectSample([]string{"src/Program.cs"})
	preview, err := BuildSourcePreview(SourcePreviewInput{
		Config: model.Config{
			License: "MIT",
			Holder:  "Acme, Inc.",
			Style:   model.StyleReuse,
		},
		Sample:       sample,
		ResolvedYear: "2026",
	})
	require.NoError(t, err)

	assert.Equal(t, LanguageCSharp, preview.Language)
	assert.Contains(t, preview.Content, "/*")
	assert.Contains(t, preview.Content, "*/")
	assert.Contains(t, preview.Content, "public class Example")
}

func TestBuildSourcePreviewEdgeCases(t *testing.T) {
	t.Run("path with no extension has no sample", func(t *testing.T) {
		sample, ok := SampleForPath("LICENSE")
		assert.False(t, ok)
		assert.Equal(t, Sample{}, sample)
	})

	t.Run("unknown family has no sample", func(t *testing.T) {
		sample, ok := sampleForFamily(LanguageFamily("Unknown"))
		assert.False(t, ok)
		assert.Equal(t, Sample{}, sample)
	})

	t.Run("empty sample falls back to C", func(t *testing.T) {
		preview, err := BuildSourcePreview(SourcePreviewInput{
			Config: model.Config{
				License: "MIT",
				Holder:  "Acme, Inc.",
				Style:   model.StyleReuse,
			},
			ResolvedYear: "2026",
		})
		require.NoError(t, err)
		assert.Equal(t, LanguageC, preview.Language)
		assert.Equal(t, "example.c", preview.Path)
	})

	t.Run("resolved year is required", func(t *testing.T) {
		_, err := BuildSourcePreview(SourcePreviewInput{
			Config: model.Config{
				License: "MIT",
				Holder:  "Acme, Inc.",
				Style:   model.StyleReuse,
			},
			Sample: SelectSample([]string{"main.go"}),
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "resolved year is required")
	})

	t.Run("unknown license is rejected", func(t *testing.T) {
		_, err := BuildSourcePreview(SourcePreviewInput{
			Config: model.Config{
				License: "Nope",
				Holder:  "Acme, Inc.",
				Style:   model.StyleReuse,
			},
			Sample:       SelectSample([]string{"main.go"}),
			ResolvedYear: "2026",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), `unknown license "Nope"`)
	})

	t.Run("sample path must have a file type", func(t *testing.T) {
		_, err := BuildSourcePreview(SourcePreviewInput{
			Config: model.Config{
				License: "MIT",
				Holder:  "Acme, Inc.",
				Style:   model.StyleReuse,
			},
			Sample: Sample{
				Language: LanguageFamily("Unknown"),
				Path:     "example.unknown",
				Source:   "content\n",
			},
			ResolvedYear: "2026",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no file type")
	})

	t.Run("uncommentable override rejects preview rendering", func(t *testing.T) {
		_, err := BuildSourcePreview(SourcePreviewInput{
			Config: model.Config{
				License: "MIT",
				Holder:  "Acme, Inc.",
				Style:   model.StyleReuse,
				FileTypeOverrides: map[string]model.FileType{
					".c": {
						Name: "custom-c",
						Skip: true,
					},
				},
			},
			Sample:       SelectSample([]string{"main.c"}),
			ResolvedYear: "2026",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "uncommentable")
	})
}

func TestBuildYAMLPreviewDelegatesToConfigRenderFile(t *testing.T) {
	cfg := model.Config{
		License:           "MIT",
		Holder:            "Acme, Inc.",
		Year:              model.YearSpec{Kind: model.YearExplicit, Start: 2026},
		Style:             model.StyleReuse,
		ManageLicenseFile: true,
		Excludes:          []string{"vendor/**"},
	}

	got, err := BuildYAMLPreview(cfg)
	require.NoError(t, err)
	want, err := config.RenderFile(cfg)
	require.NoError(t, err)

	assert.Equal(t, string(want), string(got))
}

func indexOf(t *testing.T, s, substr string) int {
	t.Helper()
	idx := -1
	for i := range s {
		if len(s[i:]) >= len(substr) && s[i:i+len(substr)] == substr {
			idx = i
			break
		}
	}
	require.NotEqualf(t, -1, idx, "%q not found in %q", substr, s)
	return idx
}
