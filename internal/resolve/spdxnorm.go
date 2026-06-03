package resolve

import (
	"strings"

	"github.com/github/go-spdx/v2/spdxexp"

	"github.com/KofTwentyTwo/license-tool/internal/spdx"
)

// normalizeSPDX maps a raw license string taken from package metadata to a
// canonical SPDX identifier, returning ok=false when the string cannot be
// positively identified. It NEVER guesses: a value that is neither a valid SPDX
// id nor a small set of well-known unambiguous aliases yields ok=false so the
// caller emits an unresolved result with a reason.
//
// WHY the alias map is deliberately tiny: the requirements forbid guessing.
// Aliases here are exact, industry-standard spellings that SPDX itself documents
// as deprecated-but-equivalent (e.g. "Apache 2.0" -> "Apache-2.0"); anything
// fuzzier is left unresolved on purpose rather than risk a wrong answer.
func normalizeSPDX(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", false
	}

	// Strip an npm "SEE LICENSE IN <file>" / "UNLICENSED" sentinel: these are
	// explicit non-answers in the npm ecosystem and must stay unresolved.
	upper := strings.ToUpper(s)
	if strings.HasPrefix(upper, "SEE LICENSE") || upper == "UNLICENSED" {
		return "", false
	}

	// First try the value verbatim as an SPDX expression. spdxexp validates a
	// single id ("MIT") or a compound expression ("MIT OR Apache-2.0"); we accept
	// only single-id expressions here because a DependencyLicense carries one id.
	if id, ok := validateSingleSPDX(s); ok {
		return id, true
	}

	// Then try the curated alias table for the small set of non-SPDX spellings
	// that appear ubiquitously in real-world metadata.
	if canon, ok := spdxAliases[strings.ToLower(s)]; ok {
		// Re-validate the canonical form against the vendored index so the alias
		// table can never introduce an id the rest of the tool does not know.
		if spdx.Validate(canon) {
			return canon, true
		}
	}

	return "", false
}

// validateSingleSPDX returns the canonical id when s is a single, valid,
// non-compound SPDX identifier. Compound expressions (OR/AND/WITH) resolve to
// more than one id and so are rejected: a single dependency record holds one id.
func validateSingleSPDX(s string) (string, bool) {
	ids, err := spdxexp.ExtractLicenses(s)
	if err != nil || len(ids) != 1 {
		return "", false
	}
	if ok, _ := spdxexp.ValidateLicenses(ids); !ok {
		return "", false
	}
	return ids[0], true
}

// spdxAliases maps lowercased, well-known non-SPDX license spellings seen in
// Maven POM <name> elements and npm package.json fields to their canonical SPDX
// id. Keys are lowercased; the map is intentionally conservative.
var spdxAliases = map[string]string{
	"apache 2.0":                       "Apache-2.0",
	"apache license 2.0":               "Apache-2.0",
	"apache license, version 2.0":      "Apache-2.0",
	"the apache software license, version 2.0": "Apache-2.0",
	"apache-2":                         "Apache-2.0",
	"mit license":                      "MIT",
	"the mit license":                  "MIT",
	"bsd":                              "BSD-2-Clause",
	"bsd license":                      "BSD-2-Clause",
	"new bsd license":                  "BSD-3-Clause",
	"3-clause bsd license":             "BSD-3-Clause",
	"bsd 3-clause":                     "BSD-3-Clause",
	"the bsd 3-clause license":         "BSD-3-Clause",
	"eclipse public license 2.0":       "EPL-2.0",
	"eclipse public license - v 2.0":   "EPL-2.0",
	"eclipse public license 1.0":       "EPL-1.0",
	"gnu lesser general public license": "LGPL-3.0-or-later",
	"isc license":                      "ISC",
	"the unlicense":                    "Unlicense",
	"mozilla public license 2.0":       "MPL-2.0",
	"gnu affero general public license v3.0 or later": "AGPL-3.0-or-later",
}
