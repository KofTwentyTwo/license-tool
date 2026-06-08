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

// String renders the dimension as its --group-by token (and the word used in
// rendered section headers). GroupNone renders as "none".
func (d GroupDimension) String() string {
	switch d {
	case GroupLicense:
		return "license"
	case GroupCategory:
		return "category"
	case GroupType:
		return "type"
	case GroupDirectory:
		return "directory"
	default:
		return "none"
	}
}

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

// GroupSpec parameterizes GroupFiles: the dimension to group by and, for the
// directory dimension, how many leading path segments form the key (Depth, min 1).
type GroupSpec struct {
	By    GroupDimension
	Depth int
}

// OnlyFilter restricts the source-file listing to "problem" files.
type OnlyFilter int

const (
	// OnlyMissing keeps files with no managed header.
	OnlyMissing OnlyFilter = iota
	// OnlyUnknown keeps files whose detected license is unclassifiable.
	OnlyUnknown
	// OnlyCopyleft keeps files under a copyleft license (weak/strong/network).
	OnlyCopyleft
	// OnlyViolations keeps files carrying a policy violation.
	OnlyViolations
)

// ParseOnly parses a comma-separated --only spec into filters. Empty yields none (no
// filtering); an unrecognized token is a usage error.
func ParseOnly(raw string) ([]OnlyFilter, error) {
	var out []OnlyFilter
	for _, tok := range strings.Split(raw, ",") {
		tok = strings.TrimSpace(tok)
		switch tok {
		case "":
			continue
		case "missing":
			out = append(out, OnlyMissing)
		case "unknown":
			out = append(out, OnlyUnknown)
		case "copyleft":
			out = append(out, OnlyCopyleft)
		case "violations":
			out = append(out, OnlyViolations)
		default:
			return nil, fmt.Errorf("report: unknown --only filter %q (expected missing|unknown|copyleft|violations)", tok)
		}
	}
	return out, nil
}

// keepFile reports whether fr passes the --only filters (matches ANY). An empty
// filter set keeps everything; skipped files never match a problem filter.
func keepFile(fr model.FileResult, only []OnlyFilter) bool {
	if len(only) == 0 {
		return true
	}
	if fr.Skipped {
		return false
	}
	for _, f := range only {
		switch f {
		case OnlyMissing:
			if !fr.Detected.Present {
				return true
			}
		case OnlyUnknown:
			if fr.Detected.Present && fr.Detected.SPDXID != "" && classifyCategory(fr.Detected.SPDXID) == model.CategoryUnknown {
				return true
			}
		case OnlyCopyleft:
			switch classifyCategory(fr.Detected.SPDXID) {
			case model.CategoryWeakCopyleft, model.CategoryStrongCopyleft, model.CategoryNetworkCopyleft:
				return true
			}
		case OnlyViolations:
			if len(fr.Violations) > 0 {
				return true
			}
		}
	}
	return false
}

// Group is one bucket of the grouped source-file view: a key, the count of files in
// it, the worst obligation risk among its files, the license breakdown within it, and
// the files themselves (sorted by path).
type Group struct {
	Key   string
	Count int
	// Risk is the worst category risk among the group's files ("high"|"medium"|
	// "low"|"none"), so directory/type groups carry a license-risk signal instead of
	// being a license-blind count.
	Risk string
	// Licenses is the license breakdown within the group (SPDX id or "(no-header)" ->
	// count), so a directory/type group shows WHICH licenses it mixes, not just a count.
	Licenses map[string]int
	Files    []model.FileResult
}

// GroupFiles partitions the non-skipped source files of r per spec, returning the
// groups (sorted by key, files sorted by path) and the count of skipped files
// (binary/uncommentable/unknown) which are never grouped. GroupNone returns no
// groups. Pure and deterministic for identical input.
func GroupFiles(r model.Report, spec GroupSpec) (groups []Group, skipped int) {
	buckets := map[string][]model.FileResult{}
	for _, fr := range r.Files {
		if fr.Skipped {
			skipped++
			continue
		}
		if spec.By == GroupNone {
			continue
		}
		buckets[groupKeyDepth(fr, spec)] = append(buckets[groupKeyDepth(fr, spec)], fr)
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
		groups = append(groups, Group{
			Key:      k,
			Count:    len(files),
			Risk:     groupRisk(files),
			Licenses: licenseBreakdown(files),
			Files:    files,
		})
	}
	return groups, skipped
}

// groupKeyDepth derives a file's bucket key, honoring the directory depth.
func groupKeyDepth(fr model.FileResult, spec GroupSpec) string {
	if spec.By == GroupDirectory {
		return topDirs(fr.Path, spec.Depth)
	}
	return groupKey(fr, spec.By)
}

// licenseBreakdown counts the license (or no-header) of each file in a group.
func licenseBreakdown(files []model.FileResult) map[string]int {
	out := map[string]int{}
	for _, fr := range files {
		out[groupKey(fr, GroupLicense)]++
	}
	return out
}

// groupRisk returns the worst category risk among a group's files.
func groupRisk(files []model.FileResult) string {
	var w worstRisk
	for _, fr := range files {
		if fr.Detected.Present && fr.Detected.SPDXID != "" {
			w.observe(classifyCategory(fr.Detected.SPDXID))
		}
	}
	level, _ := w.result()
	if level == "none" {
		// A non-empty group with no classifiable license (all missing/headerless) is an
		// audit liability, not "all clear" -- report it as unknown risk.
		return "unknown"
	}
	return level
}

// sortGroups orders groups by descending count (ties by key) when byCount is set,
// otherwise leaves the key-sorted order GroupFiles produced.
func sortGroups(groups []Group, byCount bool) {
	if !byCount {
		return
	}
	sort.SliceStable(groups, func(i, j int) bool {
		if groups[i].Count != groups[j].Count {
			return groups[i].Count > groups[j].Count
		}
		return groups[i].Key < groups[j].Key
	})
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
	default: // GroupType
		if fr.FileType != "" {
			return fr.FileType
		}
		return "(unknown type)"
	}
}

// topDirs returns the first depth directory segments of a forward-slash path (the
// directory containing the file, capped at depth). A file with fewer leading
// directories than depth groups under its own directory; root-level files group
// under ".". Depth below 1 is treated as 1.
func topDirs(path string, depth int) string {
	if depth < 1 {
		depth = 1
	}
	p := filepath.ToSlash(path)
	segments := strings.Split(p, "/")
	if len(segments) <= 1 {
		return "." // file at root, no directory
	}
	dirs := segments[:len(segments)-1] // drop the file name
	if len(dirs) > depth {
		dirs = dirs[:depth]
	}
	return strings.Join(dirs, "/")
}
