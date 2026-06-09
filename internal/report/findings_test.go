package report

import (
	"bytes"
	"testing"

	"github.com/KofTwentyTwo/license-tool/internal/model"
	"github.com/stretchr/testify/assert"
)

// fileWith builds a non-skipped FileResult carrying the given detected id (empty id
// means "no managed header").
func fileWith(path, id string) model.FileResult {
	fr := model.FileResult{Path: path}
	if id != "" {
		fr.Detected = model.DetectedHeader{Present: true, SPDXID: id}
	}
	return fr
}

func TestBuildFindingsSourceCoverageAndLicenseMix(t *testing.T) {
	r := model.Report{
		Files: []model.FileResult{
			fileWith("a.go", "MIT"),
			fileWith("b.go", "MIT"),
			fileWith("c.go", "GPL-3.0-or-later"),
			fileWith("d.go", ""), // missing header
			{Path: "skip.json", Skipped: true, SkipReason: "uncommentable"},
		},
	}
	f := buildFindings(r)

	assert.Equal(t, 4, f.SourceTotal) // skipped file excluded
	assert.Equal(t, 3, f.SourceHeadered)
	assert.Equal(t, 1, f.SourceMissing)
	assert.Equal(t, 2, f.LicenseCounts["MIT"])
	assert.Equal(t, 1, f.LicenseCounts["GPL-3.0-or-later"])

	// License mix is sorted by id, with the missing bucket last.
	assert.Equal(t, "GPL-3.0-or-later 1, MIT 2, (no-header) 1", f.licenseSummary())
}

func TestBuildFindingsUnknownAndCopyleft(t *testing.T) {
	r := model.Report{
		Files: []model.FileResult{
			fileWith("a.go", "MIT"),                // permissive, known
			fileWith("b.go", "GPL-3.0-or-later"),   // strong copyleft
			fileWith("c.go", "Not-A-Real-License"), // unrecognized
		},
	}
	f := buildFindings(r)

	assert.Equal(t, 1, f.UnknownCount)                        // only the bogus id
	assert.Equal(t, []string{"GPL-3.0-or-later"}, f.Copyleft) // strong copyleft present
	assert.Equal(t, "GPL-3.0-or-later", f.copyleftSummary())
}

func TestBuildFindingsNoSourceFiles(t *testing.T) {
	f := buildFindings(model.Report{})
	assert.Equal(t, 0, f.SourceTotal)
	assert.Equal(t, "(none)", f.licenseSummary())
	assert.Equal(t, "none", f.copyleftSummary())
}

func TestBuildFindingsDependencies(t *testing.T) {
	// No deps scanned -> the dependencies line is suppressed.
	none := buildFindings(model.Report{})
	assert.False(t, none.DepsScanned)

	r := model.Report{
		Dependencies: []model.DependencyLicense{
			{Ecosystem: "maven", Name: "a", SPDXID: "MIT", Resolution: model.ResolutionResolved},
			{Ecosystem: "npm", Name: "b", Resolution: model.ResolutionUnresolved},
		},
	}
	f := buildFindings(r)
	assert.True(t, f.DepsScanned)
	assert.Equal(t, 2, f.DepsTotal)
	assert.Equal(t, 1, f.DepsResolved)
	assert.Equal(t, 1, f.DepsUnresolved)
}

// licenseSummary has a defensive "no parts" guard: when there are coverable source
// files (SourceTotal > 0) but neither any headered ids nor any missing files, it
// returns "(none)". buildFindings cannot produce that shape (every coverable file is
// either headered or counted as missing, so SourceTotal > 0 implies at least one
// part), so we exercise the guard directly on a hand-built Findings to keep it from
// being dead-but-untested code.
func TestFindingsLicenseSummaryNoPartsGuard(t *testing.T) {
	f := Findings{SourceTotal: 1, LicenseCounts: map[string]int{}, SourceMissing: 0}
	assert.Equal(t, "(none)", f.licenseSummary())
}

func TestFindingsPolicySummary(t *testing.T) {
	assert.Equal(t, "PASS", Findings{Passed: true}.policySummary())
	assert.Equal(t, "FAIL", Findings{Passed: false}.policySummary())
	assert.Equal(t,
		"FAIL (2: deny:GPL-3.0-or-later, missing-header)",
		Findings{Passed: false, Violations: []string{"deny:GPL-3.0-or-later", "missing-header"}}.policySummary(),
	)
}

func TestRenderFindingsBlock(t *testing.T) {
	r := model.Report{
		Files: []model.FileResult{
			fileWith("a.go", "MIT"),
			fileWith("d.go", ""),
		},
		Dependencies: []model.DependencyLicense{
			{Ecosystem: "maven", Name: "a", SPDXID: "MIT", Resolution: model.ResolutionResolved},
		},
		Passed: true,
	}
	var buf bytes.Buffer
	bw := &errWriter{w: &buf}
	renderFindings(bw, buildFindings(r))
	out := buf.String()

	assert.Contains(t, out, "findings:")
	assert.Contains(t, out, "source files: 2 (headered 1, missing 1)")
	assert.Contains(t, out, "license types: MIT 1, (no-header) 1")
	assert.Contains(t, out, "unknown/unrecognized: 0")
	assert.Contains(t, out, "copyleft: none")
	assert.Contains(t, out, "dependencies: 1 (resolved 1, unresolved 0)")
	assert.Contains(t, out, "policy: PASS")
}

func TestRenderFindingsOmitsDepsWhenNotScanned(t *testing.T) {
	var buf bytes.Buffer
	bw := &errWriter{w: &buf}
	renderFindings(bw, buildFindings(model.Report{Passed: true}))
	assert.NotContains(t, buf.String(), "dependencies:")
}
