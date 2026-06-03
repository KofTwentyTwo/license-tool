// Package spdx exposes the vendored SPDX license-list-data snapshot to the rest of
// the tool with zero runtime network access. The snapshot is produced by
// scripts/gen_spdx.go and embedded via go:embed.
//
// Two surfaces:
//
//   - Validate(id): accepts ANY real SPDX id from the full vendored index (729+ ids),
//     so users may target a license the tool does not ship rendering support for.
//   - Lookup(id): returns a fully-populated model.License (text + standard header +
//     category) for the curated set the tool ships rendering for.
//
// WHY two surfaces: validation must be permissive (any valid SPDX id is a legal
// target), but rendering can only emit text/headers we have vendored. Lookup
// therefore covers a smaller curated set than Validate.
//
// We never invent or paraphrase license text. The one deliberate override is the
// AGPL StandardHeader: SPDX ships an unwrapped template with boilerplate
// placeholder lines, whereas this tool's canonical AGPL profile uses the wrapped
// GNU notice block that matches the Kingsrook checkstyle header. The license body
// (License.Text) is always the verbatim SPDX text.
package spdx

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"
	"sync"

	"github.com/KofTwentyTwo/license-tool/internal/model"
)

// dataFS embeds the vendored snapshot. The patterns must stay in sync with the
// layout written by scripts/gen_spdx.go.
//
//go:embed data/index.json
//go:embed data/licenses/*.json
var dataFS embed.FS

// indexFile mirrors the trimmed index written by the generator.
type indexFile struct {
	LicenseListVersion string       `json:"licenseListVersion"`
	ReleaseDate        string       `json:"releaseDate"`
	UpstreamRef        string       `json:"upstreamRef"`
	FetchedAt          string       `json:"fetchedAt"`
	Licenses           []indexEntry `json:"licenses"`
}

type indexEntry struct {
	LicenseID             string `json:"licenseId"`
	Name                  string `json:"name"`
	IsDeprecatedLicenseID bool   `json:"isDeprecatedLicenseId"`
}

// detailFile mirrors the trimmed per-license detail written by the generator.
type detailFile struct {
	LicenseID             string `json:"licenseId"`
	Name                  string `json:"name"`
	IsDeprecatedLicenseID bool   `json:"isDeprecatedLicenseId"`
	LicenseText           string `json:"licenseText"`
	StandardLicenseHeader string `json:"standardLicenseHeader,omitempty"`
}

// store holds the parsed, embedded snapshot. It is loaded once, lazily, and is
// read-only thereafter, so concurrent Validate/Lookup callers are safe.
type store struct {
	ids      map[string]indexEntry    // licenseId -> index entry (full set)
	licenses map[string]model.License // licenseId -> curated license (rendering set)
	version  string
}

var (
	loadOnce sync.Once
	loaded   *store
	loadErr  error
)

func get() (*store, error) {
	loadOnce.Do(func() {
		loaded, loadErr = load()
	})
	return loaded, loadErr
}

func load() (*store, error) {
	return loadFrom(dataFS)
}

// loadFrom parses the snapshot from fsys. It is split out from load so tests can
// drive the error and skip branches with a synthetic filesystem; production
// always calls it with the embedded dataFS, so runtime behavior is unchanged.
func loadFrom(fsys fs.FS) (*store, error) {
	s := &store{
		ids:      make(map[string]indexEntry),
		licenses: make(map[string]model.License),
	}

	// Index (full id set).
	idxBytes, err := fs.ReadFile(fsys, "data/index.json")
	if err != nil {
		return nil, fmt.Errorf("read embedded index: %w", err)
	}
	var idx indexFile
	if uerr := json.Unmarshal(idxBytes, &idx); uerr != nil {
		return nil, fmt.Errorf("parse embedded index: %w", uerr)
	}
	s.version = idx.LicenseListVersion
	for _, e := range idx.Licenses {
		s.ids[e.LicenseID] = e
	}

	// Curated per-license details (rendering set).
	entries, err := fs.ReadDir(fsys, "data/licenses")
	if err != nil {
		return nil, fmt.Errorf("read embedded licenses dir: %w", err)
	}
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".json") {
			continue
		}
		b, err := fs.ReadFile(fsys, "data/licenses/"+ent.Name())
		if err != nil {
			return nil, fmt.Errorf("read embedded license %s: %w", ent.Name(), err)
		}
		var d detailFile
		if err := json.Unmarshal(b, &d); err != nil {
			return nil, fmt.Errorf("parse embedded license %s: %w", ent.Name(), err)
		}
		lic := model.License{
			SPDXID:         d.LicenseID,
			Name:           d.Name,
			Category:       classify(d.LicenseID),
			Text:           d.LicenseText,
			StandardHeader: standardHeaderFor(d.LicenseID, d.StandardLicenseHeader),
		}
		s.licenses[d.LicenseID] = lic
	}
	return s, nil
}

// Validate reports whether id is a real SPDX identifier present in the full
// vendored index. It is intentionally permissive: any valid SPDX id is a legal
// target even if the tool ships no rendering support for it.
func Validate(id string) bool {
	return validateFrom(get())(id)
}

// validateFrom builds the Validate lookup closure from a get() result. Splitting
// it out lets tests drive the load-error branch with an injected error; the
// embedded snapshot always loads, so production behavior is unchanged.
func validateFrom(s *store, err error) func(string) bool {
	return func(id string) bool {
		if err != nil {
			return false
		}
		_, ok := s.ids[id]
		return ok
	}
}

// Lookup returns the curated model.License for id (full text + standard header +
// category). The bool is false when id is outside the curated rendering set, even
// if Validate(id) is true.
func Lookup(id string) (model.License, bool) {
	return lookupFrom(get())(id)
}

// lookupFrom builds the Lookup closure from a get() result, so tests can drive
// the load-error branch with an injected error without changing production flow.
func lookupFrom(s *store, err error) func(string) (model.License, bool) {
	return func(id string) (model.License, bool) {
		if err != nil {
			return model.License{}, false
		}
		lic, ok := s.licenses[id]
		return lic, ok
	}
}

// ListVersion returns the SPDX license-list version this snapshot was taken from,
// for provenance display (e.g. license-tool version output).
func ListVersion() string {
	return listVersionFrom(get())
}

// listVersionFrom derives the version string from a get() result, so tests can
// drive the load-error branch with an injected error; production is unchanged.
func listVersionFrom(s *store, err error) string {
	if err != nil {
		return ""
	}
	return s.version
}

// classify maps a curated SPDX id to its model.Category. AGPL is both strong and
// network copyleft; we report it as network-copyleft since that is its defining,
// most-restrictive obligation, and the policy layer treats network-copyleft as a
// superset of strong for incompatibility purposes.
func classify(id string) model.Category {
	switch id {
	case "AGPL-3.0-or-later", "AGPL-3.0-only":
		return model.CategoryNetworkCopyleft
	case "GPL-3.0-or-later", "GPL-2.0-only":
		return model.CategoryStrongCopyleft
	case "LGPL-3.0-or-later", "MPL-2.0":
		return model.CategoryWeakCopyleft
	case "Apache-2.0", "MIT", "BSD-2-Clause", "BSD-3-Clause", "ISC", "Unlicense", "CC0-1.0":
		return model.CategoryPermissive
	default:
		return model.CategoryUnknown
	}
}

// agplStandardHeader is the canonical AGPL per-file notice block this tool emits.
// It is the wrapped GNU "This program is free software..." text matching the
// Kingsrook checkstyle header (qqq/checkstyle/license.txt lines 8-19), NOT the
// unwrapped SPDX template. Holder/year/program lines are layered on by the render
// package; this is only the immutable grant-and-warranty body.
const agplStandardHeader = `This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program.  If not, see <https://www.gnu.org/licenses/>.`

// standardHeaderFor returns the per-file standard header for id. For AGPL ids it
// returns the canonical wrapped block; otherwise it returns the vendored SPDX
// standardLicenseHeader verbatim (which may be empty).
func standardHeaderFor(id, vendored string) string {
	switch id {
	case "AGPL-3.0-or-later", "AGPL-3.0-only":
		return agplStandardHeader
	default:
		return vendored
	}
}
