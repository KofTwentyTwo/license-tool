package enumerate

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/KofTwentyTwo/license-tool/internal/filetype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEnumerateAbsError covers Enumerate's filepath.Abs error guard via the
// filepathAbs seam. filepath.Abs only fails when os.Getwd does, so the guard is
// otherwise unreachable; injecting an error confirms Enumerate propagates it.
func TestEnumerateAbsError(t *testing.T) {
	boom := errors.New("synthetic abs failure")
	orig := filepathAbs
	t.Cleanup(func() { filepathAbs = orig })
	filepathAbs = func(string) (string, error) { return "", boom }

	entries, err := Enumerate(t.TempDir(), Options{}, filetype.Lookup)
	require.ErrorIs(t, err, boom)
	assert.Nil(t, entries)
}

// TestWalkListFilesRelError covers walkListFiles' filepath.Rel error guard via the
// filepathRel seam. WalkDir always hands the callback a path under the root, so Rel
// never fails in practice; injecting an error confirms the callback returns it and
// the walk aborts with that error.
func TestWalkListFilesRelError(t *testing.T) {
	boom := errors.New("synthetic rel failure")
	orig := filepathRel
	t.Cleanup(func() { filepathRel = orig })
	filepathRel = func(string, string) (string, error) { return "", boom }

	root := t.TempDir()
	writeFile(t, root, "a.go", "package x\n")

	// A non-git directory takes the walk path; the first non-root entry triggers Rel.
	paths, err := walkListFiles(root, Options{})
	require.ErrorIs(t, err, boom)
	assert.Nil(t, paths)
}

// TestReadHead exercises readHead's error and short-read branches directly: a
// nonexistent path (Open fails) and an empty file (Read returns n==0, io.EOF),
// plus the normal happy path that returns the leading bytes.
func TestReadHead(t *testing.T) {
	root := t.TempDir()

	t.Run("open error on missing file", func(t *testing.T) {
		content, err := readHead(filepath.Join(root, "does-not-exist"))
		require.Error(t, err)
		assert.Nil(t, content)
	})

	t.Run("empty file yields read error with zero bytes", func(t *testing.T) {
		empty := filepath.Join(root, "empty.txt")
		require.NoError(t, os.WriteFile(empty, nil, 0o644))
		content, err := readHead(empty)
		require.Error(t, err)
		assert.Nil(t, content)
	})

	t.Run("regular file returns leading bytes", func(t *testing.T) {
		full := filepath.Join(root, "full.txt")
		require.NoError(t, os.WriteFile(full, []byte("hello world"), 0o644))
		content, err := readHead(full)
		require.NoError(t, err)
		assert.Equal(t, "hello world", string(content))
	})
}

// TestGitListFilesError verifies gitListFiles propagates a non-zero git exit (a
// directory that is not a git repo) as an error rather than a path list.
func TestGitListFilesError(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir() // deliberately not a git repo
	paths, err := gitListFiles(root)
	require.Error(t, err)
	assert.Nil(t, paths)
}

// TestClassifyEntryLstatError covers the os.Lstat failure branch: git ls-files can
// name a path (a staged deletion) that no longer exists on disk; classifyEntry must
// skip it as unknown type rather than abort. Exercised directly to isolate the branch.
func TestClassifyEntryLstatError(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "ghost.go")

	entry := classifyEntry("ghost.go", missing, filetype.Lookup)
	assert.True(t, entry.Skip)
	assert.Equal(t, reasonUnknownType, entry.SkipReason)
	assert.Equal(t, "ghost.go", entry.Path)
}

func TestClassifyEntryContentLstatError(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "ghost")

	entry := classifyEntryContent("ghost", missing, filetype.LookupContent)
	assert.True(t, entry.Skip)
	assert.Equal(t, reasonUnknownType, entry.SkipReason)
	assert.Equal(t, "ghost", entry.Path)
}

// TestClassifyEntryNonRegular covers the non-regular-file branch: a directory path
// (which the walk path or a submodule gitlink can surface) is skipped as unknown type
// rather than read.
func TestClassifyEntryNonRegular(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "adir")
	require.NoError(t, os.Mkdir(dir, 0o755))

	entry := classifyEntry("adir", dir, filetype.Lookup)
	assert.True(t, entry.Skip)
	assert.Equal(t, reasonUnknownType, entry.SkipReason)
}

func TestClassifyEntryContentNonRegular(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "adir")
	require.NoError(t, os.Mkdir(dir, 0o755))

	entry := classifyEntryContent("adir", dir, filetype.LookupContent)
	assert.True(t, entry.Skip)
	assert.Equal(t, reasonUnknownType, entry.SkipReason)
}

// TestClassifyEntrySymlinkDirect covers the symlink branch in isolation (the git/walk
// integration tests skip on Windows; this keeps the unit-level branch deterministic
// on non-Windows hosts).
func TestClassifyEntrySymlinkDirect(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on windows")
	}
	root := t.TempDir()
	target := filepath.Join(root, "target.go")
	require.NoError(t, os.WriteFile(target, []byte("package x\n"), 0o644))
	link := filepath.Join(root, "link.go")
	require.NoError(t, os.Symlink("target.go", link))

	entry := classifyEntry("link.go", link, filetype.Lookup)
	assert.True(t, entry.Skip)
	assert.Equal(t, reasonSymlink, entry.SkipReason)
}

func TestClassifyEntryContentSymlinkDirect(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on windows")
	}
	root := t.TempDir()
	target := filepath.Join(root, "target")
	require.NoError(t, os.WriteFile(target, []byte("#!/usr/bin/env python3\n"), 0o644))
	link := filepath.Join(root, "link")
	require.NoError(t, os.Symlink("target", link))

	entry := classifyEntryContent("link", link, filetype.LookupContent)
	assert.True(t, entry.Skip)
	assert.Equal(t, reasonSymlink, entry.SkipReason)
}

// TestEnumerateWalkError covers the discovery-error return in Enumerate together with
// the WalkDir callback's err!=nil branch: a nonexistent root is not a git repo, so the
// walk runs and its first callback receives the stat error, which propagates out.
func TestEnumerateWalkError(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "no-such-dir")

	entries, err := Enumerate(missing, Options{}, filetype.Lookup)
	require.Error(t, err)
	assert.Nil(t, entries)
}

// TestWalkListFilesPrunesDotGit covers the .git pruning branch on the non-git walk
// path: a literal .git directory present in an otherwise non-repo tree is skipped
// wholesale, so its contents never appear.
func TestWalkListFilesPrunesDotGit(t *testing.T) {
	root := t.TempDir()

	writeFile(t, root, "keep.go", "package x\n")
	// A .git directory with content, but NOT an initialized repo (no git init), so
	// isGitRepo is false and the manual walk runs and must prune .git itself.
	writeFile(t, root, ".git/config", "[core]\n")
	writeFile(t, root, ".git/objects/aa/bbbb", "blob\n")

	entries, err := Enumerate(root, Options{}, filetype.Lookup)
	require.NoError(t, err)
	byPath := indexEntries(entries)

	assert.Contains(t, byPath, "keep.go")
	for p := range byPath {
		assert.NotContains(t, p, ".git/", "the .git directory must be pruned: %s", p)
	}
}

// TestEnumerateGitStagedDeletion drives the os.Lstat-error branch through the full
// Enumerate pipeline on the git path: a file is committed (so git ls-files lists it)
// and then removed from the working tree, so the on-disk Lstat fails and the entry is
// skipped as unknown type rather than aborting enumeration.
func TestEnumerateGitStagedDeletion(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root)

	writeFile(t, root, "present.go", "package x\n")
	writeFile(t, root, "vanished.go", "package y\n")
	gitAddCommit(t, root)

	// Remove from the working tree without staging the deletion: git ls-files still
	// reports it (HEAD index), but the path no longer exists on disk.
	require.NoError(t, os.Remove(filepath.Join(root, "vanished.go")))

	entries, err := Enumerate(root, Options{}, filetype.Lookup)
	require.NoError(t, err)
	byPath := indexEntries(entries)

	present, ok := byPath["present.go"]
	require.True(t, ok)
	assert.False(t, present.Skip)

	vanished, ok := byPath["vanished.go"]
	require.True(t, ok)
	assert.True(t, vanished.Skip)
	assert.Equal(t, reasonUnknownType, vanished.SkipReason)
}

// TestEnumerateGitDirEntry drives the non-regular-file branch through the full
// Enumerate pipeline on the git path. A submodule-style gitlink is hard to fabricate
// deterministically without network, so this stages a real subdirectory tracked file,
// then replaces the file on disk with a directory of the same name: git ls-files names
// the original blob path, but Lstat now reports a directory at that path.
func TestEnumerateGitPathBecomesDir(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root)

	writeFile(t, root, "swap.go", "package x\n")
	gitAddCommit(t, root)

	// Replace the regular file with a directory at the same path.
	abs := filepath.Join(root, "swap.go")
	require.NoError(t, os.Remove(abs))
	require.NoError(t, os.Mkdir(abs, 0o755))

	entries, err := Enumerate(root, Options{}, filetype.Lookup)
	require.NoError(t, err)
	byPath := indexEntries(entries)

	swap, ok := byPath["swap.go"]
	require.True(t, ok)
	assert.True(t, swap.Skip)
	assert.Equal(t, reasonUnknownType, swap.SkipReason)
}

// TestEnumerateNonGitEmptyFileNotBinary confirms a zero-byte file of a known type is
// not classified binary: readHead returns an error for the empty file, so the binary
// check is skipped and the entry survives. This guards the readHead-error path inside
// classifyEntry's binary step (rerr != nil short-circuits IsBinary).
func TestEnumerateNonGitEmptyFileNotBinary(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "empty.go", "")

	entries, err := Enumerate(root, Options{}, filetype.Lookup)
	require.NoError(t, err)
	byPath := indexEntries(entries)

	e, ok := byPath["empty.go"]
	require.True(t, ok)
	assert.False(t, e.Skip)
	assert.Equal(t, "Go", e.FileType.Name)
}
