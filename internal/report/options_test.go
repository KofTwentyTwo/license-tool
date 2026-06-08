package report

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/KofTwentyTwo/license-tool/internal/model"
	"github.com/KofTwentyTwo/license-tool/internal/policy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func reportWithViolations() model.Report {
	r := optionsFixture()
	r.ViolationDetails = []model.ViolationDetail{
		{Condition: model.FailOnMissingHeader, Path: "src/legacy.go", Message: "src/legacy.go: no managed license header"},
		{Condition: model.FailOnPolicyViolation, SPDXID: "AGPL-3.0-or-later", Path: "web/c.ts", Message: `web/c.ts: license "AGPL-3.0-or-later" is not on the allow list`},
	}
	return r
}

func TestPercent(t *testing.T) {
	assert.Equal(t, "0.0%", percent(5, 0))
	assert.Equal(t, "50.0%", percent(1, 2))
	assert.Equal(t, "100.0%", percent(4, 4))
}

func TestSortByCountHelpers(t *testing.T) {
	counts := map[string]int{"a": 1, "b": 3, "c": 2}

	byKey := sortedCountsBy(counts, false)
	assert.Equal(t, []string{"a", "b", "c"}, []string{byKey[0].key, byKey[1].key, byKey[2].key})

	byCount := sortedCountsBy(counts, true)
	assert.Equal(t, []string{"b", "c", "a"}, []string{byCount[0].key, byCount[1].key, byCount[2].key})

	groups := []Group{{Key: "a", Count: 1}, {Key: "b", Count: 3}, {Key: "c", Count: 2}}
	sortGroups(groups, false) // no-op leaves key order
	assert.Equal(t, "a", groups[0].Key)
	sortGroups(groups, true)
	assert.Equal(t, []string{"b", "c", "a"}, []string{groups[0].Key, groups[1].Key, groups[2].Key})

	tied := []Group{{Key: "z", Count: 2}, {Key: "a", Count: 2}}
	sortGroups(tied, true) // equal counts -> alphabetical key
	assert.Equal(t, "a", tied[0].Key)
}

func TestWorstRiskAndSummary(t *testing.T) {
	var w worstRisk
	lvl, cat := w.result()
	assert.Equal(t, "none", lvl)
	assert.Empty(t, cat)

	w.observe(model.CategoryPermissive)      // low
	w.observe(model.CategoryStrongCopyleft)  // high beats low
	w.observe(model.CategoryWeakCopyleft)    // medium does not beat high
	w.observe(model.CategoryNetworkCopyleft) // tie on high, higher enum wins
	w.observe(model.CategoryUnknown)         // unknown (rank 0) loses
	lvl, cat = w.result()
	assert.Equal(t, "high", lvl)
	assert.Equal(t, "network-copyleft", cat)

	assert.Equal(t, "none", Findings{}.riskSummary())
	assert.Equal(t, "high (network-copyleft)", Findings{RiskLevel: "high", WorstCategory: "network-copyleft"}.riskSummary())
}

func TestToViolationDetailsSortsDedupesAndHandlesEmpty(t *testing.T) {
	assert.Empty(t, toViolationDetails(nil))

	got := toViolationDetails([]policy.Violation{
		{Condition: model.FailOnPolicyViolation, SPDXID: "MIT", Path: "b", Message: "m"},
		{Condition: model.FailOnMissingHeader, Path: "a", Message: "m2"},
		{Condition: model.FailOnMissingHeader, Path: "a", Message: "m1"}, // same cond+path, diff msg
		{Condition: model.FailOnMissingHeader, Path: "a", Message: "m1"}, // exact duplicate
	})
	require.Len(t, got, 3) // duplicate collapsed
	// missing-header (0) sorts before policy-violation (2); within, by path then message.
	assert.Equal(t, model.FailOnMissingHeader, got[0].Condition)
	assert.Equal(t, "m1", got[0].Message)
	assert.Equal(t, "m2", got[1].Message)
	assert.Equal(t, model.FailOnPolicyViolation, got[2].Condition)
}

func TestRenderTextViolationDetails(t *testing.T) {
	out := renderToString(t, reportWithViolations(), FormatText, RenderOptions{})
	assert.Contains(t, out, "policy violations:")
	assert.Contains(t, out, "[missing-header] src/legacy.go: no managed license header")
	assert.Contains(t, out, `[policy-violation] web/c.ts: license "AGPL-3.0-or-later" is not on the allow list`)
}

func TestRenderMarkdownViolationDetails(t *testing.T) {
	out := renderToString(t, reportWithViolations(), FormatMarkdown, RenderOptions{})
	assert.Contains(t, out, "## Policy violations")
	assert.Contains(t, out, "| Condition | License | Location | Detail |")
	assert.Contains(t, out, "| policy-violation | `AGPL-3.0-or-later` | web/c.ts |")
}

func TestRenderJSONViolationDetails(t *testing.T) {
	out := renderToString(t, reportWithViolations(), FormatJSON, RenderOptions{})
	var raw map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &raw))
	vd, ok := raw["violationDetails"].([]any)
	require.True(t, ok)
	require.Len(t, vd, 2)
	first := vd[0].(map[string]any)
	assert.Equal(t, "missing-header", first["condition"])
	assert.Contains(t, first, "message")
}

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
	assert.Contains(t, out, "    src/a.go")    // nested file line
	assert.Contains(t, out, "(no-header) (1)") // headerless bucket
	assert.Contains(t, out, "(skipped: 1)")    // skipped note
	assert.Contains(t, out, "left-pad")        // deps list still shown (not summary)
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

func TestRenderJSONIgnoresSummaryTrim(t *testing.T) {
	// JSON is always the complete report: --summary only trims human formats, so a
	// machine consumer still gets files, dependencies, and findings in one call.
	out := renderToString(t, optionsFixture(), FormatJSON, RenderOptions{Summary: true})
	var raw map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &raw))
	assert.Equal(t, "license-tool/report/v1", raw["schema"])
	assert.Contains(t, raw, "files")
	assert.Contains(t, raw, "dependencies")
	assert.Contains(t, raw, "findings")
}

func TestRenderJSONSummaryGroupKeepsFiles(t *testing.T) {
	out := renderToString(t, optionsFixture(), FormatJSON, RenderOptions{Summary: true, GroupBy: GroupCategory})
	var raw map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &raw))
	first := raw["groups"].([]any)[0].(map[string]any)
	assert.Contains(t, first, "count")
	assert.Contains(t, first, "files") // JSON groups always carry detail
}

func TestRenderDirectoryBreakdown(t *testing.T) {
	r := optionsFixture() // src/a.go (MIT), src/b.go (no header)
	textOut := renderToString(t, r, FormatText, RenderOptions{GroupBy: GroupDirectory})
	assert.Contains(t, textOut, "source files by directory:")
	assert.Contains(t, textOut, "licenses: ") // per-group breakdown line (non-license dim)

	mdOut := renderToString(t, r, FormatMarkdown, RenderOptions{GroupBy: GroupDirectory})
	assert.Contains(t, mdOut, "Licenses: ")

	jsonOut := renderToString(t, r, FormatJSON, RenderOptions{GroupBy: GroupDirectory})
	var raw map[string]any
	require.NoError(t, json.Unmarshal([]byte(jsonOut), &raw))
	g := raw["groups"].([]any)[0].(map[string]any)
	assert.Contains(t, g, "licenses")
	assert.Contains(t, g, "risk")
}

func TestParseOnly(t *testing.T) {
	got, err := ParseOnly("missing, copyleft,violations,unknown")
	require.NoError(t, err)
	assert.Equal(t, []OnlyFilter{OnlyMissing, OnlyCopyleft, OnlyViolations, OnlyUnknown}, got)

	empty, err := ParseOnly("")
	require.NoError(t, err)
	assert.Empty(t, empty)

	_, err = ParseOnly("bogus")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown --only filter")
}

func TestKeepFile(t *testing.T) {
	missing := model.FileResult{Path: "a.go"}
	mit := headered("b.go", "Go", "MIT")
	agpl := headered("c.go", "Go", "AGPL-3.0-or-later")
	unknownLic := headered("d.go", "Go", "Frobnicate-9000")
	violating := model.FileResult{Path: "e.go", Detected: model.DetectedHeader{Present: true, SPDXID: "MIT"}, Violations: []string{"policy-violation"}}
	skipped := model.FileResult{Path: "x.bin", Skipped: true}

	assert.True(t, keepFile(missing, nil))                        // empty -> keep all
	assert.False(t, keepFile(skipped, []OnlyFilter{OnlyMissing})) // skipped never matches
	assert.True(t, keepFile(missing, []OnlyFilter{OnlyMissing}))
	assert.False(t, keepFile(mit, []OnlyFilter{OnlyMissing}))
	assert.True(t, keepFile(unknownLic, []OnlyFilter{OnlyUnknown}))
	assert.True(t, keepFile(agpl, []OnlyFilter{OnlyCopyleft}))
	assert.False(t, keepFile(mit, []OnlyFilter{OnlyCopyleft}))
	assert.True(t, keepFile(violating, []OnlyFilter{OnlyViolations}))
}

func TestRenderTextOnlyFilter(t *testing.T) {
	out := renderToString(t, optionsFixture(), FormatText, RenderOptions{Only: []OnlyFilter{OnlyMissing}})
	assert.Contains(t, out, "src/b.go")    // headerless kept
	assert.NotContains(t, out, "src/a.go") // MIT-headered filtered out
}
