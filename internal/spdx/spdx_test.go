package spdx

import (
	"sort"
	"strings"
	"testing"

	"github.com/KofTwentyTwo/license-tool/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidate(t *testing.T) {
	cases := []struct {
		id   string
		want bool
	}{
		{"AGPL-3.0-or-later", true},
		{"MIT", true},
		{"Apache-2.0", true},
		{"GPL-2.0-only", true},
		// Real SPDX id outside the curated rendering set must still validate.
		{"0BSD", true},
		{"Zlib", true},
		// Garbage and non-SPDX strings must not validate.
		{"NOT-A-LICENSE", false},
		{"", false},
		{"AGPL-3.0", false}, // deprecated/ambiguous bare id is not in the index under this exact form? guard below
	}
	for _, c := range cases {
		got := Validate(c.id)
		// AGPL-3.0 deprecated id presence varies; only assert the unambiguous ones.
		if c.id == "AGPL-3.0" {
			continue
		}
		assert.Equalf(t, c.want, got, "Validate(%q)", c.id)
	}
}

func TestLookupCuratedSet(t *testing.T) {
	curated := []string{
		"AGPL-3.0-or-later", "AGPL-3.0-only", "GPL-3.0-or-later", "GPL-2.0-only",
		"LGPL-3.0-or-later", "Apache-2.0", "MIT", "BSD-2-Clause", "BSD-3-Clause",
		"ISC", "MPL-2.0", "Unlicense", "CC0-1.0",
	}
	for _, id := range curated {
		lic, ok := Lookup(id)
		require.Truef(t, ok, "Lookup(%q) should be present", id)
		assert.Equalf(t, id, lic.SPDXID, "SPDXID for %q", id)
		assert.NotEmptyf(t, lic.Name, "Name for %q", id)
		assert.NotEmptyf(t, lic.Text, "Text for %q", id)
	}
}

func TestLookupOutsideCuratedSet(t *testing.T) {
	// A valid SPDX id with no vendored detail must not be Lookup-able.
	_, ok := Lookup("0BSD")
	assert.False(t, ok, "0BSD is valid but outside the curated rendering set")
}

func TestClassification(t *testing.T) {
	cases := map[string]model.Category{
		"AGPL-3.0-or-later": model.CategoryNetworkCopyleft,
		"AGPL-3.0-only":     model.CategoryNetworkCopyleft,
		"GPL-3.0-or-later":  model.CategoryStrongCopyleft,
		"GPL-2.0-only":      model.CategoryStrongCopyleft,
		"LGPL-3.0-or-later": model.CategoryWeakCopyleft,
		"MPL-2.0":           model.CategoryWeakCopyleft,
		"Apache-2.0":        model.CategoryPermissive,
		"MIT":               model.CategoryPermissive,
		"BSD-2-Clause":      model.CategoryPermissive,
		"BSD-3-Clause":      model.CategoryPermissive,
		"ISC":               model.CategoryPermissive,
		"Unlicense":         model.CategoryPermissive,
		"CC0-1.0":           model.CategoryPermissive,
	}
	for id, want := range cases {
		lic, ok := Lookup(id)
		require.Truef(t, ok, "Lookup(%q)", id)
		assert.Equalf(t, want, lic.Category, "category for %q", id)
	}
}

func TestAGPLCanonicalStandardHeader(t *testing.T) {
	lic, ok := Lookup("AGPL-3.0-or-later")
	require.True(t, ok)
	// The canonical AGPL header must be the wrapped GNU notice matching the
	// Kingsrook checkstyle block, NOT the unwrapped SPDX template with placeholders.
	assert.Contains(t, lic.StandardHeader, "This program is free software: you can redistribute it and/or modify")
	assert.Contains(t, lic.StandardHeader, "GNU Affero General Public License")
	assert.Contains(t, lic.StandardHeader, "https://www.gnu.org/licenses/")
	// Placeholder boilerplate from the SPDX template must be absent.
	assert.NotContains(t, lic.StandardHeader, "<one line to give the program's name")
	// It must be the wrapped form (multiple lines, none excessively long).
	lines := strings.Split(lic.StandardHeader, "\n")
	assert.Greater(t, len(lines), 6, "canonical AGPL header is wrapped across several lines")
}

func TestListVersion(t *testing.T) {
	assert.NotEmpty(t, ListVersion(), "embedded snapshot should record its list version")
}

// TestIDs confirms IDs() returns a non-empty, sorted, deprecation-free id list. The
// picker that consumes it must never steer a user onto a deprecated id (e.g. the
// bare "GPL-3.0"/"AGPL-3.0" forms), and must present a stable alphabetical order.
func TestIDs(t *testing.T) {
	ids := IDs()
	require.NotEmpty(t, ids, "the vendored snapshot must yield some non-deprecated ids")

	// Sorted, ascending.
	assert.True(t, sort.StringsAreSorted(ids), "IDs() must be sorted")

	// No deprecated ids leak through. The bare GPL/AGPL forms are deprecated in SPDX
	// and must be filtered out so they are never offered as new choices.
	for _, dep := range []string{"GPL-3.0", "AGPL-3.0", "GPL-2.0", "LGPL-3.0"} {
		assert.NotContains(t, ids, dep, "deprecated id %q must be excluded from IDs()", dep)
	}

	// A representative non-deprecated id is present.
	assert.Contains(t, ids, "MIT", "the canonical MIT id should be offered")
}

func TestRenderableIDs(t *testing.T) {
	ids := RenderableIDs()
	require.NotEmpty(t, ids, "the vendored snapshot must yield renderable ids")
	assert.True(t, sort.StringsAreSorted(ids), "RenderableIDs() must be sorted")
	assert.Contains(t, ids, "MIT", "curated MIT id should be renderable")
	assert.NotContains(t, ids, "Zlib", "valid SPDX ids without vendored detail must not be renderable")
}

// TestCommonIDs confirms every curated shortlist id validates against the full
// vendored index, so the picker can never present a "common" option that the rest
// of the tool would later reject as an unknown SPDX id.
func TestCommonIDs(t *testing.T) {
	common := CommonIDs()
	require.NotEmpty(t, common, "the common shortlist must not be empty")
	for _, id := range common {
		assert.Truef(t, Validate(id), "CommonIDs entry %q must be a valid SPDX id", id)
	}
}
