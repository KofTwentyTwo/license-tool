package initwizard

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/KofTwentyTwo/license-tool/internal/config"
	"github.com/KofTwentyTwo/license-tool/internal/detect"
	"github.com/KofTwentyTwo/license-tool/internal/enumerate"
	"github.com/KofTwentyTwo/license-tool/internal/spdx"
)

// Detected marks which seeded Answers fields came from repo detection, so the form
// can badge them and the user sees what was inferred versus defaulted.
type Detected struct {
	License bool
	Holder  bool
	Manage  bool
}

// SeedDeps holds injectable seams for Seed. WHY only the git author is a seam: the
// header scan is deterministic against a temp directory (the project's test style),
// but the configured git author depends on machine/CI git config, so it is injected.
type SeedDeps struct {
	// GitAuthor returns the configured author name for root, or "" when unknown.
	GitAuthor func(root string) string
}

// seedReadFile is a seam over os.ReadFile so the per-file read-error path is testable.
var seedReadFile = os.ReadFile

// Seed pre-fills base from repository detection and reports which fields were
// inferred. It never overrides a non-empty base field (e.g. one set from a flag).
// Detection reuses the same enumerate + detect + spdx machinery as audit, so it
// agrees with the rest of the tool, and it writes nothing: these are editable
// defaults the user reviews against the live preview.
func Seed(root string, base Answers, deps SeedDeps) (Answers, Detected) {
	if deps.GitAuthor == nil {
		deps.GitAuthor = gitAuthor
	}
	var detected Detected

	licenseID, holder := scanHeaders(root)

	if base.License.SPDXID == "" && licenseID != "" {
		// Only seed a license the tool can actually render; an existing header in an
		// id we cannot emit is left for the user to choose explicitly.
		if _, ok := spdx.Lookup(licenseID); ok {
			base.License.SPDXID = licenseID
			detected.License = true
		}
	}

	if base.Identity.Holder == "" {
		switch {
		case holder != "":
			base.Identity.Holder = holder
			detected.Holder = true
		default:
			if name := deps.GitAuthor(root); name != "" {
				base.Identity.Holder = name
				detected.Holder = true
			}
		}
	}

	if licenseFileExists(root) {
		detected.Manage = true
	}

	return base, detected
}

// scanHeaders enumerates source files and returns the most common detected license
// id and holder among recognized headers; empty strings when none are found.
func scanHeaders(root string) (licenseID, holder string) {
	classify := config.ContentLookupFunc(config.Defaults())
	entries, err := enumerate.WithContent(root, enumerate.Options{}, classify)
	if err != nil {
		return "", ""
	}

	licenseCounts := map[string]int{}
	holderCounts := map[string]int{}
	for _, e := range entries {
		if e.Skip {
			continue
		}
		content, rerr := seedReadFile(e.AbsPath)
		if rerr != nil {
			continue
		}
		dh, derr := detect.Detect(content, e.FileType)
		if derr != nil || !dh.Present {
			continue
		}
		if dh.SPDXID != "" {
			licenseCounts[dh.SPDXID]++
		}
		if h := strings.TrimSpace(dh.Holder); h != "" {
			holderCounts[h]++
		}
	}
	return mostCommon(licenseCounts), mostCommon(holderCounts)
}

// mostCommon returns the highest-count key, breaking ties by lexical order for
// determinism; empty when the map is empty.
func mostCommon(counts map[string]int) string {
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	best, bestCount := "", 0
	for _, k := range keys {
		if counts[k] > bestCount {
			best, bestCount = k, counts[k]
		}
	}
	return best
}

// licenseFileExists reports whether a top-level LICENSE file or LICENSES/ directory
// already exists, which the form uses to badge the manage-license-files choice.
func licenseFileExists(root string) bool {
	if _, err := os.Stat(filepath.Join(root, "LICENSE")); err == nil {
		return true
	}
	if info, err := os.Stat(filepath.Join(root, "LICENSES")); err == nil && info.IsDir() {
		return true
	}
	return false
}

// gitAuthor returns the configured git author name for root, or "" when git is
// absent or the value is unset.
func gitAuthor(root string) string {
	out, err := exec.Command("git", "-C", root, "config", "user.name").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
