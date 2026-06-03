//go:build ignore

// Command gen_spdx vendors a pinned snapshot of the SPDX license-list-data into
// internal/spdx/data/ so the binary can validate and render licenses with zero
// network access at runtime.
//
// WHY a generator instead of committing a hand-curated file: the SPDX list is the
// single source of truth for license text and the official standardLicenseHeader.
// Hand-copying that text would risk transcription drift and legal inaccuracy, both
// of which this tool exists to prevent. The generator fetches the upstream JSON
// verbatim and pins it; refreshing is re-running this script against a new upstream
// ref. We never invent or paraphrase license text.
//
// It writes two artifacts (beside the package that embeds them, since go:embed
// cannot reference paths outside its own package directory):
//
//   - internal/spdx/data/index.json: the full id index (every SPDX id + deprecation
//     flag), used by spdx.Validate to accept any real SPDX id, not just the curated set.
//   - internal/spdx/data/licenses/<ID>.json: per-license detail (text +
//     standardLicenseHeader) for the curated set the tool ships rendering support for.
//
// Run with:
//
//	go run ./scripts/gen_spdx.go
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// upstreamRef pins the SPDX license-list-data git ref this snapshot is taken from.
// Bump this (and re-run) to refresh; recording it keeps the vendored data auditable.
const upstreamRef = "main"

const (
	indexURL      = "https://raw.githubusercontent.com/spdx/license-list-data/" + upstreamRef + "/json/licenses.json"
	detailURLTmpl = "https://raw.githubusercontent.com/spdx/license-list-data/" + upstreamRef + "/json/details/%s.json"
)

// curated is the set of licenses the tool ships full rendering support for. The
// generator fetches per-license detail (text + standard header) only for these;
// the full id index still covers every SPDX id for validation.
var curated = []string{
	"AGPL-3.0-or-later",
	"AGPL-3.0-only",
	"GPL-3.0-or-later",
	"GPL-2.0-only",
	"LGPL-3.0-or-later",
	"Apache-2.0",
	"MIT",
	"BSD-2-Clause",
	"BSD-3-Clause",
	"ISC",
	"MPL-2.0",
	"Unlicense",
	"CC0-1.0",
}

// indexFile is the trimmed shape we vendor for the id index. The upstream file
// carries per-entry cross-reference and OSI metadata we do not need at runtime,
// so we keep only what Validate consumes plus provenance fields.
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

// upstreamIndex matches the upstream json/licenses.json shape (only fields we read).
type upstreamIndex struct {
	LicenseListVersion string `json:"licenseListVersion"`
	ReleaseDate        string `json:"releaseDate"`
	Licenses           []struct {
		LicenseID             string `json:"licenseId"`
		Name                  string `json:"name"`
		IsDeprecatedLicenseID bool   `json:"isDeprecatedLicenseId"`
	} `json:"licenses"`
}

// detailFile is the trimmed per-license detail we vendor. We drop the HTML and
// template variants upstream ships; the tool renders from plain text only.
type detailFile struct {
	LicenseID             string `json:"licenseId"`
	Name                  string `json:"name"`
	IsDeprecatedLicenseID bool   `json:"isDeprecatedLicenseId"`
	LicenseText           string `json:"licenseText"`
	StandardLicenseHeader string `json:"standardLicenseHeader,omitempty"`
}

// upstreamDetail matches the upstream json/details/<ID>.json shape (fields we read).
type upstreamDetail struct {
	LicenseID             string `json:"licenseId"`
	Name                  string `json:"name"`
	IsDeprecatedLicenseID bool   `json:"isDeprecatedLicenseId"`
	LicenseText           string `json:"licenseText"`
	StandardLicenseHeader string `json:"standardLicenseHeader"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "gen_spdx:", err)
		os.Exit(1)
	}
}

func run() error {
	// WHY internal/spdx/data: go:embed cannot reference paths outside its own
	// package directory, so the vendored snapshot lives beside the package that
	// embeds it rather than at a repo-root data/spdx/.
	outDir := filepath.Join("internal", "spdx", "data")
	detailDir := filepath.Join(outDir, "licenses")
	if err := os.MkdirAll(detailDir, 0o755); err != nil {
		return err
	}

	fetchedAt := time.Now().UTC().Format(time.RFC3339)

	// Index.
	var up upstreamIndex
	if err := fetchJSON(indexURL, &up); err != nil {
		return fmt.Errorf("fetch index: %w", err)
	}
	idx := indexFile{
		LicenseListVersion: up.LicenseListVersion,
		ReleaseDate:        up.ReleaseDate,
		UpstreamRef:        upstreamRef,
		FetchedAt:          fetchedAt,
	}
	for _, l := range up.Licenses {
		idx.Licenses = append(idx.Licenses, indexEntry{
			LicenseID:             l.LicenseID,
			Name:                  l.Name,
			IsDeprecatedLicenseID: l.IsDeprecatedLicenseID,
		})
	}
	if err := writeJSON(filepath.Join(outDir, "index.json"), idx); err != nil {
		return err
	}
	fmt.Printf("wrote index.json (%d ids, list %s)\n", len(idx.Licenses), idx.LicenseListVersion)

	// Per-license detail for the curated set.
	for _, id := range curated {
		var d upstreamDetail
		if err := fetchJSON(fmt.Sprintf(detailURLTmpl, id), &d); err != nil {
			return fmt.Errorf("fetch detail %s: %w", id, err)
		}
		out := detailFile{
			LicenseID:             d.LicenseID,
			Name:                  d.Name,
			IsDeprecatedLicenseID: d.IsDeprecatedLicenseID,
			LicenseText:           d.LicenseText,
			StandardLicenseHeader: d.StandardLicenseHeader,
		}
		if err := writeJSON(filepath.Join(detailDir, id+".json"), out); err != nil {
			return err
		}
		fmt.Printf("wrote licenses/%s.json (text %d bytes, header %t)\n", id, len(out.LicenseText), out.StandardLicenseHeader != "")
	}
	return nil
}

func fetchJSON(url string, dst any) error {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, dst)
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(path, b, 0o644)
}
