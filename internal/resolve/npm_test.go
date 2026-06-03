package resolve

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/KofTwentyTwo/license-tool/internal/model"
)

// writeInstalledPkg writes node_modules/<name>/package.json with the given body.
func writeInstalledPkg(t *testing.T, root, name, body string) {
	t.Helper()
	dir := filepath.Join(root, "node_modules", filepath.FromSlash(name))
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), []byte(body), 0o644))
}

func TestNPMEcosystemAndDetect(t *testing.T) {
	r := &NPMResolver{}
	assert.Equal(t, "npm", r.Ecosystem())

	dir := t.TempDir()
	assert.False(t, r.Detect(dir))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0o644))
	assert.True(t, r.Detect(dir))
}

// TestNPMResolveRootError verifies a missing or malformed root package.json is a
// hard error.
func TestNPMResolveRootError(t *testing.T) {
	dir := t.TempDir()
	r := &NPMResolver{}
	_, err := r.Resolve(dir, model.ResolveOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "npm: read")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), []byte("{ not json"), 0o644))
	_, err = r.Resolve(dir, model.ResolveOptions{})
	require.Error(t, err)
}

// TestNPMResolve drives Resolve across resolved and unresolved outcomes for both
// dependencies and devDependencies, with various license shapes installed.
func TestNPMResolve(t *testing.T) {
	dir := t.TempDir()

	root := `{
  "name": "root",
  "version": "1.0.0",
  "dependencies": {
    "string-lic": "^1.0.0",
    "object-lic": "^1.0.0",
    "array-lic": "^1.0.0",
    "bad-lic": "^1.0.0",
    "no-lic": "^1.0.0",
    "@scope/pkg": "^1.0.0",
    "not-installed": "^1.0.0"
  },
  "devDependencies": {
    "dev-string-lic": "^2.0.0"
  }
}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), []byte(root), 0o644))

	writeInstalledPkg(t, dir, "string-lic", `{"name":"string-lic","version":"1.2.3","license":"MIT"}`)
	writeInstalledPkg(t, dir, "object-lic", `{"name":"object-lic","version":"1.0.0","license":{"type":"Apache-2.0","url":"http://x"}}`)
	writeInstalledPkg(t, dir, "array-lic", `{"name":"array-lic","version":"1.0.0","licenses":[{"type":"ISC"}]}`)
	writeInstalledPkg(t, dir, "bad-lic", `{"name":"bad-lic","version":"4.0.0","license":"Totally Made Up"}`)
	writeInstalledPkg(t, dir, "no-lic", `{"name":"no-lic","version":"5.0.0"}`)
	writeInstalledPkg(t, dir, "@scope/pkg", `{"name":"@scope/pkg","version":"9.9.9","license":"BSD-3-Clause"}`)
	writeInstalledPkg(t, dir, "dev-string-lic", `{"name":"dev-string-lic","version":"2.0.0","license":"MIT"}`)
	// not-installed: intentionally absent from node_modules.

	r := &NPMResolver{}
	out, err := r.Resolve(dir, model.ResolveOptions{})
	require.NoError(t, err)

	byName := map[string]model.DependencyLicense{}
	for _, d := range out {
		byName[d.Name] = d
	}

	assert.Equal(t, model.ResolutionResolved, byName["string-lic"].Resolution)
	assert.Equal(t, "MIT", byName["string-lic"].SPDXID)
	assert.Equal(t, "1.2.3", byName["string-lic"].Version)

	assert.Equal(t, "Apache-2.0", byName["object-lic"].SPDXID)
	assert.Equal(t, "ISC", byName["array-lic"].SPDXID)
	assert.Equal(t, "BSD-3-Clause", byName["@scope/pkg"].SPDXID)
	assert.Equal(t, "MIT", byName["dev-string-lic"].SPDXID)

	// Unrecognized license string -> unresolved but version still captured.
	bad := byName["bad-lic"]
	assert.Equal(t, model.ResolutionUnresolved, bad.Resolution)
	assert.Equal(t, "4.0.0", bad.Version)
	assert.Contains(t, bad.Reason, "not recognized as SPDX")

	// No license field -> unresolved, version still captured.
	noLic := byName["no-lic"]
	assert.Equal(t, model.ResolutionUnresolved, noLic.Resolution)
	assert.Equal(t, "5.0.0", noLic.Version)
	assert.Contains(t, noLic.Reason, "no license field")

	// Not installed -> unresolved, no version.
	ni := byName["not-installed"]
	assert.Equal(t, model.ResolutionUnresolved, ni.Resolution)
	assert.Empty(t, ni.Version)
	assert.Contains(t, ni.Reason, "not installed under node_modules/")
}

// TestResolveInstalledNPM covers the unreadable-package.json branch directly: a
// node_modules/<name>/package.json that exists but is malformed.
func TestResolveInstalledNPM(t *testing.T) {
	dir := t.TempDir()
	nodeModules := filepath.Join(dir, "node_modules")

	// Not installed.
	id, ver, reason := resolveInstalledNPM(nodeModules, "ghost")
	assert.Empty(t, id)
	assert.Empty(t, ver)
	assert.Contains(t, reason, "not installed")

	// Installed but malformed JSON.
	writeInstalledPkg(t, dir, "broken", "{ not json")
	id, ver, reason = resolveInstalledNPM(nodeModules, "broken")
	assert.Empty(t, id)
	assert.Empty(t, ver)
	assert.Contains(t, reason, "unreadable")
}

// TestDeclaredDependencyNames verifies the sorted union of deps and devDeps with
// de-duplication.
func TestDeclaredDependencyNames(t *testing.T) {
	p := packageJSON{
		Dependencies:    map[string]string{"b": "1", "a": "1", "shared": "1"},
		DevDependencies: map[string]string{"c": "2", "shared": "2"},
	}
	got := declaredDependencyNames(p)
	assert.Equal(t, []string{"a", "b", "c", "shared"}, got)

	assert.Empty(t, declaredDependencyNames(packageJSON{}))
}

// TestLicenseStringFromPackage covers each license-shape branch.
func TestLicenseStringFromPackage(t *testing.T) {
	tests := []struct {
		name       string
		pkg        packageJSON
		wantRaw    string
		wantReason string
	}{
		{
			name:    "string form",
			pkg:     packageJSON{License: []byte(`"MIT"`)},
			wantRaw: "MIT",
		},
		{
			name:    "string form trimmed",
			pkg:     packageJSON{License: []byte(`"  MIT  "`)},
			wantRaw: "MIT",
		},
		{
			name:    "object form",
			pkg:     packageJSON{License: []byte(`{"type":"Apache-2.0","url":"http://x"}`)},
			wantRaw: "Apache-2.0",
		},
		{
			name:    "array form first usable wins",
			pkg:     packageJSON{Licenses: []licenseObject{{Type: "  "}, {Type: "ISC"}}},
			wantRaw: "ISC",
		},
		{
			name:    "empty string license falls through to array",
			pkg:     packageJSON{License: []byte(`""`), Licenses: []licenseObject{{Type: "MIT"}}},
			wantRaw: "MIT",
		},
		{
			name:    "object with empty type falls through to array",
			pkg:     packageJSON{License: []byte(`{"type":"  "}`), Licenses: []licenseObject{{Type: "ISC"}}},
			wantRaw: "ISC",
		},
		{
			name:       "no license at all",
			pkg:        packageJSON{},
			wantReason: "no license field",
		},
		{
			name:       "license is malformed non-string non-object number, no array",
			pkg:        packageJSON{License: []byte(`123`)},
			wantReason: "no license field",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			raw, reason := licenseStringFromPackage(tc.pkg)
			assert.Equal(t, tc.wantRaw, raw)
			if tc.wantReason == "" {
				assert.Empty(t, reason)
			} else {
				assert.Contains(t, reason, tc.wantReason)
			}
		})
	}
}

// TestParsePackageJSON covers read-error, unmarshal-error, and success.
func TestParsePackageJSON(t *testing.T) {
	dir := t.TempDir()

	_, err := parsePackageJSON(filepath.Join(dir, "missing.json"))
	require.Error(t, err)

	bad := filepath.Join(dir, "bad.json")
	require.NoError(t, os.WriteFile(bad, []byte("{ not json"), 0o644))
	_, err = parsePackageJSON(bad)
	require.Error(t, err)

	good := filepath.Join(dir, "good.json")
	require.NoError(t, os.WriteFile(good, []byte(`{"name":"x","version":"1.0.0"}`), 0o644))
	p, err := parsePackageJSON(good)
	require.NoError(t, err)
	assert.Equal(t, "x", p.Name)
	assert.Equal(t, "1.0.0", p.Version)
}
