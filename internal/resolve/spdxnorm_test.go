package resolve

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestValidateSingleSPDXValidateFailure covers validateSingleSPDX's
// ValidateLicenses-failure guard via the validateLicensesFn seam. In practice
// spdxexp.ExtractLicenses already rejects an unknown id with an error, so a single
// extracted id always validates; injecting a failing validator confirms the guard
// returns ("", false) without changing production behavior.
func TestValidateSingleSPDXValidateFailure(t *testing.T) {
	origValidate := validateLicensesFn
	t.Cleanup(func() { validateLicensesFn = origValidate })
	// Extraction of "MIT" yields exactly one id; the injected validator then rejects it.
	validateLicensesFn = func([]string) (bool, []string) { return false, []string{"MIT"} }

	id, ok := validateSingleSPDX("MIT")
	assert.False(t, ok)
	assert.Equal(t, "", id)
}

// TestNormalizeSPDX exercises every branch of normalizeSPDX: empty input, npm
// sentinels, a verbatim valid SPDX id, an alias-table hit, and an unrecognized
// string.
func TestNormalizeSPDX(t *testing.T) {
	tests := []struct {
		name   string
		raw    string
		wantID string
		wantOK bool
	}{
		{"empty", "", "", false},
		{"whitespace only", "   ", "", false},
		{"see license sentinel", "SEE LICENSE IN LICENSE.txt", "", false},
		{"see license lowercase", "see license in foo", "", false},
		{"unlicensed sentinel", "UNLICENSED", "", false},
		{"verbatim valid id", "MIT", "MIT", true},
		{"verbatim valid id with surrounding space", "  Apache-2.0  ", "Apache-2.0", true},
		{"alias apache 2.0", "Apache 2.0", "Apache-2.0", true},
		{"alias the apache software license", "The Apache Software License, Version 2.0", "Apache-2.0", true},
		{"alias mit license", "MIT License", "MIT", true},
		{"alias bsd 3-clause", "New BSD License", "BSD-3-Clause", true},
		{"alias eclipse 2.0", "Eclipse Public License 2.0", "EPL-2.0", true},
		{"alias agpl", "GNU Affero General Public License v3.0 or later", "AGPL-3.0-or-later", true},
		{"compound expression rejected", "MIT OR Apache-2.0", "", false},
		{"unrecognized string", "Totally Made Up License", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			id, ok := normalizeSPDX(tc.raw)
			assert.Equal(t, tc.wantOK, ok)
			assert.Equal(t, tc.wantID, id)
		})
	}
}

// TestValidateSingleSPDX covers the three rejection branches (parse error,
// multi-id expression, validation failure) plus the success path. A single valid
// id succeeds; "OR"/garbage are rejected.
func TestValidateSingleSPDX(t *testing.T) {
	tests := []struct {
		name   string
		s      string
		wantID string
		wantOK bool
	}{
		{"single valid id", "MIT", "MIT", true},
		{"compound is multi-id", "MIT OR Apache-2.0", "", false},
		{"three-id expression", "MIT OR Apache-2.0 OR ISC", "", false},
		{"parse error / unknown token", "NOT-A-REAL-LICENSE-ID", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			id, ok := validateSingleSPDX(tc.s)
			assert.Equal(t, tc.wantOK, ok)
			assert.Equal(t, tc.wantID, id)
		})
	}
}
