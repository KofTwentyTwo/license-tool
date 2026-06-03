package report

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/KofTwentyTwo/license-tool/internal/model"
	"github.com/KofTwentyTwo/license-tool/internal/policy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// header builds a present DetectedHeader carrying the given SPDX id, the common
// shape every "managed file with a license" case needs.
func header(id string) model.DetectedHeader {
	return model.DetectedHeader{Present: true, SPDXID: id}
}

// errBoom is a sentinel error fakes return to exercise the error branches.
var errBoom = errors.New("boom")

// ----- ParseFormat / Format.String -----

func TestParseFormat(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		want    Format
		wantErr bool
	}{
		{name: "text", raw: "text", want: FormatText},
		{name: "empty defaults to text", raw: "", want: FormatText},
		{name: "json", raw: "json", want: FormatJSON},
		{name: "markdown", raw: "markdown", want: FormatMarkdown},
		{name: "md alias", raw: "md", want: FormatMarkdown},
		{name: "unknown errors and falls back to text", raw: "xml", want: FormatText, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseFormat(tc.raw)
			assert.Equal(t, tc.want, got)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "unknown format")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestFormatString(t *testing.T) {
	cases := []struct {
		name string
		f    Format
		want string
	}{
		{name: "text", f: FormatText, want: "text"},
		{name: "json", f: FormatJSON, want: "json"},
		{name: "markdown", f: FormatMarkdown, want: "markdown"},
		{name: "unknown falls back to text", f: Format(99), want: "text"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.f.String())
		})
	}
}

// ----- Build: aggregation, ordering, dedupe, Passed -----

func TestBuildAggregationAndOrdering(t *testing.T) {
	cfg := model.Config{
		License: "MIT",
		Holder:  "Kingsrook, LLC",
		Style:   model.StyleReusePlusNotice,
		Policy:  model.Policy{FailOn: []model.FailCondition{model.FailOnMissingHeader}},
	}

	files := []model.FileResult{
		// Curated permissive license -> counts toward MIT + permissive.
		{Path: "b.go", FileType: "Go", Detected: header("MIT")},
		// Same id again -> MIT tally becomes 2.
		{Path: "a.go", FileType: "Go", Detected: header("MIT")},
		// Uncurated id (valid-looking but not in curated set) -> unknown category.
		{Path: "c.txt", FileType: "Text", Detected: header("Frobnicate-1.0")},
		// Managed-but-headerless -> (none) bucket + unknown category, and a
		// missing-header violation token on the file.
		{Path: "d.go", FileType: "Go", Detected: model.DetectedHeader{Present: false},
			Violations: []string{model.FailOnMissingHeader.String()}},
		// Skipped file: contributes to file-type breakdown only, never license/category.
		{Path: "e.json", FileType: "JSON", Skipped: true, SkipReason: "uncommentable"},
		// Empty file type: contributes to nothing in the file-type breakdown.
		{Path: "f", FileType: "", Detected: header("MIT")},
	}

	r := Build("/repo", cfg, files, nil, nil)

	assert.Equal(t, "/repo", r.Root)
	assert.Equal(t, cfg, r.Config)

	// Files slice is a copy (defensive); mutating the input must not change the report.
	require.Len(t, r.Files, len(files))
	files[0].Path = "MUTATED"
	assert.Equal(t, "b.go", r.Files[0].Path)

	// License counts: MIT appears 3x (b, a, f), Frobnicate 1x, (none) 1x.
	assert.Equal(t, map[string]int{"MIT": 3, "Frobnicate-1.0": 1, noLicenseKey: 1}, r.LicenseCounts)

	// Category counts: permissive 3 (MIT x3), unknown 2 (Frobnicate + the (none) file).
	assert.Equal(t, map[string]int{"permissive": 3, "unknown": 2}, r.CategoryCounts)

	// File-type counts: Go x3, Text x1, JSON x1 (skipped still counts); the empty
	// file type does not appear.
	assert.Equal(t, map[string]int{"Go": 3, "Text": 1, "JSON": 1}, r.FileTypeCounts)

	// FailOnMissingHeader is configured and a file carries that token -> not passed.
	assert.False(t, r.Passed)
}

func TestBuildPassedWhenNoFailConditionTrips(t *testing.T) {
	// A missing header exists, but fail_on does not include missing-header, so the
	// run passes.
	cfg := model.Config{
		Policy: model.Policy{FailOn: []model.FailCondition{model.FailOnPolicyViolation}},
	}
	files := []model.FileResult{
		{Path: "d.go", FileType: "Go", Detected: model.DetectedHeader{Present: false},
			Violations: []string{model.FailOnMissingHeader.String()}},
	}
	r := Build("/repo", cfg, files, nil, nil)
	assert.True(t, r.Passed)
}

func TestBuildUnresolvedDependencyFeedsFailOn(t *testing.T) {
	cfg := model.Config{
		Policy: model.Policy{FailOn: []model.FailCondition{model.FailOnUnresolvedDependency}},
	}
	deps := []model.DependencyLicense{
		{Ecosystem: "maven", Name: "org.example:lib", Version: "1.0", SPDXID: "Apache-2.0", Resolution: model.ResolutionResolved},
		{Ecosystem: "npm", Name: "left-pad", Resolution: model.ResolutionUnresolved, Reason: "no metadata"},
	}
	r := Build("/repo", cfg, nil, deps, nil)

	// Dependencies copied defensively.
	require.Len(t, r.Dependencies, 2)
	deps[0].Name = "MUTATED"
	assert.Equal(t, "org.example:lib", r.Dependencies[0].Name)

	// One dependency is unresolved and fail_on gates on it -> not passed.
	assert.False(t, r.Passed)
}

func TestBuildRepoViolationsSortedAndDeduped(t *testing.T) {
	cfg := model.Config{
		Policy: model.Policy{FailOn: []model.FailCondition{model.FailOnPolicyViolation}},
	}
	// Two violations carry the same token; Build sorts then dedupes the token set.
	repoViolations := []policy.Violation{
		{Condition: model.FailOnPolicyViolation, Message: "first"},
		{Condition: model.FailOnPolicyViolation, Message: "second"},
	}
	r := Build("/repo", cfg, nil, nil, repoViolations)

	assert.Equal(t, []string{model.FailOnPolicyViolation.String()}, r.Violations)
	assert.False(t, r.Passed)
}

func TestBuildEmptyInputsProduceEmptyButNonNilAggregates(t *testing.T) {
	r := Build("/repo", model.Config{}, nil, nil, nil)
	assert.NotNil(t, r.LicenseCounts)
	assert.NotNil(t, r.CategoryCounts)
	assert.NotNil(t, r.FileTypeCounts)
	assert.Empty(t, r.LicenseCounts)
	assert.Empty(t, r.Violations)
	// No fail conditions configured and no violations -> passes.
	assert.True(t, r.Passed)
}

// ----- categoryToken / conditionFromToken / dedupeStrings (via Build + direct) -----

func TestCategoryToken(t *testing.T) {
	cases := []struct {
		name string
		id   string
		want string
	}{
		{name: "curated permissive", id: "MIT", want: "permissive"},
		{name: "curated network copyleft", id: "AGPL-3.0-or-later", want: "network-copyleft"},
		{name: "uncurated id is unknown", id: "Frobnicate-1.0", want: "unknown"},
		{name: "empty id is unknown", id: "", want: "unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, categoryToken(tc.id))
		})
	}
}

func TestConditionFromToken(t *testing.T) {
	cases := []struct {
		name   string
		tok    string
		want   model.FailCondition
		wantOK bool
	}{
		{name: "missing-header", tok: model.FailOnMissingHeader.String(), want: model.FailOnMissingHeader, wantOK: true},
		{name: "unknown-license", tok: model.FailOnUnknownLicense.String(), want: model.FailOnUnknownLicense, wantOK: true},
		{name: "policy-violation", tok: model.FailOnPolicyViolation.String(), want: model.FailOnPolicyViolation, wantOK: true},
		{name: "unresolved-dependency", tok: model.FailOnUnresolvedDependency.String(), want: model.FailOnUnresolvedDependency, wantOK: true},
		{name: "garbage token", tok: "not-a-real-token", want: 0, wantOK: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := conditionFromToken(tc.tok)
			assert.Equal(t, tc.wantOK, ok)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestDedupeStrings(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{name: "nil", in: nil, want: nil},
		{name: "empty", in: []string{}, want: nil},
		{name: "single", in: []string{"a"}, want: []string{"a"}},
		{name: "adjacent dupes removed", in: []string{"a", "a", "b", "b", "b", "c"}, want: []string{"a", "b", "c"}},
		{name: "no dupes", in: []string{"a", "b", "c"}, want: []string{"a", "b", "c"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, dedupeStrings(tc.in))
		})
	}
}

// Build folds a file whose violation token is NOT a known condition: the token is
// dropped from the fail-on set (conditionFromToken returns ok=false), exercising the
// false branch through Build.
func TestBuildIgnoresUnknownFileViolationToken(t *testing.T) {
	cfg := model.Config{Policy: model.Policy{FailOn: []model.FailCondition{model.FailOnMissingHeader}}}
	files := []model.FileResult{
		{Path: "a.go", FileType: "Go", Detected: header("MIT"), Violations: []string{"mystery-token"}},
	}
	r := Build("/repo", cfg, files, nil, nil)
	// The mystery token maps to no condition, so nothing trips fail_on.
	assert.True(t, r.Passed)
}

// ----- Audit (fake Pipeline) -----

func TestAuditRequiresEnumerate(t *testing.T) {
	_, err := Audit("/repo", model.Config{}, Options{}, Pipeline{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires a Pipeline.Enumerate")
}

func TestAuditEnumerateError(t *testing.T) {
	pipe := Pipeline{
		Enumerate: func(root string, excludes []string) ([]SourceFile, error) {
			return nil, errBoom
		},
	}
	_, err := Audit("/repo", model.Config{}, Options{}, pipe)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "enumerate")
	assert.ErrorIs(t, err, errBoom)
}

func TestAuditHappyPathWithDetectionAndDeps(t *testing.T) {
	cfg := model.Config{
		License:  "MIT",
		Excludes: []string{"vendor"},
		Policy: model.Policy{
			Required: "MIT",
			FailOn:   []model.FailCondition{model.FailOnMissingHeader, model.FailOnUnresolvedDependency},
		},
	}

	var sawExcludes []string
	var sawAllowTool bool
	pipe := Pipeline{
		Enumerate: func(root string, excludes []string) ([]SourceFile, error) {
			sawExcludes = excludes
			return []SourceFile{
				{Path: "a.go", FileType: model.FileType{Name: "Go"}, Content: []byte("//a")},
				// Skipped source: detection is not attempted; SkipReason is carried.
				{Path: "img.png", FileType: model.FileType{Name: "PNG"}, Skip: true, SkipReason: "binary"},
				// Headerless managed file -> missing-header violation attaches to the file.
				{Path: "b.go", FileType: model.FileType{Name: "Go"}, Content: []byte("//b")},
			}, nil
		},
		Detect: func(content []byte, ft model.FileType) (model.DetectedHeader, error) {
			if string(content) == "//a" {
				return header("MIT"), nil
			}
			return model.DetectedHeader{Present: false}, nil
		},
		ResolveDeps: func(root string, allowToolShellOut bool) ([]model.DependencyLicense, error) {
			sawAllowTool = allowToolShellOut
			return []model.DependencyLicense{
				{Ecosystem: "maven", Name: "lib", Version: "1.0", SPDXID: "Apache-2.0", Resolution: model.ResolutionResolved},
			}, nil
		},
	}

	opts := Options{IncludeDeps: true, ResolveDeps: "tool", AllowToolShellOut: true}
	r, err := Audit("/repo", cfg, opts, pipe)
	require.NoError(t, err)

	assert.Equal(t, []string{"vendor"}, sawExcludes)
	assert.True(t, sawAllowTool)

	require.Len(t, r.Files, 3)

	// Files retain enumerator order in Audit (Render sorts for output).
	assert.Equal(t, "a.go", r.Files[0].Path)
	assert.Equal(t, "MIT", r.Files[0].Detected.SPDXID)
	assert.Equal(t, "none", r.Files[0].Action)
	assert.Empty(t, r.Files[0].Violations)

	// Skipped file: reason carried, no detection, no violation.
	assert.True(t, r.Files[1].Skipped)
	assert.Equal(t, "binary", r.Files[1].SkipReason)

	// Headerless file b.go carries the missing-header token.
	assert.Equal(t, []string{model.FailOnMissingHeader.String()}, r.Files[2].Violations)

	require.Len(t, r.Dependencies, 1)
	assert.Equal(t, "Apache-2.0", r.Dependencies[0].SPDXID)

	// b.go is missing a header and fail_on gates on missing-header -> fails.
	assert.False(t, r.Passed)
}

func TestAuditDetectErrorRecordedNotFatal(t *testing.T) {
	pipe := Pipeline{
		Enumerate: func(root string, excludes []string) ([]SourceFile, error) {
			return []SourceFile{
				{Path: "bad.go", FileType: model.FileType{Name: "Go"}, Content: []byte("x")},
				{Path: "ok.go", FileType: model.FileType{Name: "Go"}, Content: []byte("y")},
			}, nil
		},
		Detect: func(content []byte, ft model.FileType) (model.DetectedHeader, error) {
			if string(content) == "x" {
				return model.DetectedHeader{}, errBoom
			}
			return header("MIT"), nil
		},
	}
	r, err := Audit("/repo", model.Config{}, Options{}, pipe)
	require.NoError(t, err)
	require.Len(t, r.Files, 2)

	// The detect error is recorded on the file and the run continued.
	assert.Equal(t, errBoom.Error(), r.Files[0].Err)
	assert.Empty(t, r.Files[0].Violations) // errored files get no policy verdict

	// The second file detected normally.
	assert.Equal(t, "MIT", r.Files[1].Detected.SPDXID)
}

func TestAuditNilDetectLeavesHeadersUndetected(t *testing.T) {
	// Detect is nil: the stage is absent, so files carry no detected header but still
	// flow through policy (which faults them as missing-header).
	pipe := Pipeline{
		Enumerate: func(root string, excludes []string) ([]SourceFile, error) {
			return []SourceFile{{Path: "a.go", FileType: model.FileType{Name: "Go"}}}, nil
		},
	}
	r, err := Audit("/repo", model.Config{}, Options{}, pipe)
	require.NoError(t, err)
	require.Len(t, r.Files, 1)
	assert.False(t, r.Files[0].Detected.Present)
	assert.Equal(t, []string{model.FailOnMissingHeader.String()}, r.Files[0].Violations)
}

func TestAuditResolveDepsError(t *testing.T) {
	pipe := Pipeline{
		Enumerate: func(root string, excludes []string) ([]SourceFile, error) {
			return nil, nil
		},
		ResolveDeps: func(root string, allowToolShellOut bool) ([]model.DependencyLicense, error) {
			return nil, errBoom
		},
	}
	opts := Options{IncludeDeps: true, ResolveDeps: "ondisk"}
	_, err := Audit("/repo", model.Config{}, opts, pipe)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolve dependencies")
	assert.ErrorIs(t, err, errBoom)
}

func TestAuditDepsDisabledPaths(t *testing.T) {
	resolveCalled := false
	base := Pipeline{
		Enumerate: func(root string, excludes []string) ([]SourceFile, error) { return nil, nil },
		ResolveDeps: func(root string, allowToolShellOut bool) ([]model.DependencyLicense, error) {
			resolveCalled = true
			return nil, nil
		},
	}
	cases := []struct {
		name string
		opts Options
	}{
		{name: "IncludeDeps false", opts: Options{IncludeDeps: false, ResolveDeps: "tool"}},
		{name: "ResolveDeps off", opts: Options{IncludeDeps: true, ResolveDeps: "off"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resolveCalled = false
			r, err := Audit("/repo", model.Config{}, tc.opts, base)
			require.NoError(t, err)
			assert.False(t, resolveCalled, "ResolveDeps must not be invoked")
			assert.Empty(t, r.Dependencies)
		})
	}
}

func TestAuditNilResolveDepsField(t *testing.T) {
	// IncludeDeps on and tier != off, but the ResolveDeps field itself is nil: the
	// stage is absent and yields no dependencies (no panic).
	pipe := Pipeline{
		Enumerate: func(root string, excludes []string) ([]SourceFile, error) { return nil, nil },
	}
	opts := Options{IncludeDeps: true, ResolveDeps: "ondisk"}
	r, err := Audit("/repo", model.Config{}, opts, pipe)
	require.NoError(t, err)
	assert.Empty(t, r.Dependencies)
}

// Audit feeds distinct detected source ids into repo-level policy; a required
// license absent from the detected set surfaces as a repo policy violation.
func TestAuditRepoLevelRequiredViolation(t *testing.T) {
	cfg := model.Config{
		Policy: model.Policy{
			Required: "Apache-2.0",
			FailOn:   []model.FailCondition{model.FailOnPolicyViolation},
		},
	}
	pipe := Pipeline{
		Enumerate: func(root string, excludes []string) ([]SourceFile, error) {
			return []SourceFile{{Path: "a.go", FileType: model.FileType{Name: "Go"}, Content: []byte("//a")}}, nil
		},
		Detect: func(content []byte, ft model.FileType) (model.DetectedHeader, error) {
			return header("MIT"), nil
		},
	}
	r, err := Audit("/repo", cfg, Options{}, pipe)
	require.NoError(t, err)
	// Repo-level required-license violation present and gated on -> fails.
	assert.False(t, r.Passed)
	assert.NotEmpty(t, r.Violations)
}

// ----- distinctSourceIDs (via Audit and directly) -----

func TestDistinctSourceIDs(t *testing.T) {
	files := []model.FileResult{
		{Path: "a", Detected: header("MIT")},
		{Path: "b", Detected: header("Apache-2.0")},
		{Path: "c", Detected: header("MIT")},                                   // dupe
		{Path: "d", Skipped: true, Detected: header("GPL-3.0-or-later")},       // skipped excluded
		{Path: "e", Detected: model.DetectedHeader{Present: true, SPDXID: ""}}, // empty id excluded
		{Path: "f", Detected: model.DetectedHeader{Present: false}},            // not present excluded
	}
	assert.Equal(t, []string{"Apache-2.0", "MIT"}, distinctSourceIDs(files))
}

// ----- Check exit codes -----

func TestCheckExitCodes(t *testing.T) {
	okPipe := Pipeline{
		Enumerate: func(root string, excludes []string) ([]SourceFile, error) {
			return []SourceFile{{Path: "a.go", FileType: model.FileType{Name: "Go"}, Content: []byte("//a")}}, nil
		},
		Detect: func(content []byte, ft model.FileType) (model.DetectedHeader, error) {
			return header("MIT"), nil
		},
	}
	failPipe := Pipeline{
		Enumerate: func(root string, excludes []string) ([]SourceFile, error) {
			// Headerless managed file trips missing-header.
			return []SourceFile{{Path: "a.go", FileType: model.FileType{Name: "Go"}, Content: []byte("//a")}}, nil
		},
		Detect: func(content []byte, ft model.FileType) (model.DetectedHeader, error) {
			return model.DetectedHeader{Present: false}, nil
		},
	}
	errPipe := Pipeline{
		Enumerate: func(root string, excludes []string) ([]SourceFile, error) {
			return nil, errBoom
		},
	}

	cases := []struct {
		name     string
		cfg      model.Config
		pipe     Pipeline
		wantCode int
		wantErr  bool
	}{
		{
			name:     "passing audit exits 0",
			cfg:      model.Config{Policy: model.Policy{FailOn: []model.FailCondition{model.FailOnMissingHeader}}},
			pipe:     okPipe,
			wantCode: 0,
		},
		{
			name:     "failing audit exits 1",
			cfg:      model.Config{Policy: model.Policy{FailOn: []model.FailCondition{model.FailOnMissingHeader}}},
			pipe:     failPipe,
			wantCode: 1,
		},
		{
			name:     "internal audit error exits 4",
			cfg:      model.Config{},
			pipe:     errPipe,
			wantCode: 4,
			wantErr:  true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, err := Check("/repo", tc.cfg, Options{}, tc.pipe)
			assert.Equal(t, tc.wantCode, code)
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// ----- Render dispatch + unknown format -----

func TestRenderUnknownFormat(t *testing.T) {
	var buf bytes.Buffer
	err := Render(&buf, model.Report{}, Format(99))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot render unknown format")
}

// representativeReport returns a Report touching every renderer branch: counts in
// all three aggregates, a detected file, a skipped file, an errored file, a
// headerless file, an empty-file-type file, a resolved dep, an unresolved dep, and
// repo-level violations.
func representativeReport() model.Report {
	return model.Report{
		Root: "/repo",
		Config: model.Config{
			License: "MIT",
			Holder:  "Kingsrook, LLC",
			Style:   model.StyleReusePlusNotice,
		},
		Files: []model.FileResult{
			{Path: "z.go", FileType: "Go", Detected: header("MIT"), Action: "none"},
			{Path: "a.go", FileType: "Go", Detected: header("MIT"), Action: "none"},
			{Path: "skip.json", FileType: "JSON", Skipped: true, SkipReason: "uncommentable"},
			{Path: "err.go", FileType: "Go", Err: "read failed"},
			{Path: "bare.go", FileType: "Go", Detected: model.DetectedHeader{Present: false}},
			{Path: "noft", FileType: "", Detected: header("MIT")},
		},
		Dependencies: []model.DependencyLicense{
			{Ecosystem: "maven", Name: "lib", Version: "1.0", SPDXID: "Apache-2.0", Resolution: model.ResolutionResolved},
			{Ecosystem: "npm", Name: "left-pad", Resolution: model.ResolutionUnresolved, Reason: "no metadata"},
			{Ecosystem: "npm", Name: "noversion", SPDXID: "MIT", Resolution: model.ResolutionResolved},
		},
		LicenseCounts:  map[string]int{"MIT": 3, noLicenseKey: 1},
		CategoryCounts: map[string]int{"permissive": 3, "unknown": 1},
		FileTypeCounts: map[string]int{"Go": 4, "JSON": 1},
		Violations:     []string{"policy-violation"},
		Passed:         false,
	}
}

// emptyReport returns a Report with no files/deps/counts/violations to hit the
// "(none)"/empty-section branches in each renderer.
func emptyReport() model.Report {
	return model.Report{
		Root:           "/empty",
		Config:         model.Config{Style: model.StyleReuse},
		LicenseCounts:  map[string]int{},
		CategoryCounts: map[string]int{},
		FileTypeCounts: map[string]int{},
		Passed:         true,
	}
}

func TestRenderText(t *testing.T) {
	t.Run("representative", func(t *testing.T) {
		var buf bytes.Buffer
		require.NoError(t, Render(&buf, representativeReport(), FormatText))
		out := buf.String()

		assert.Contains(t, out, "license-tool audit report")
		assert.Contains(t, out, Disclaimer)
		assert.Contains(t, out, "root: /repo")
		assert.Contains(t, out, "license: MIT")
		assert.Contains(t, out, "holder: Kingsrook, LLC")
		assert.Contains(t, out, "result: FAIL")
		// Aggregates rendered.
		assert.Contains(t, out, "MIT")
		assert.Contains(t, out, noLicenseKey)
		assert.Contains(t, out, "permissive")
		// File lines: detected, skipped, error, headerless.
		assert.Contains(t, out, "a.go  [MIT]")
		assert.Contains(t, out, "skip.json  [skipped: uncommentable]")
		assert.Contains(t, out, "err.go  [error: read failed]")
		assert.Contains(t, out, "bare.go  [no managed header]")
		// Dep lines: resolved with id, unresolved with reason.
		assert.Contains(t, out, "maven/lib@1.0  [Apache-2.0]")
		assert.Contains(t, out, "npm/left-pad  [unresolved: no metadata]")
		assert.Contains(t, out, "npm/noversion  [MIT]")
		// Violations section.
		assert.Contains(t, out, "policy violations:")
		assert.Contains(t, out, "policy-violation")

		// Deterministic file ordering: a.go before z.go.
		assert.Less(t, strings.Index(out, "a.go"), strings.Index(out, "z.go"))
	})

	t.Run("empty hits none branches", func(t *testing.T) {
		var buf bytes.Buffer
		require.NoError(t, Render(&buf, emptyReport(), FormatText))
		out := buf.String()
		assert.Contains(t, out, "result: PASS")
		assert.Contains(t, out, "(no managed source files)")
		// by category / by file type empty -> "(none)" lines.
		assert.Contains(t, out, "(none)")
		// No violations section.
		assert.NotContains(t, out, "policy violations:")
	})
}

// fileLine covers the unresolved-dep "no reason" -> (none) and the empty-config
// orNone paths exercised through a tailored report.
func TestRenderTextOrNoneFallbacks(t *testing.T) {
	r := emptyReport()
	r.Config = model.Config{} // empty license + holder -> (none)
	r.Files = []model.FileResult{{Path: "x.go", Skipped: true, SkipReason: ""}}
	r.Dependencies = []model.DependencyLicense{
		{Ecosystem: "go", Name: "mod", Resolution: model.ResolutionUnresolved}, // no reason
	}
	var buf bytes.Buffer
	require.NoError(t, Render(&buf, r, FormatText))
	out := buf.String()
	assert.Contains(t, out, "license: (none)")
	assert.Contains(t, out, "holder: (none)")
	assert.Contains(t, out, "x.go  [skipped: (none)]")
	assert.Contains(t, out, "go/mod  [unresolved: (none)]")
}

func TestRenderMarkdown(t *testing.T) {
	t.Run("representative", func(t *testing.T) {
		var buf bytes.Buffer
		require.NoError(t, Render(&buf, representativeReport(), FormatMarkdown))
		out := buf.String()

		assert.Contains(t, out, "# license-tool audit report")
		assert.Contains(t, out, "> "+Disclaimer)
		assert.Contains(t, out, "**Root:** `/repo`")
		assert.Contains(t, out, "**License:** `MIT`")
		assert.Contains(t, out, "**Holder:** Kingsrook, LLC")
		assert.Contains(t, out, "**Result:** FAIL")
		assert.Contains(t, out, "## By SPDX id")
		assert.Contains(t, out, "| `MIT` | 3 |")
		assert.Contains(t, out, "## By category")
		assert.Contains(t, out, "| permissive | 3 |")
		assert.Contains(t, out, "## By file type")
		assert.Contains(t, out, "| Go | 4 |")
		assert.Contains(t, out, "## Source files (6)")
		// mdFileStatus branches.
		assert.Contains(t, out, "| `a.go` | Go | MIT |")
		assert.Contains(t, out, "| `skip.json` | JSON | skipped: uncommentable |")
		assert.Contains(t, out, "| `err.go` | Go | error: read failed |")
		assert.Contains(t, out, "| `bare.go` | Go | no managed header |")
		// Empty file type -> (none) in the file-type column.
		assert.Contains(t, out, "| `noft` | (none) | MIT |")
		assert.Contains(t, out, "## Dependencies (3)")
		assert.Contains(t, out, "| maven | `lib` | 1.0 | `Apache-2.0` | resolved |")
		assert.Contains(t, out, "| npm | `left-pad` | (none) | `(none)` | unresolved |")
		assert.Contains(t, out, "## Policy violations")
		assert.Contains(t, out, "- policy-violation")
	})

	t.Run("empty omits violations section", func(t *testing.T) {
		var buf bytes.Buffer
		require.NoError(t, Render(&buf, emptyReport(), FormatMarkdown))
		out := buf.String()
		assert.Contains(t, out, "**Result:** PASS")
		assert.Contains(t, out, "## Source files (0)")
		assert.Contains(t, out, "## Dependencies (0)")
		assert.NotContains(t, out, "## Policy violations")
	})
}

func TestRenderJSON(t *testing.T) {
	t.Run("representative", func(t *testing.T) {
		var buf bytes.Buffer
		require.NoError(t, Render(&buf, representativeReport(), FormatJSON))

		var got jsonReport
		require.NoError(t, json.Unmarshal(buf.Bytes(), &got))

		assert.Equal(t, "license-tool/report/v1", got.Schema)
		assert.Equal(t, Disclaimer, got.Disclaimer)
		assert.Equal(t, "/repo", got.Root)
		assert.False(t, got.Passed)
		assert.Equal(t, "MIT", got.Config.License)
		assert.Equal(t, "Kingsrook, LLC", got.Config.Holder)
		assert.Equal(t, "reuse+notice", got.Config.Style)
		assert.Equal(t, 3, got.LicenseCounts["MIT"])
		assert.Equal(t, 3, got.CategoryCounts["permissive"])
		assert.Equal(t, 4, got.FileTypeCounts["Go"])
		assert.Equal(t, []string{"policy-violation"}, got.Violations)

		require.Len(t, got.Files, 6)
		// Sorted by path: a.go first.
		assert.Equal(t, "a.go", got.Files[0].Path)
		assert.True(t, got.Files[0].HasHeader)
		assert.Equal(t, "MIT", got.Files[0].SPDXID)

		require.Len(t, got.Dependencies, 3)
		// Sorted by (ecosystem, name, version): maven before npm.
		assert.Equal(t, "maven", got.Dependencies[0].Ecosystem)
		assert.Equal(t, "resolved", got.Dependencies[0].Resolution)
		// Unresolved dep carries its reason.
		var foundUnresolved bool
		for _, d := range got.Dependencies {
			if d.Name == "left-pad" {
				foundUnresolved = true
				assert.Equal(t, "unresolved", d.Resolution)
				assert.Equal(t, "no metadata", d.Reason)
			}
		}
		assert.True(t, foundUnresolved)
	})

	t.Run("empty emits non-null aggregates and slices", func(t *testing.T) {
		var buf bytes.Buffer
		require.NoError(t, Render(&buf, emptyReport(), FormatJSON))
		out := buf.String()
		// Non-nil helpers => {} and [] rather than null.
		assert.Contains(t, out, `"licenseCounts": {}`)
		assert.Contains(t, out, `"violations": []`)
		assert.Contains(t, out, `"files": []`)
		assert.Contains(t, out, `"dependencies": []`)
		assert.NotContains(t, out, "null")
	})
}

// nonNilCounts / nonNilStrings nil branches: a Report with nil maps/slices must
// still serialize to {} / [] (the empty report uses non-nil maps, so exercise the
// nil path directly through a report with nil aggregates).
func TestRenderJSONNilAggregates(t *testing.T) {
	var buf bytes.Buffer
	r := model.Report{Root: "/x", Config: model.Config{Style: model.StyleNotice}}
	require.NoError(t, Render(&buf, r, FormatJSON))
	out := buf.String()
	assert.Contains(t, out, `"licenseCounts": {}`)
	assert.Contains(t, out, `"categoryCounts": {}`)
	assert.Contains(t, out, `"fileTypeCounts": {}`)
	assert.Contains(t, out, `"violations": []`)
	assert.Contains(t, out, `"notice"`)
}

// ----- helper functions exercised directly for completeness -----

func TestNonNilCounts(t *testing.T) {
	assert.NotNil(t, nonNilCounts(nil))
	m := map[string]int{"a": 1}
	assert.Equal(t, m, nonNilCounts(m))
}

func TestNonNilStrings(t *testing.T) {
	assert.NotNil(t, nonNilStrings(nil))
	s := []string{"a"}
	assert.Equal(t, s, nonNilStrings(s))
}

func TestPassLabel(t *testing.T) {
	assert.Equal(t, "PASS", passLabel(true))
	assert.Equal(t, "FAIL", passLabel(false))
}

func TestOrNone(t *testing.T) {
	assert.Equal(t, "(none)", orNone(""))
	assert.Equal(t, "x", orNone("x"))
}

func TestSortedFilesTieBreakers(t *testing.T) {
	// Same path forces the FileType tiebreaker, then the SPDXID tiebreaker.
	in := []model.FileResult{
		{Path: "p", FileType: "B", Detected: header("MIT")},
		{Path: "p", FileType: "A", Detected: header("Zlib")},
		{Path: "p", FileType: "A", Detected: header("Apache-2.0")},
	}
	out := sortedFiles(in)
	// FileType A sorts before B; within A, Apache-2.0 sorts before Zlib.
	assert.Equal(t, "A", out[0].FileType)
	assert.Equal(t, "Apache-2.0", out[0].Detected.SPDXID)
	assert.Equal(t, "A", out[1].FileType)
	assert.Equal(t, "Zlib", out[1].Detected.SPDXID)
	assert.Equal(t, "B", out[2].FileType)
}

func TestSortedDepsTieBreakers(t *testing.T) {
	in := []model.DependencyLicense{
		{Ecosystem: "npm", Name: "b", Version: "2"},
		{Ecosystem: "npm", Name: "a", Version: "9"},
		{Ecosystem: "maven", Name: "z", Version: "1"},
		{Ecosystem: "npm", Name: "a", Version: "1"},
	}
	out := sortedDeps(in)
	assert.Equal(t, "maven", out[0].Ecosystem)
	assert.Equal(t, "a", out[1].Name)
	assert.Equal(t, "1", out[1].Version) // a@1 before a@9
	assert.Equal(t, "a", out[2].Name)
	assert.Equal(t, "9", out[2].Version)
	assert.Equal(t, "b", out[3].Name)
}

// ----- errWriter short-circuit -----

// failingWriter returns an error after the first n successful writes, so we can
// assert errWriter captures the first error and stops.
type failingWriter struct {
	remaining int
}

func (f *failingWriter) Write(p []byte) (int, error) {
	if f.remaining <= 0 {
		return 0, errBoom
	}
	f.remaining--
	return len(p), nil
}

func TestRenderPropagatesWriteError(t *testing.T) {
	cases := []struct {
		name   string
		format Format
	}{
		{name: "text", format: FormatText},
		{name: "markdown", format: FormatMarkdown},
		{name: "json", format: FormatJSON},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// First write fails immediately -> error surfaces.
			w := &failingWriter{remaining: 0}
			err := Render(w, representativeReport(), tc.format)
			require.Error(t, err)
		})
	}
}

func TestErrWriterStopsAfterFirstError(t *testing.T) {
	// After the first error, subsequent printf calls are no-ops (the early-return
	// branch), and the captured error is the first one.
	w := &failingWriter{remaining: 1}
	bw := &errWriter{w: w}
	bw.printf("first ok\n")      // succeeds (remaining 1 -> 0)
	bw.printf("second err\n")    // fails, captures err
	bw.printf("third skipped\n") // no-op via early return
	require.Error(t, bw.err)
	assert.ErrorIs(t, bw.err, errBoom)
}

// ----- package-private orchestration seams (declared for the integration layer) -----

// readFile is the default unwired reader; its body returns a "not wired" error. It
// is called here so the closure body is covered (production wires it elsewhere).
func TestDefaultReadFileUnwired(t *testing.T) {
	_, err := readFile("/anything")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "file reader not wired")
}

// classifier wraps classifierFn; the default classifierFn returns a lookup that
// always reports "no match". Exercising both covers the default seam bodies.
func TestDefaultClassifierNoMatch(t *testing.T) {
	lookup := classifier(model.Config{})
	ft, ok := lookup("anything.go")
	assert.False(t, ok)
	assert.Equal(t, model.FileType{}, ft)
}
