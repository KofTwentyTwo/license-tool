package enumerate

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/KofTwentyTwo/license-tool/internal/filetype"
	"github.com/KofTwentyTwo/license-tool/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIsBinary exercises every branch of the null-byte / UTF-8 heuristic, including
// the truncated-trailing-rune tolerance that keeps a split multi-byte sequence from
// being misjudged as binary.
func TestIsBinary(t *testing.T) {
	// A 3-byte UTF-8 rune (euro sign) so we can slice it mid-sequence.
	euro := []byte("€")
	require.Len(t, euro, 3)

	cases := []struct {
		name    string
		content []byte
		want    bool
	}{
		{"empty", []byte{}, false},
		{"plain ascii", []byte("package main\n"), false},
		{"valid multibyte utf8", append([]byte("price "), euro...), false},
		{"null byte", []byte("abc\x00def"), true},
		{"null byte at start", []byte{0x00}, true},
		{"truncated trailing rune is text", append([]byte("price "), euro[:2]...), false},
		{"truncated leading-byte only is text", append([]byte("price "), euro[:1]...), false},
		{"invalid utf8 mid-content is binary", []byte("abc\xff\xfe\xfddef ghi"), true},
		{"lone continuation byte is binary", []byte("\x80\x80\x80\x80\x80\x80"), true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, IsBinary(tc.content))
		})
	}
}

// indexEntries maps an enumeration result to relative-path -> Entry for assertions.
func indexEntries(entries []Entry) map[string]Entry {
	m := make(map[string]Entry, len(entries))
	for _, e := range entries {
		m[e.Path] = e
	}
	return m
}

// writeFile creates parent dirs and writes content under root.
func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	abs := filepath.Join(root, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
	require.NoError(t, os.WriteFile(abs, []byte(content), 0o644))
}

// gitInit initializes a quiet, identity-configured repo so commits/ls-files work in
// CI where no global git identity exists. It skips the test if git is unavailable.
func gitInit(t *testing.T, root string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		{"config", "commit.gpgsign", "false"},
	} {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
}

func gitAddCommit(t *testing.T, root string) {
	t.Helper()
	for _, args := range [][]string{
		{"add", "-A"},
		{"commit", "-q", "-m", "init"},
	} {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
}

// TestEnumerateGitRepo covers the primary path: inside a git repo, the tracked set
// is enumerated, .gitignore is inherited (untracked ignored files excluded), and the
// skip ladder classifies each surviving file.
func TestEnumerateGitRepo(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root)

	writeFile(t, root, "src/Main.java", "package x;\n")
	writeFile(t, root, "build/Generated.java", "package y;\n")
	writeFile(t, root, "config.yaml", "key: value\n")
	writeFile(t, root, "package.json", "{}\n")             // uncommentable
	writeFile(t, root, "README", "docs\n")                 // unknown type
	writeFile(t, root, "blob.go", "package z\x00trailing") // known type, binary content
	writeFile(t, root, ".gitignore", "build/\nignored.go\n")
	writeFile(t, root, "ignored.go", "package ignored\n") // matched by .gitignore, untracked

	gitAddCommit(t, root)

	entries, err := Enumerate(root, Options{}, filetype.Lookup)
	require.NoError(t, err)
	byPath := indexEntries(entries)

	// .gitignore'd files are absent because git ls-files never lists them.
	assert.NotContains(t, byPath, "ignored.go")
	assert.NotContains(t, byPath, "build/Generated.java")

	java, ok := byPath["src/Main.java"]
	require.True(t, ok)
	assert.False(t, java.Skip)
	assert.Equal(t, "Java", java.FileType.Name)
	assert.Equal(t, filepath.Join(root, "src/Main.java"), java.AbsPath)

	yaml, ok := byPath["config.yaml"]
	require.True(t, ok)
	assert.False(t, yaml.Skip)
	assert.Equal(t, "YAML", yaml.FileType.Name)

	cases := []struct {
		path       string
		wantSkip   bool
		wantReason string
		wantType   string
	}{
		{"package.json", true, reasonUncommentable, "JSON"},
		{"README", true, reasonUnknownType, ""},
		{"blob.go", true, reasonBinary, "Go"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			e, ok := byPath[tc.path]
			require.True(t, ok)
			assert.True(t, e.Skip)
			assert.Equal(t, tc.wantReason, e.SkipReason)
			assert.Equal(t, tc.wantType, e.FileType.Name)
		})
	}
}

// TestEnumerateGitRepoSymlink verifies a tracked symlink is surfaced with the
// symlink skip reason rather than followed.
func TestEnumerateGitRepoSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on windows")
	}
	root := t.TempDir()
	gitInit(t, root)

	writeFile(t, root, "real.go", "package x\n")
	require.NoError(t, os.Symlink("real.go", filepath.Join(root, "link.go")))
	gitAddCommit(t, root)

	entries, err := Enumerate(root, Options{}, filetype.Lookup)
	require.NoError(t, err)
	byPath := indexEntries(entries)

	link, ok := byPath["link.go"]
	require.True(t, ok)
	assert.True(t, link.Skip)
	assert.Equal(t, reasonSymlink, link.SkipReason)
}

// TestEnumerateGitRepoConfigExclude verifies config/flag excludes layer on top of
// git's own .gitignore handling.
func TestEnumerateGitRepoConfigExclude(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root)

	writeFile(t, root, "keep.go", "package x\n")
	writeFile(t, root, "gen/a.pb.go", "package gen\n")
	writeFile(t, root, "gen/b.go", "package gen\n")
	gitAddCommit(t, root)

	entries, err := Enumerate(root, Options{Excludes: []string{"**/*.pb.go", "gen/b.go"}}, filetype.Lookup)
	require.NoError(t, err)
	byPath := indexEntries(entries)

	assert.Contains(t, byPath, "keep.go")
	assert.NotContains(t, byPath, "gen/a.pb.go")
	assert.NotContains(t, byPath, "gen/b.go")
}

// TestEnumerateGitRepoIncludes verifies include globs restrict the set.
func TestEnumerateGitRepoIncludes(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root)

	writeFile(t, root, "a.go", "package x\n")
	writeFile(t, root, "b.py", "x = 1\n")
	writeFile(t, root, "sub/c.go", "package y\n")
	gitAddCommit(t, root)

	entries, err := Enumerate(root, Options{Includes: []string{"*.go"}}, filetype.Lookup)
	require.NoError(t, err)
	byPath := indexEntries(entries)

	assert.Contains(t, byPath, "a.go")
	assert.Contains(t, byPath, "sub/c.go")
	assert.NotContains(t, byPath, "b.py")
}

// TestEnumerateGitRepoIncludesDoublestar verifies that ** include globs span
// directories (issue #32): a src/** include must reach nested files, not just the
// top level. The previous filepath.Match include path failed this silently.
func TestEnumerateGitRepoIncludesDoublestar(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root)

	writeFile(t, root, "src/a.go", "package x\n")
	writeFile(t, root, "src/deep/nested/b.go", "package y\n")
	writeFile(t, root, "other/c.go", "package z\n")
	gitAddCommit(t, root)

	entries, err := Enumerate(root, Options{Includes: []string{"src/**"}}, filetype.Lookup)
	require.NoError(t, err)
	byPath := indexEntries(entries)

	assert.Contains(t, byPath, "src/a.go")
	assert.Contains(t, byPath, "src/deep/nested/b.go")
	assert.NotContains(t, byPath, "other/c.go")
}

// TestEnumerateNonGitWalk covers the fallback walk: no git repo, .gitignore honored,
// the .git-style pruning is irrelevant, directories descended, classification applied.
func TestEnumerateNonGitWalk(t *testing.T) {
	root := t.TempDir()
	// Deliberately NOT a git repo.

	writeFile(t, root, "src/Main.java", "package x;\n")
	writeFile(t, root, "config.yaml", "k: v\n")
	writeFile(t, root, "node_modules/dep/index.js", "x\n")
	writeFile(t, root, "build/out.go", "package b\n")
	writeFile(t, root, ".gitignore", "node_modules/\nbuild/\n*.log\n")
	writeFile(t, root, "debug.log", "noise\n")

	entries, err := Enumerate(root, Options{}, filetype.Lookup)
	require.NoError(t, err)
	byPath := indexEntries(entries)

	assert.Contains(t, byPath, "src/Main.java")
	assert.Contains(t, byPath, "config.yaml")
	// Ignored directory pruned wholesale.
	assert.NotContains(t, byPath, "node_modules/dep/index.js")
	assert.NotContains(t, byPath, "build/out.go")
	// Ignored file by glob.
	assert.NotContains(t, byPath, "debug.log")
}

// TestEnumerateNonGitNoGitignore verifies --no-gitignore disables .gitignore on the
// walk path so ignored files reappear.
func TestEnumerateNonGitNoGitignore(t *testing.T) {
	root := t.TempDir()

	writeFile(t, root, "keep.go", "package x\n")
	writeFile(t, root, "build/out.go", "package b\n")
	writeFile(t, root, ".gitignore", "build/\n")

	entries, err := Enumerate(root, Options{NoGitignore: true}, filetype.Lookup)
	require.NoError(t, err)
	byPath := indexEntries(entries)

	assert.Contains(t, byPath, "keep.go")
	assert.Contains(t, byPath, "build/out.go")
}

// TestEnumerateNonGitWalkBinaryAndSymlink verifies the walk path applies the same
// skip ladder for binary content and symlinks as the git path.
func TestEnumerateNonGitWalkBinaryAndSymlink(t *testing.T) {
	root := t.TempDir()

	writeFile(t, root, "good.go", "package x\n")
	writeFile(t, root, "bin.go", "package x\x00\x01\x02")
	writeFile(t, root, "data.json", "{}\n")

	if runtime.GOOS != "windows" {
		require.NoError(t, os.Symlink("good.go", filepath.Join(root, "link.go")))
	}

	entries, err := Enumerate(root, Options{}, filetype.Lookup)
	require.NoError(t, err)
	byPath := indexEntries(entries)

	good := byPath["good.go"]
	assert.False(t, good.Skip)

	bin := byPath["bin.go"]
	assert.True(t, bin.Skip)
	assert.Equal(t, reasonBinary, bin.SkipReason)

	js := byPath["data.json"]
	assert.True(t, js.Skip)
	assert.Equal(t, reasonUncommentable, js.SkipReason)

	if runtime.GOOS != "windows" {
		link := byPath["link.go"]
		assert.True(t, link.Skip)
		assert.Equal(t, reasonSymlink, link.SkipReason)
	}
}

// TestEnumerateNonGitConfigOverrides verifies a filetype.Merge closure is honored:
// a custom extension that the built-in table does not know becomes a real type.
func TestEnumerateNonGitConfigOverrides(t *testing.T) {
	root := t.TempDir()

	writeFile(t, root, "thing.myext", "data\n")
	writeFile(t, root, "other.unknownext", "data\n")

	classify := filetype.Merge(map[string]model.FileType{
		".myext": {
			Name:         "MyExt",
			Extensions:   []string{".myext"},
			CommentStyle: model.CommentStyle{Block: false, LinePrefix: "// "},
		},
	})

	entries, err := Enumerate(root, Options{}, classify)
	require.NoError(t, err)
	byPath := indexEntries(entries)

	my := byPath["thing.myext"]
	assert.False(t, my.Skip)
	assert.Equal(t, "MyExt", my.FileType.Name)

	other := byPath["other.unknownext"]
	assert.True(t, other.Skip)
	assert.Equal(t, reasonUnknownType, other.SkipReason)
}

func TestWithContentDetectsExtensionlessShebang(t *testing.T) {
	root := t.TempDir()

	writeFile(t, root, "tool", "#!/usr/bin/env python3\nprint('ok')\n")

	entries, err := WithContent(root, Options{}, filetype.LookupContent)
	require.NoError(t, err)
	byPath := indexEntries(entries)

	tool := byPath["tool"]
	assert.False(t, tool.Skip)
	assert.Equal(t, "Python", tool.FileType.Name)
}

func TestWithContentSkipAndOverrideBehavior(t *testing.T) {
	root := t.TempDir()

	writeFile(t, root, "unknown-script", "#!/usr/bin/env mystery\nrun\n")
	writeFile(t, root, "plain", "plain text\n")
	writeFile(t, root, "package.json", "{}\n")
	writeFile(t, root, "binary-tool", "#!/usr/bin/env python3\n\x00")
	writeFile(t, root, "thing.myext", "#!/usr/bin/env python3\nprint('path wins')\n")

	classify := filetype.MergeContent(map[string]model.FileType{
		".myext": {
			Name:         "MyExt",
			Extensions:   []string{".myext"},
			CommentStyle: model.CommentStyle{Block: false, LinePrefix: "// "},
		},
	})

	entries, err := WithContent(root, Options{}, classify)
	require.NoError(t, err)
	byPath := indexEntries(entries)

	cases := []struct {
		path       string
		wantReason string
		wantType   string
	}{
		{"unknown-script", reasonUnknownType, ""},
		{"plain", reasonUnknownType, ""},
		{"package.json", reasonUncommentable, "JSON"},
		{"binary-tool", reasonBinary, "Python"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			e := byPath[tc.path]
			assert.True(t, e.Skip)
			assert.Equal(t, tc.wantReason, e.SkipReason)
			assert.Equal(t, tc.wantType, e.FileType.Name)
		})
	}

	custom := byPath["thing.myext"]
	assert.False(t, custom.Skip)
	assert.Equal(t, "MyExt", custom.FileType.Name)
}

// TestEnumerateRelativeRoot verifies a relative root is resolved to an absolute path
// and entries carry correct relative + absolute paths.
func TestEnumerateRelativeRoot(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.go", "package x\n")

	wd, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chdir(wd) })
	require.NoError(t, os.Chdir(root))

	entries, err := Enumerate(".", Options{}, filetype.Lookup)
	require.NoError(t, err)
	byPath := indexEntries(entries)

	a, ok := byPath["a.go"]
	require.True(t, ok)
	assert.True(t, filepath.IsAbs(a.AbsPath))
	assert.True(t, strings.HasSuffix(filepath.ToSlash(a.AbsPath), "a.go"))
}

// TestEnumerateDeterministicOrder confirms the git path preserves git ls-files'
// sorted order, which keeps reports and diffs stable across runs.
func TestEnumerateDeterministicOrder(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root)

	writeFile(t, root, "z.go", "package x\n")
	writeFile(t, root, "a.go", "package x\n")
	writeFile(t, root, "m.go", "package x\n")
	gitAddCommit(t, root)

	entries, err := Enumerate(root, Options{}, filetype.Lookup)
	require.NoError(t, err)

	var paths []string
	for _, e := range entries {
		paths = append(paths, e.Path)
	}
	require.True(t, sort.StringsAreSorted(paths), "git ls-files output should be sorted: %v", paths)
}
