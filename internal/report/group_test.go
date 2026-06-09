package report

import (
	"testing"

	"github.com/KofTwentyTwo/license-tool/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func headered(path, ft, id string) model.FileResult {
	return model.FileResult{Path: path, FileType: ft, Detected: model.DetectedHeader{Present: true, SPDXID: id}}
}

func TestParseGroupBy(t *testing.T) {
	cases := map[string]GroupDimension{
		"":          GroupNone,
		"license":   GroupLicense,
		"category":  GroupCategory,
		"type":      GroupType,
		"directory": GroupDirectory,
	}
	for in, want := range cases {
		got, err := ParseGroupBy(in)
		require.NoError(t, err, "ParseGroupBy(%q)", in)
		assert.Equal(t, want, got)
	}

	_, err := ParseGroupBy("nonsense")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown group-by dimension")
}

func sampleReport() model.Report {
	return model.Report{Files: []model.FileResult{
		headered("src/a.go", "Go", "MIT"),
		headered("src/b.go", "Go", "MIT"),
		headered("web/c.ts", "TypeScript/JavaScript", "AGPL-3.0-or-later"),
		{Path: "src/headerless.go", FileType: "Go"},                               // non-skipped, no header
		{Path: "img/logo.png", FileType: "", Skipped: true, SkipReason: "binary"}, // skipped
	}}
}

func TestGroupFilesNone(t *testing.T) {
	groups, skipped := GroupFiles(sampleReport(), GroupSpec{By: GroupNone})
	assert.Empty(t, groups)
	assert.Equal(t, 1, skipped)
}

func TestGroupFilesByLicense(t *testing.T) {
	groups, skipped := GroupFiles(sampleReport(), GroupSpec{By: GroupLicense})
	assert.Equal(t, 1, skipped)
	// Keys sorted: "(no-header)" < "AGPL-3.0-or-later" < "MIT".
	require.Len(t, groups, 3)
	assert.Equal(t, "(no-header)", groups[0].Key)
	assert.Equal(t, 1, groups[0].Count)
	assert.Equal(t, "src/headerless.go", groups[0].Files[0].Path)
	assert.Equal(t, "AGPL-3.0-or-later", groups[1].Key)
	assert.Equal(t, "MIT", groups[2].Key)
	assert.Equal(t, 2, groups[2].Count)
	// Files within a group are path-sorted.
	assert.Equal(t, "src/a.go", groups[2].Files[0].Path)
	assert.Equal(t, "src/b.go", groups[2].Files[1].Path)
}

func TestGroupFilesByCategory(t *testing.T) {
	groups, _ := GroupFiles(sampleReport(), GroupSpec{By: GroupCategory})
	keys := map[string]int{}
	for _, g := range groups {
		keys[g.Key] = g.Count
	}
	assert.Equal(t, 2, keys["permissive"])       // MIT x2
	assert.Equal(t, 1, keys["network-copyleft"]) // AGPL
	assert.Equal(t, 1, keys["unknown"])          // headerless
}

func TestGroupFilesByType(t *testing.T) {
	r := model.Report{Files: []model.FileResult{
		headered("a.go", "Go", "MIT"),
		{Path: "weird", FileType: ""}, // non-skipped, no file type
	}}
	groups, _ := GroupFiles(r, GroupSpec{By: GroupType})
	keys := map[string]int{}
	for _, g := range groups {
		keys[g.Key] = g.Count
	}
	assert.Equal(t, 1, keys["Go"])
	assert.Equal(t, 1, keys["(unknown type)"])
}

func TestGroupFilesByDirectory(t *testing.T) {
	groups, _ := GroupFiles(sampleReport(), GroupSpec{By: GroupDirectory})
	keys := map[string]int{}
	for _, g := range groups {
		keys[g.Key] = g.Count
	}
	assert.Equal(t, 3, keys["src"]) // a.go, b.go, headerless.go
	assert.Equal(t, 1, keys["web"])
	// img/logo.png is skipped, so "img" never appears.
	_, hasImg := keys["img"]
	assert.False(t, hasImg)
}

func TestGroupFilesDirectoryRoot(t *testing.T) {
	r := model.Report{Files: []model.FileResult{headered("main.go", "Go", "MIT")}}
	groups, _ := GroupFiles(r, GroupSpec{By: GroupDirectory})
	require.Len(t, groups, 1)
	assert.Equal(t, ".", groups[0].Key)
}

func TestTopDirs(t *testing.T) {
	assert.Equal(t, ".", topDirs("main.go", 1))
	assert.Equal(t, "src", topDirs("src/a.go", 1))
	assert.Equal(t, "src", topDirs("src/sub/deep/a.go", 1))     // capped at depth 1
	assert.Equal(t, "src/sub", topDirs("src/sub/deep/a.go", 2)) // depth 2
	assert.Equal(t, "src", topDirs("src/a.go", 0))              // depth < 1 -> 1
}

// groupByKey returns the group with the given key, failing the test if absent.
func groupByKey(t *testing.T, groups []Group, key string) Group {
	t.Helper()
	for _, g := range groups {
		if g.Key == key {
			return g
		}
	}
	t.Fatalf("no group with key %q in %v", key, groups)
	return Group{}
}

func TestGroupRiskCategoryOnlyWhenNoPolicyConcern(t *testing.T) {
	// sampleReport mixes MIT (permissive) and AGPL (network-copyleft). MIT and AGPL
	// are NOT a curated hard incompatibility, so the MIT group must stay "low" (no
	// false escalation), AGPL stays "high" by its category, and the headerless group
	// is "unknown".
	groups, _ := GroupFiles(sampleReport(), GroupSpec{By: GroupLicense})
	assert.Equal(t, "low", groupByKey(t, groups, "MIT").Risk)
	assert.Equal(t, "high", groupByKey(t, groups, "AGPL-3.0-or-later").Risk)
	assert.Equal(t, "unknown", groupByKey(t, groups, noLicenseKey).Risk)
}

func TestGroupRiskEscalatesOnRepoIncompatibility(t *testing.T) {
	// Apache-2.0 is permissive (category risk "low"), but beside AGPL-3.0-or-later it
	// is party to a curated hard incompatibility. The Apache group must read "high",
	// not "low" -- a group is not "all clear" when its license clashes repo-wide.
	r := model.Report{Files: []model.FileResult{
		headered("a/x.go", "Go", "Apache-2.0"),
		headered("b/y.go", "Go", "AGPL-3.0-or-later"),
	}}
	groups, _ := GroupFiles(r, GroupSpec{By: GroupLicense})
	assert.Equal(t, "high", groupByKey(t, groups, "Apache-2.0").Risk)
	assert.Equal(t, "high", groupByKey(t, groups, "AGPL-3.0-or-later").Risk)
}

func TestGroupRiskEscalatesOnFileScopedPolicyViolation(t *testing.T) {
	// A permissive license carrying a file-scoped policy violation (e.g. deny-listed)
	// must escalate its group to "high" rather than reading its category risk "low".
	denied := headered("a/x.go", "Go", "MIT")
	denied.Violations = []string{model.FailOnPolicyViolation.String()}
	r := model.Report{Files: []model.FileResult{denied}}
	groups, _ := GroupFiles(r, GroupSpec{By: GroupLicense})
	assert.Equal(t, "high", groupByKey(t, groups, "MIT").Risk)
}

func TestLicenseBreakdown(t *testing.T) {
	files := []model.FileResult{
		headered("a.go", "Go", "MIT"),
		headered("b.go", "Go", "MIT"),
		{Path: "c.go", FileType: "Go"}, // no header
	}
	got := licenseBreakdown(files)
	assert.Equal(t, 2, got["MIT"])
	assert.Equal(t, 1, got["(no-header)"])
}
