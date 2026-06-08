package report

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/KofTwentyTwo/license-tool/internal/model"
)

// GroupDimension selects how the source-file listing is organized for the
// --group-by option. GroupNone is the default flat listing.
type GroupDimension int

const (
	// GroupNone is the default: a flat, ungrouped file listing.
	GroupNone GroupDimension = iota
	// GroupLicense groups files by detected SPDX id.
	GroupLicense
	// GroupCategory groups files by license category (permissive, copyleft, ...).
	GroupCategory
	// GroupType groups files by matched file-type name.
	GroupType
	// GroupDirectory groups files by top-level path segment.
	GroupDirectory
)

// ParseGroupBy parses a --group-by token. An empty token is GroupNone (no error);
// an unrecognized token is a usage error.
func ParseGroupBy(raw string) (GroupDimension, error) {
	switch raw {
	case "":
		return GroupNone, nil
	case "license":
		return GroupLicense, nil
	case "category":
		return GroupCategory, nil
	case "type":
		return GroupType, nil
	case "directory":
		return GroupDirectory, nil
	default:
		return GroupNone, fmt.Errorf("report: unknown group-by dimension %q (expected license|category|type|directory)", raw)
	}
}

// Group is one bucket of the grouped source-file view: a key, the count of files in
// it, and the files themselves (sorted by path).
type Group struct {
	Key   string
	Count int
	Files []model.FileResult
}

// GroupFiles partitions the non-skipped source files of r by dim, returning the
// groups (sorted by key, files sorted by path) and the count of skipped files
// (binary/uncommentable/unknown) which are never grouped. GroupNone returns no
// groups. Pure and deterministic for identical input.
func GroupFiles(r model.Report, dim GroupDimension) (groups []Group, skipped int) {
	buckets := map[string][]model.FileResult{}
	for _, fr := range r.Files {
		if fr.Skipped {
			skipped++
			continue
		}
		if dim == GroupNone {
			continue
		}
		key := groupKey(fr, dim)
		buckets[key] = append(buckets[key], fr)
	}

	keys := make([]string, 0, len(buckets))
	for k := range buckets {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	groups = make([]Group, 0, len(keys))
	for _, k := range keys {
		files := buckets[k]
		sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
		groups = append(groups, Group{Key: k, Count: len(files), Files: files})
	}
	return groups, skipped
}

// groupKey derives a file's bucket key for the given dimension.
func groupKey(fr model.FileResult, dim GroupDimension) string {
	switch dim {
	case GroupLicense:
		if fr.Detected.Present && fr.Detected.SPDXID != "" {
			return fr.Detected.SPDXID
		}
		return noLicenseKey
	case GroupCategory:
		if fr.Detected.Present && fr.Detected.SPDXID != "" {
			return categoryToken(fr.Detected.SPDXID)
		}
		return model.CategoryUnknown.String()
	case GroupType:
		if fr.FileType != "" {
			return fr.FileType
		}
		return "(unknown type)"
	default: // GroupDirectory
		return topDir(fr.Path)
	}
}

// topDir returns the first path segment of a forward-slash path; root-level files
// group under ".".
func topDir(path string) string {
	p := filepath.ToSlash(path)
	if i := strings.IndexByte(p, '/'); i >= 0 {
		return p[:i]
	}
	return "."
}
