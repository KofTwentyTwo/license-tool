package report

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/KofTwentyTwo/license-tool/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// optionsFixture builds a realistic report (counts populated by Build) with headered,
// headerless, and skipped source files plus resolved/unresolved dependencies.
func optionsFixture() model.Report {
	files := []model.FileResult{
		headered("src/a.go", "Go", "MIT"),
		{Path: "src/b.go", FileType: "Go"},                                        // headerless
		{Path: "img/logo.png", FileType: "", Skipped: true, SkipReason: "binary"}, // skipped
	}
	deps := []model.DependencyLicense{
		{Ecosystem: "npm", Name: "left-pad", Version: "1.0.0", SPDXID: "MIT", Resolution: model.ResolutionResolved},
		{Ecosystem: "maven", Name: "foo", Resolution: model.ResolutionUnresolved, Reason: "no metadata"},
	}
	return Build("/root", model.Config{License: "MIT", Holder: "Acme", Style: model.StyleReuse}, files, deps, nil)
}

func renderToString(t *testing.T, r model.Report, format Format, opts RenderOptions) string {
	t.Helper()
	var b bytes.Buffer
	require.NoError(t, RenderWithOptions(&b, r, format, opts))
	return b.String()
}

func TestGroupDimensionString(t *testing.T) {
	assert.Equal(t, "none", GroupNone.String())
	assert.Equal(t, "license", GroupLicense.String())
	assert.Equal(t, "category", GroupCategory.String())
	assert.Equal(t, "type", GroupType.String())
	assert.Equal(t, "directory", GroupDirectory.String())
}

func TestTitleCase(t *testing.T) {
	assert.Equal(t, "", titleCase(""))
	assert.Equal(t, "License", titleCase("license"))
}

func TestRenderTextSummaryOmitsLists(t *testing.T) {
	out := renderToString(t, optionsFixture(), FormatText, RenderOptions{Summary: true})
	assert.Contains(t, out, "by SPDX id:") // rollups stay
	assert.Contains(t, out, "findings:")   // findings stays
	// The flat list header sits at the line start; the findings line is indented.
	assert.NotContains(t, out, "\nsource files:")
	assert.NotContains(t, out, "left-pad") // dependency list omitted
}

func TestRenderTextGroupBy(t *testing.T) {
	out := renderToString(t, optionsFixture(), FormatText, RenderOptions{GroupBy: GroupLicense})
	assert.Contains(t, out, "source files by license:")
	assert.Contains(t, out, "MIT (1)")
	assert.Contains(t, out, "    src/a.go") // nested file line
	assert.Contains(t, out, "(none) (1)")   // headerless bucket
	assert.Contains(t, out, "(skipped: 1)") // skipped note
	assert.Contains(t, out, "left-pad")     // deps list still shown (not summary)
}

func TestRenderTextGroupBySummaryCountsOnly(t *testing.T) {
	out := renderToString(t, optionsFixture(), FormatText, RenderOptions{Summary: true, GroupBy: GroupDirectory})
	assert.Contains(t, out, "source files by directory:")
	assert.Contains(t, out, "src (2)")         // src/a.go + src/b.go
	assert.NotContains(t, out, "    src/a.go") // no file paths under summary
	assert.NotContains(t, out, "left-pad")     // deps omitted
}

func TestRenderTextGroupByEmpty(t *testing.T) {
	// A report with only skipped files yields no groups but still a skipped note.
	r := model.Report{Files: []model.FileResult{{Path: "a.bin", Skipped: true, SkipReason: "binary"}}}
	out := renderToString(t, r, FormatText, RenderOptions{GroupBy: GroupType})
	assert.Contains(t, out, "(no managed source files)")
	assert.Contains(t, out, "(skipped: 1)")
}

func TestRenderMarkdownGroupBy(t *testing.T) {
	out := renderToString(t, optionsFixture(), FormatMarkdown, RenderOptions{GroupBy: GroupLicense})
	assert.Contains(t, out, "## Source files by license")
	assert.Contains(t, out, "### `MIT` (1)")
	assert.Contains(t, out, "| `src/a.go` |")
	assert.Contains(t, out, "_Skipped (uneditable): 1_")
}

func TestRenderMarkdownSummaryGroupBy(t *testing.T) {
	out := renderToString(t, optionsFixture(), FormatMarkdown, RenderOptions{Summary: true, GroupBy: GroupLicense})
	assert.Contains(t, out, "| License | Files |")
	assert.Contains(t, out, "| `MIT` | 1 |")
	assert.NotContains(t, out, "| `src/a.go` |")
}

func TestRenderMarkdownSummaryOmitsTables(t *testing.T) {
	out := renderToString(t, optionsFixture(), FormatMarkdown, RenderOptions{Summary: true})
	assert.Contains(t, out, "## By SPDX id")
	assert.NotContains(t, out, "## Source files")
	assert.NotContains(t, out, "## Dependencies")
}

func TestRenderJSONGroupBy(t *testing.T) {
	out := renderToString(t, optionsFixture(), FormatJSON, RenderOptions{GroupBy: GroupLicense})
	var raw map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &raw))
	groups, ok := raw["groups"].([]any)
	require.True(t, ok, "groups array present")
	require.NotEmpty(t, groups)
	first := groups[0].(map[string]any)
	assert.Contains(t, first, "key")
	assert.Contains(t, first, "count")
	assert.Contains(t, first, "files") // detail present in non-summary group-by
	// Full report still carries the top-level files/dependencies.
	assert.Contains(t, raw, "files")
	assert.Contains(t, raw, "dependencies")
}

func TestRenderJSONSummaryTrimsDetail(t *testing.T) {
	out := renderToString(t, optionsFixture(), FormatJSON, RenderOptions{Summary: true})
	var raw map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &raw))
	assert.Equal(t, "license-tool/report/v1", raw["schema"])
	assert.Contains(t, raw, "licenseCounts")
	assert.NotContains(t, raw, "files")
	assert.NotContains(t, raw, "dependencies")
	assert.NotContains(t, raw, "groups") // none requested
}

func TestRenderJSONSummaryGroupByCountsOnly(t *testing.T) {
	out := renderToString(t, optionsFixture(), FormatJSON, RenderOptions{Summary: true, GroupBy: GroupCategory})
	var raw map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &raw))
	groups := raw["groups"].([]any)
	require.NotEmpty(t, groups)
	first := groups[0].(map[string]any)
	assert.Contains(t, first, "count")
	assert.NotContains(t, first, "files") // counts only under summary
	assert.NotContains(t, raw, "files")
}
