package spdx

import (
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/KofTwentyTwo/license-tool/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// errReadFS wraps an fstest.MapFS and forces Open (and thus fs.ReadFile) to fail
// for failName, while leaving directory listing intact. This lets a test reach
// the per-license ReadFile error branch: the entry is listed by ReadDir but
// cannot be read.
type errReadFS struct {
	fstest.MapFS
	failName string
}

func (e errReadFS) Open(name string) (fs.File, error) {
	if name == e.failName {
		return nil, &fs.PathError{Op: "open", Path: name, Err: errors.New("synthetic read failure")}
	}
	return e.MapFS.Open(name)
}

// ReadFile is overridden because fstest.MapFS implements ReadFileFS; fs.ReadFile
// would otherwise call the promoted MapFS.ReadFile and bypass the Open override.
func (e errReadFS) ReadFile(name string) ([]byte, error) {
	if name == e.failName {
		return nil, &fs.PathError{Op: "open", Path: name, Err: errors.New("synthetic read failure")}
	}
	return e.MapFS.ReadFile(name)
}

// errStore is a sentinel used to drive the load-error branches of the public
// accessors without touching the embedded snapshot that production always uses.
var errStore = errors.New("synthetic load failure")

const goodIndex = `{
  "licenseListVersion": "test-1.0",
  "releaseDate": "2026-05-28T00:00:00Z",
  "upstreamRef": "main",
  "fetchedAt": "2026-06-03T00:00:00Z",
  "licenses": [
    {"licenseId": "MIT", "name": "MIT License", "isDeprecatedLicenseId": false}
  ]
}`

const mitDetail = `{
  "licenseId": "MIT",
  "name": "MIT License",
  "isDeprecatedLicenseId": false,
  "licenseText": "MIT License body"
}`

// TestLoadFromHappyPath confirms loadFrom builds a populated store from a
// well-formed synthetic filesystem, exercising the curated-detail loop body.
func TestLoadFromHappyPath(t *testing.T) {
	fsys := fstest.MapFS{
		"data/index.json":          {Data: []byte(goodIndex)},
		"data/licenses/MIT.json":   {Data: []byte(mitDetail)},
		"data/licenses/README.txt": {Data: []byte("not a license json, must be skipped")},
		// A file under a subdirectory makes "nested" a synthesized directory
		// entry in ReadDir("data/licenses"), exercising the IsDir() skip arm.
		"data/licenses/nested/inner.json": {Data: []byte(mitDetail)},
	}

	s, err := loadFrom(fsys)
	require.NoError(t, err)
	assert.Equal(t, "test-1.0", s.version)

	_, ok := s.ids["MIT"]
	assert.True(t, ok, "index id should be present")

	lic, ok := s.licenses["MIT"]
	require.True(t, ok, "curated MIT detail should be loaded")
	assert.Equal(t, "MIT License body", lic.Text)
	assert.Equal(t, model.CategoryPermissive, lic.Category)

	// The non-.json file and the directory entry must not have produced licenses.
	assert.Len(t, s.licenses, 1, "only MIT.json should yield a curated license")
}

// TestLoadFromErrors drives every error branch of loadFrom with synthetic
// filesystems that fail at each stage.
func TestLoadFromErrors(t *testing.T) {
	cases := []struct {
		name    string
		fsys    fstest.MapFS
		wantSub string
	}{
		{
			name:    "missing index",
			fsys:    fstest.MapFS{}, // no data/index.json
			wantSub: "read embedded index",
		},
		{
			name: "malformed index json",
			fsys: fstest.MapFS{
				"data/index.json": {Data: []byte("{ this is not json")},
			},
			wantSub: "parse embedded index",
		},
		{
			name: "missing licenses dir",
			fsys: fstest.MapFS{
				"data/index.json": {Data: []byte(goodIndex)},
			},
			wantSub: "read embedded licenses dir",
		},
		{
			name: "malformed license detail json",
			fsys: fstest.MapFS{
				"data/index.json":        {Data: []byte(goodIndex)},
				"data/licenses/Bad.json": {Data: []byte("{ not valid json")},
			},
			wantSub: "parse embedded license Bad.json",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, err := loadFrom(c.fsys)
			require.Error(t, err)
			assert.Nil(t, s)
			assert.Contains(t, err.Error(), c.wantSub)
		})
	}
}

// TestLoadFromReadFileError covers the per-license ReadFile error branch using a
// filesystem whose directory listing advertises an entry that cannot be read.
func TestLoadFromReadFileError(t *testing.T) {
	fsys := errReadFS{
		MapFS: fstest.MapFS{
			"data/index.json":        {Data: []byte(goodIndex)},
			"data/licenses/MIT.json": {Data: []byte(mitDetail)},
		},
		failName: "data/licenses/MIT.json",
	}

	s, err := loadFrom(fsys)
	require.Error(t, err)
	assert.Nil(t, s)
	assert.Contains(t, err.Error(), "read embedded license MIT.json")
}

// TestAccessorsWithLoadError covers the get()-error guard inside the public
// accessors by invoking the extracted *From helpers with an injected error.
// Production always loads the embedded snapshot, so these branches are otherwise
// unreachable.
func TestAccessorsWithLoadError(t *testing.T) {
	assert.False(t, validateFrom(nil, errStore)("MIT"), "Validate must report false on load error")

	lic, ok := lookupFrom(nil, errStore)("MIT")
	assert.False(t, ok, "Lookup must report not-found on load error")
	assert.Equal(t, model.License{}, lic, "Lookup must return the zero License on load error")

	assert.Equal(t, "", listVersionFrom(nil, errStore), "ListVersion must be empty on load error")

	assert.Nil(t, idsFrom(nil, errStore), "IDs must be nil on load error so the picker degrades to empty")
}

// TestAccessorsFromHelpersHappyPath confirms the extracted helpers preserve the
// non-error behavior they were factored out of.
func TestAccessorsFromHelpersHappyPath(t *testing.T) {
	s := &store{
		ids:      map[string]indexEntry{"MIT": {LicenseID: "MIT", Name: "MIT License"}},
		licenses: map[string]model.License{"MIT": {SPDXID: "MIT", Text: "body"}},
		version:  "test-1.0",
	}

	assert.True(t, validateFrom(s, nil)("MIT"))
	assert.False(t, validateFrom(s, nil)("NOPE"))

	lic, ok := lookupFrom(s, nil)("MIT")
	assert.True(t, ok)
	assert.Equal(t, "MIT", lic.SPDXID)

	_, ok = lookupFrom(s, nil)("NOPE")
	assert.False(t, ok)

	assert.Equal(t, "test-1.0", listVersionFrom(s, nil))
}

// TestClassifyAllBranches drives every arm of classify, including the default
// (CategoryUnknown) branch for ids outside the curated set.
func TestClassifyAllBranches(t *testing.T) {
	cases := []struct {
		id   string
		want model.Category
	}{
		{"AGPL-3.0-or-later", model.CategoryNetworkCopyleft},
		{"AGPL-3.0-only", model.CategoryNetworkCopyleft},
		{"GPL-3.0-or-later", model.CategoryStrongCopyleft},
		{"GPL-2.0-only", model.CategoryStrongCopyleft},
		{"LGPL-3.0-or-later", model.CategoryWeakCopyleft},
		{"MPL-2.0", model.CategoryWeakCopyleft},
		{"Apache-2.0", model.CategoryPermissive},
		{"MIT", model.CategoryPermissive},
		{"BSD-2-Clause", model.CategoryPermissive},
		{"BSD-3-Clause", model.CategoryPermissive},
		{"ISC", model.CategoryPermissive},
		{"Unlicense", model.CategoryPermissive},
		{"CC0-1.0", model.CategoryPermissive},
		// Default branch: a real-but-uncurated id and outright garbage.
		{"0BSD", model.CategoryUnknown},
		{"NOT-A-LICENSE", model.CategoryUnknown},
		{"", model.CategoryUnknown},
	}

	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			assert.Equalf(t, c.want, classify(c.id), "classify(%q)", c.id)
		})
	}
}

// TestStandardHeaderForBranches covers both arms of standardHeaderFor: the AGPL
// override and the verbatim pass-through.
func TestStandardHeaderForBranches(t *testing.T) {
	cases := []struct {
		name     string
		id       string
		vendored string
		want     string
	}{
		{"agpl or-later override", "AGPL-3.0-or-later", "SPDX TEMPLATE", agplStandardHeader},
		{"agpl only override", "AGPL-3.0-only", "SPDX TEMPLATE", agplStandardHeader},
		{"non-agpl passthrough", "MIT", "vendored header text", "vendored header text"},
		{"non-agpl empty passthrough", "Apache-2.0", "", ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, standardHeaderFor(c.id, c.vendored))
		})
	}
}
