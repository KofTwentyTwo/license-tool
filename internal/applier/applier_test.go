package applier

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/KofTwentyTwo/license-tool/internal/model"
	"github.com/KofTwentyTwo/license-tool/internal/render"
	"github.com/KofTwentyTwo/license-tool/internal/spdx"
)

// --- shared test helpers --------------------------------------------------

// goFileType returns the built-in Go file type (block comments, package-decl
// preserved before the header) so tests do not duplicate the comment-style table.
func goFileType() model.FileType {
	return model.FileType{
		Name:         "Go",
		Extensions:   []string{".go"},
		CommentStyle: model.CommentStyle{Block: true, Open: "/*", Close: "*/"},
		PreserveFirst: []model.PreserveRule{
			{Kind: model.PreserveBOM, Before: false},
			{Kind: model.PreserveGoBuildConstraint, Before: false},
			{Kind: model.PreservePackageDecl, Before: true},
		},
	}
}

func cssFileType() model.FileType {
	return model.FileType{
		Name:         "CSS",
		Extensions:   []string{".css"},
		CommentStyle: model.CommentStyle{Block: true, Open: "/*", Close: "*/"},
		PreserveFirst: []model.PreserveRule{
			{Kind: model.PreserveBOM, Before: false},
			{Kind: model.PreserveCSSCharset, Before: false},
		},
	}
}

func markupFileType() model.FileType {
	return model.FileType{
		Name:         "XML/HTML",
		Extensions:   []string{".html"},
		CommentStyle: model.CommentStyle{Block: true, Open: "<!--", Close: "-->"},
		PreserveFirst: []model.PreserveRule{
			{Kind: model.PreserveBOM, Before: false},
			{Kind: model.PreserveXMLDecl, Before: false},
			{Kind: model.PreserveDoctype, Before: false},
		},
	}
}

// phpFileType returns a block-comment PHP file type whose preserve rules mirror the
// builtin table: BOM, then a (universal) shebang, then the <?php open tag, all before
// the header. It is defined locally for the same reason goFileType is -- to avoid
// depending on the filetype package's table from the applier tests.
func phpFileType() model.FileType {
	return model.FileType{
		Name:         "PHP",
		Extensions:   []string{".php"},
		CommentStyle: model.CommentStyle{Block: true, Open: "/*", Close: "*/"},
		PreserveFirst: []model.PreserveRule{
			{Kind: model.PreserveBOM, Before: false},
			{Kind: model.PreserveShebang, Before: false},
			{Kind: model.PreservePHPOpen, Before: false},
		},
	}
}

// agplConfig is the canonical apply config: AGPL, reuse+notice, an explicit year so
// the rendered header is deterministic and independent of the wall clock.
func agplConfig() model.Config {
	return model.Config{
		License: "AGPL-3.0-or-later",
		Holder:  "Acme Corp",
		Year:    model.YearSpec{Kind: model.YearExplicit, Start: 2024},
		Style:   model.StyleReusePlusNotice,
	}
}

// writeFile creates parent dirs and writes content under root.
func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	abs := filepath.Join(root, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
	require.NoError(t, os.WriteFile(abs, []byte(content), 0o644))
}

// gitInit initializes a quiet, identity-configured repo so commits / ls-files /
// status work in CI where no global git identity exists. It skips the test if git
// is unavailable. The committer identity is pinned for determinism.
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

// gitAddCommit stages everything and commits, leaving a clean working tree.
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

// gitHeadSubject returns the subject line of HEAD so the commit-path test can assert
// the conventional message that apply --commit produced.
func gitHeadSubject(t *testing.T, root string) string {
	t.Helper()
	cmd := exec.Command("git", "-C", root, "log", "-1", "--format=%s")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git log: %s", out)
	return strings.TrimSpace(string(out))
}

// corruptGitIndex overwrites .git/index with garbage so `git status` and
// `git ls-files` fail (exit 128) while `git rev-parse --is-inside-work-tree` still
// reports a work tree. This lets tests drive the git-command error paths
// (gitutil.IsClean / enumerate's ls-files) without a production change.
func corruptGitIndex(t *testing.T, root string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(root, ".git", "index"), []byte("not-a-valid-git-index"), 0o644))
}

// findResult returns the FileResult for rel from a report, failing the test if absent.
func findResult(t *testing.T, files []model.FileResult, rel string) model.FileResult {
	t.Helper()
	for _, fr := range files {
		if fr.Path == rel {
			return fr
		}
	}
	t.Fatalf("no result for %q in %v", rel, files)
	return model.FileResult{}
}

// --- gateWrite ------------------------------------------------------------

// TestGateWrite covers every branch of the write-safety gate: clean repo, dirty
// repo (refused), dirty repo with --allow-dirty (permitted), non-git refused, and
// non-git with --force (permitted).
func TestGateWrite(t *testing.T) {
	t.Run("clean repo permits", func(t *testing.T) {
		root := t.TempDir()
		gitInit(t, root)
		writeFile(t, root, "a.go", "package x\n")
		gitAddCommit(t, root)

		require.NoError(t, gateWrite(root, Options{Write: true}))
	})

	t.Run("dirty repo is refused", func(t *testing.T) {
		root := t.TempDir()
		gitInit(t, root)
		writeFile(t, root, "a.go", "package x\n")
		gitAddCommit(t, root)
		// Introduce an uncommitted change to make the tree dirty.
		writeFile(t, root, "a.go", "package x\nvar y = 1\n")

		err := gateWrite(root, Options{Write: true})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "dirty git tree")
	})

	t.Run("dirty repo with allow-dirty permits", func(t *testing.T) {
		root := t.TempDir()
		gitInit(t, root)
		writeFile(t, root, "a.go", "package x\n")
		gitAddCommit(t, root)
		writeFile(t, root, "a.go", "package x\nvar y = 1\n")

		require.NoError(t, gateWrite(root, Options{Write: true, AllowDirty: true}))
	})

	t.Run("non-git is refused without force", func(t *testing.T) {
		root := t.TempDir() // deliberately not a git repo
		err := gateWrite(root, Options{Write: true})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "non-git directory")
	})

	t.Run("non-git with force permits", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, gateWrite(root, Options{Write: true, Force: true}))
	})

	t.Run("is-clean check error is surfaced", func(t *testing.T) {
		root := t.TempDir()
		gitInit(t, root)
		writeFile(t, root, "a.go", "package x\n")
		gitAddCommit(t, root)
		// Corrupt the index so the IsRepo probe still succeeds but `git status`
		// (IsClean) fails, exercising the gate's error-return branch.
		corruptGitIndex(t, root)

		err := gateWrite(root, Options{Write: true})
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "dirty git tree", "this is a git failure, not a dirty-tree refusal")
	})
}

// --- unifiedDiff ----------------------------------------------------------

// TestUnifiedDiff covers both branches: identical inputs yield an empty diff, and a
// genuine change yields a unified diff labeled with the file name and the changed
// lines.
func TestUnifiedDiff(t *testing.T) {
	t.Run("identical yields empty", func(t *testing.T) {
		assert.Equal(t, "", unifiedDiff("x.go", []byte("same\n"), []byte("same\n")))
	})

	t.Run("difference yields labeled diff", func(t *testing.T) {
		diff := unifiedDiff("x.go", []byte("old line\n"), []byte("new line\n"))
		require.NotEmpty(t, diff)
		assert.Contains(t, diff, "x.go")
		assert.Contains(t, diff, "-old line")
		assert.Contains(t, diff, "+new line")
	})
}

// --- AtomicWrite ----------------------------------------------------------

// TestAtomicWriteNewFile writes a brand-new file (no existing mode to preserve) and
// confirms the content lands and the default mode is applied.
func TestAtomicWriteNewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new.txt")

	require.NoError(t, AtomicWrite(path, []byte("hello\n")))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "hello\n", string(got))
}

// TestAtomicWritePreservesMode confirms an existing file's permission bits survive
// the temp-file-then-rename, which is the documented mode-preservation invariant.
func TestAtomicWritePreservesMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix file modes do not apply on windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "script.sh")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\necho old\n"), 0o755))

	require.NoError(t, AtomicWrite(path, []byte("#!/bin/sh\necho new\n")))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "#!/bin/sh\necho new\n", string(got))

	fi, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o755), fi.Mode().Perm(), "executable bit must survive the write")
}

// TestAtomicWriteCreateTempError forces os.CreateTemp to fail by pointing at a
// directory that does not exist, exercising the early error return.
func TestAtomicWriteCreateTempError(t *testing.T) {
	dir := t.TempDir()
	// The parent directory of the target does not exist, so CreateTemp(dir, ...) fails.
	path := filepath.Join(dir, "missing-subdir", "file.txt")

	err := AtomicWrite(path, []byte("data"))
	require.Error(t, err)
}

// TestAtomicWriteWriteError covers the temp-file write-failure branch via the
// tmpWrite seam. A short write or disk-full error is environment-dependent, so the
// seam injects the failure deterministically and confirms AtomicWrite returns it.
func TestAtomicWriteWriteError(t *testing.T) {
	boom := errors.New("synthetic write failure")
	origWrite := tmpWrite
	t.Cleanup(func() { tmpWrite = origWrite })
	tmpWrite = func(*os.File, []byte) (int, error) { return 0, boom }

	err := AtomicWrite(filepath.Join(t.TempDir(), "f.txt"), []byte("data"))
	require.ErrorIs(t, err, boom)
}

// TestAtomicWriteCloseError covers the temp-file close-failure branch via the
// tmpClose seam (write succeeds, close fails).
func TestAtomicWriteCloseError(t *testing.T) {
	boom := errors.New("synthetic close failure")
	origClose := tmpClose
	t.Cleanup(func() { tmpClose = origClose })
	tmpClose = func(*os.File) error { return boom }

	err := AtomicWrite(filepath.Join(t.TempDir(), "f.txt"), []byte("data"))
	require.ErrorIs(t, err, boom)
}

// TestAtomicWriteChmodError covers the chmod-failure branch via the chmodFn seam
// (write and close succeed, chmod fails). The injected close must still close the
// real file so the temp is flushed; only chmod is faulted.
func TestAtomicWriteChmodError(t *testing.T) {
	boom := errors.New("synthetic chmod failure")
	origChmod := chmodFn
	t.Cleanup(func() { chmodFn = origChmod })
	chmodFn = func(string, os.FileMode) error { return boom }

	err := AtomicWrite(filepath.Join(t.TempDir(), "f.txt"), []byte("data"))
	require.ErrorIs(t, err, boom)
}

// --- ApplyFile ------------------------------------------------------------

// agplHeaderFunc returns a HeaderRenderFunc that renders the canonical AGPL header
// for the given file type, used by the in-memory ApplyFile tests.
func agplHeaderFunc(t *testing.T) HeaderRenderFunc {
	t.Helper()
	license, ok := spdx.Lookup("AGPL-3.0-or-later")
	require.True(t, ok)
	return func(ft model.FileType) (string, error) {
		return render.Header(render.HeaderInput{
			License:  license,
			Holder:   "Acme Corp",
			Year:     "2024",
			Style:    model.StyleReusePlusNotice,
			FileType: ft,
		})
	}
}

// TestApplyFile covers the in-memory single-file paths: a skip type short-circuits,
// a render-func error propagates, an insert into a headerless file, and an
// idempotent re-apply that produces no change.
func TestApplyFile(t *testing.T) {
	t.Run("skip type short-circuits", func(t *testing.T) {
		ft := model.FileType{Name: "JSON", Skip: true}
		content := []byte("{}\n")
		newContent, diff, action, err := ApplyFile(content, ft, agplHeaderFunc(t))
		require.NoError(t, err)
		assert.Equal(t, "skip", action)
		assert.Empty(t, diff)
		assert.Equal(t, content, newContent)
	})

	t.Run("render error propagates", func(t *testing.T) {
		boom := errors.New("render boom")
		_, diff, action, err := ApplyFile([]byte("package x\n"), goFileType(), func(model.FileType) (string, error) {
			return "", boom
		})
		require.ErrorIs(t, err, boom)
		assert.Equal(t, "none", action)
		assert.Empty(t, diff)
	})

	t.Run("detect error propagates", func(t *testing.T) {
		// detect.Detect only errors when its (infallible) preserve-boundary step fails,
		// so the detectFn seam drives ApplyFile's detect-error guard deterministically.
		boom := errors.New("detect boom")
		orig := detectFn
		t.Cleanup(func() { detectFn = orig })
		detectFn = func([]byte, model.FileType) (model.DetectedHeader, error) {
			return model.DetectedHeader{}, boom
		}

		_, diff, action, err := ApplyFile([]byte("package x\n"), goFileType(), agplHeaderFunc(t))
		require.ErrorIs(t, err, boom)
		assert.Equal(t, "none", action)
		assert.Empty(t, diff)
	})

	t.Run("insert into headerless file", func(t *testing.T) {
		content := []byte("package x\n")
		newContent, diff, action, err := ApplyFile(content, goFileType(), agplHeaderFunc(t))
		require.NoError(t, err)
		assert.Equal(t, "insert", action)
		assert.NotEmpty(t, diff)
		assert.Contains(t, string(newContent), "SPDX-License-Identifier: AGPL-3.0-or-later")
		assert.Contains(t, string(newContent), "license-tool:managed")
		// The package declaration survives below the inserted header.
		assert.Contains(t, string(newContent), "package x")
	})

	t.Run("re-apply is idempotent", func(t *testing.T) {
		content := []byte("package x\n")
		once, _, action1, err := ApplyFile(content, goFileType(), agplHeaderFunc(t))
		require.NoError(t, err)
		require.Equal(t, "insert", action1)

		twice, diff2, action2, err := ApplyFile(once, goFileType(), agplHeaderFunc(t))
		require.NoError(t, err)
		assert.Equal(t, "none", action2)
		assert.Empty(t, diff2)
		assert.Equal(t, once, twice)
	})
}

func TestApplyFileIdempotentAfterPreserveFirstConstructs(t *testing.T) {
	cases := []struct {
		name    string
		ft      model.FileType
		content []byte
		prefix  string
	}{
		{
			name:    "go build constraint",
			ft:      goFileType(),
			content: []byte("//go:build linux\n\npackage x\n"),
			prefix:  "//go:build linux\n\n",
		},
		{
			name:    "css charset",
			ft:      cssFileType(),
			content: []byte("@charset \"UTF-8\";\nbody {}\n"),
			prefix:  "@charset \"UTF-8\";\n",
		},
		{
			name:    "doctype",
			ft:      markupFileType(),
			content: []byte("<!DOCTYPE html>\n<html></html>\n"),
			prefix:  "<!DOCTYPE html>\n",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			once, _, action1, err := ApplyFile(c.content, c.ft, agplHeaderFunc(t))
			require.NoError(t, err)
			require.Equal(t, "insert", action1)
			assert.True(t, strings.HasPrefix(string(once), c.prefix))

			twice, diff2, action2, err := ApplyFile(once, c.ft, agplHeaderFunc(t))
			require.NoError(t, err)
			assert.Equal(t, "none", action2)
			assert.Empty(t, diff2)
			assert.Equal(t, once, twice)
		})
	}
}

// TestApplyFileShebangPHPIdempotent is the applier-level regression guard for the
// universal-shebang fix on a BLOCK-comment type. A PHP CLI script begins with a "#!"
// line then "<?php"; the header must land after both, and a second apply must be a
// byte-identical no-op. Before the fix the header was spliced above the shebang, so the
// second pass re-detected differently and the result was not idempotent.
func TestApplyFileShebangPHPIdempotent(t *testing.T) {
	content := []byte("#!/usr/bin/env php\n<?php\necho 'hi';\n")

	once, _, action1, err := ApplyFile(content, phpFileType(), agplHeaderFunc(t))
	require.NoError(t, err)
	require.Equal(t, "insert", action1)

	got := string(once)
	assert.True(t, strings.HasPrefix(got, "#!/usr/bin/env php\n<?php\n"),
		"the shebang stays line 1 and <?php line 2, with the header after both; got %q", got)
	idxShebang := strings.Index(got, "#!")
	idxHeader := strings.Index(got, "SPDX-License-Identifier: AGPL-3.0-or-later")
	require.GreaterOrEqual(t, idxHeader, 0)
	assert.Equal(t, 0, idxShebang, "the shebang must start at byte 0")
	assert.Less(t, idxShebang, idxHeader, "the header is inserted after the shebang")

	twice, diff2, action2, err := ApplyFile(once, phpFileType(), agplHeaderFunc(t))
	require.NoError(t, err)
	assert.Equal(t, "none", action2, "a second apply detects the managed header and changes nothing")
	assert.Empty(t, diff2)
	assert.Equal(t, once, twice, "apply twice must be byte-identical")
}

// --- ManageLicenseFiles ---------------------------------------------------

// TestManageLicenseFilesUnknownLicense verifies an unknown SPDX id is rejected
// before any file work.
func TestManageLicenseFilesUnknownLicense(t *testing.T) {
	root := t.TempDir()
	cfg := agplConfig()
	cfg.License = "NOT-A-REAL-LICENSE"
	_, err := ManageLicenseFiles(root, cfg, Options{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown license")
}

// TestManageLicenseFilesLicenseFileError covers the render.LicenseFile-failure
// branch via the licenseFileFn seam. Every curated license spdx.Lookup returns has
// non-empty text, so LicenseFile never fails for a looked-up license; the seam drives
// the guard deterministically.
func TestManageLicenseFilesLicenseFileError(t *testing.T) {
	boom := errors.New("synthetic license-file render failure")
	orig := licenseFileFn
	t.Cleanup(func() { licenseFileFn = orig })
	licenseFileFn = func(model.License) (string, error) { return "", boom }

	_, err := ManageLicenseFiles(t.TempDir(), agplConfig(), Options{})
	require.ErrorIs(t, err, boom)
}

// TestManageLicenseFilesEntryError covers the render.LicensesEntry-failure branch via
// the licensesEntryFn seam (LicenseFile succeeds, the per-id entry render fails).
func TestManageLicenseFilesEntryError(t *testing.T) {
	boom := errors.New("synthetic licenses-entry render failure")
	orig := licensesEntryFn
	t.Cleanup(func() { licensesEntryFn = orig })
	licensesEntryFn = func(model.License) (string, error) { return "", boom }

	_, err := ManageLicenseFiles(t.TempDir(), agplConfig(), Options{})
	require.ErrorIs(t, err, boom)
}

// TestManageLicenseFilesDryRun computes the LICENSE and LICENSES/<id>.txt results
// without writing (insert action, populated diffs, nothing on disk).
func TestManageLicenseFilesDryRun(t *testing.T) {
	root := t.TempDir()
	cfg := agplConfig()

	results, err := ManageLicenseFiles(root, cfg, Options{Write: false})
	require.NoError(t, err)
	require.Len(t, results, 2)

	license := findResult(t, results, "LICENSE")
	assert.Equal(t, "insert", license.Action)
	assert.Equal(t, "license", license.FileType)
	assert.NotEmpty(t, license.Diff)

	entryRel := filepath.Join("LICENSES", cfg.License+".txt")
	entry := findResult(t, results, entryRel)
	assert.Equal(t, "insert", entry.Action)
	assert.NotEmpty(t, entry.Diff)

	// Dry-run never touches disk.
	_, statErr := os.Stat(filepath.Join(root, "LICENSE"))
	assert.True(t, os.IsNotExist(statErr))
}

// TestManageLicenseFilesWrite writes both managed files, then verifies a second
// write reports "none" (idempotent) and a content change reports "replace".
func TestManageLicenseFilesWrite(t *testing.T) {
	root := t.TempDir()
	cfg := agplConfig()
	entryRel := filepath.Join("LICENSES", cfg.License+".txt")

	results, err := ManageLicenseFiles(root, cfg, Options{Write: true})
	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.Empty(t, findResult(t, results, "LICENSE").Err)

	body, err := os.ReadFile(filepath.Join(root, "LICENSE"))
	require.NoError(t, err)
	assert.Contains(t, string(body), "GNU AFFERO GENERAL PUBLIC LICENSE")
	_, err = os.Stat(filepath.Join(root, entryRel))
	require.NoError(t, err)

	// Second write: files already match, so both actions are "none".
	again, err := ManageLicenseFiles(root, cfg, Options{Write: true})
	require.NoError(t, err)
	assert.Equal(t, "none", findResult(t, again, "LICENSE").Action)
	assert.Equal(t, "none", findResult(t, again, entryRel).Action)

	// Corrupt LICENSE so the next pass must replace it.
	require.NoError(t, os.WriteFile(filepath.Join(root, "LICENSE"), []byte("stale\n"), 0o644))
	replaced, err := ManageLicenseFiles(root, cfg, Options{Write: true})
	require.NoError(t, err)
	assert.Equal(t, "replace", findResult(t, replaced, "LICENSE").Action)
}

// --- writeManaged error paths ---------------------------------------------

// TestWriteManagedMkdirError forces the MkdirAll guard to fail by placing a regular
// file where the LICENSES directory needs to be created.
func TestWriteManagedMkdirError(t *testing.T) {
	root := t.TempDir()
	// "LICENSES" exists as a FILE, so MkdirAll(LICENSES) fails for the entry write.
	require.NoError(t, os.WriteFile(filepath.Join(root, "LICENSES"), []byte("x"), 0o644))

	fr := writeManaged(root, filepath.Join("LICENSES", "AGPL-3.0-or-later.txt"), []byte("body"), Options{Write: true})
	assert.NotEmpty(t, fr.Err)
}

// TestWriteManagedWriteError forces AtomicWrite to fail (the parent dir cannot be
// created because a path component is a file) while still exercising the
// write-error recording branch independent of the MkdirAll guard.
func TestWriteManagedWriteError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permission semantics differ on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory write permissions")
	}
	root := t.TempDir()
	// A read-only directory: MkdirAll(dir) succeeds (dir already exists), but
	// CreateTemp inside it fails, so AtomicWrite errors and writeManaged records it.
	roDir := filepath.Join(root, "ro")
	require.NoError(t, os.Mkdir(roDir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(roDir, 0o755) })

	fr := writeManaged(roDir, "LICENSE", []byte("body"), Options{Write: true})
	assert.NotEmpty(t, fr.Err)
}

// --- Apply: validation and dry-run ----------------------------------------

// TestApplyUnknownLicense rejects an unknown SPDX id up front.
func TestApplyUnknownLicense(t *testing.T) {
	root := t.TempDir()
	cfg := agplConfig()
	cfg.License = "NOPE-1.0"
	_, err := Apply(root, cfg, Options{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown license")
}

// TestApplyGateWriteFailure verifies a write run aborts when the gate refuses (a
// non-git directory without --force).
func TestApplyGateWriteFailure(t *testing.T) {
	root := t.TempDir() // not a git repo
	writeFile(t, root, "a.go", "package x\n")

	_, err := Apply(root, agplConfig(), Options{Write: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-git directory")
}

// TestApplyYearResolverError drives the year-resolution failure path: the git year
// policy on a directory with no commits cannot resolve a first-commit year.
func TestApplyYearResolverError(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	gitInit(t, root) // repo with zero commits
	cfg := agplConfig()
	cfg.Year = model.YearSpec{Kind: model.YearGit}

	// Dry-run (no gate); the year resolver still runs and fails on the empty repo.
	_, err := Apply(root, cfg, Options{})
	require.Error(t, err)
}

// TestApplyEnumerateError drives the enumeration-failure path: a corrupt git index
// makes `git ls-files` fail, so Enumerate returns an error that Apply propagates.
// A dry-run (no gate) keeps the failure isolated to the enumerate step.
func TestApplyEnumerateError(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	gitInit(t, root)
	writeFile(t, root, "a.go", "package x\n")
	gitAddCommit(t, root)
	corruptGitIndex(t, root)

	_, err := Apply(root, agplConfig(), Options{Write: false})
	require.Error(t, err)
}

// TestApplyDryRun runs a non-write pass over a git repo and asserts diffs are
// produced without mutating any file on disk.
func TestApplyDryRun(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root)
	writeFile(t, root, "main.go", "package main\n")
	writeFile(t, root, "tool", "#!/usr/bin/env python3\nprint('ok')\n")
	writeFile(t, root, "data.json", "{}\n") // uncommentable: skipped
	gitAddCommit(t, root)

	before, err := os.ReadFile(filepath.Join(root, "main.go"))
	require.NoError(t, err)

	rep, err := Apply(root, agplConfig(), Options{Write: false})
	require.NoError(t, err)

	main := findResult(t, rep.Files, "main.go")
	assert.Equal(t, "insert", main.Action)
	assert.NotEmpty(t, main.Diff)
	assert.Empty(t, main.Err)

	tool := findResult(t, rep.Files, "tool")
	assert.Equal(t, "Python", tool.FileType)
	assert.Equal(t, "insert", tool.Action)
	assert.NotEmpty(t, tool.Diff)

	json := findResult(t, rep.Files, "data.json")
	assert.True(t, json.Skipped)
	assert.Equal(t, "none", json.Action)

	// Dry-run left the file untouched.
	after, err := os.ReadFile(filepath.Join(root, "main.go"))
	require.NoError(t, err)
	assert.Equal(t, before, after)
}

// --- Apply: full write + license files + commit ---------------------------

// TestApplyWriteWithLicenseAndCommit is the end-to-end happy path: a clean repo is
// written, the managed LICENSE files are produced, and a single conventional commit
// is made. It also confirms the header landed on disk and the tree is clean after.
func TestApplyWriteWithLicenseAndCommit(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root)
	writeFile(t, root, "main.go", "package main\n\nfunc main() {}\n")
	gitAddCommit(t, root)

	rep, err := Apply(root, agplConfig(), Options{
		Write:             true,
		Commit:            true,
		ManageLicenseFile: true,
	})
	require.NoError(t, err)

	main := findResult(t, rep.Files, "main.go")
	assert.Equal(t, "insert", main.Action)
	assert.Empty(t, main.Err)

	// Header was written to disk.
	onDisk, err := os.ReadFile(filepath.Join(root, "main.go"))
	require.NoError(t, err)
	assert.Contains(t, string(onDisk), "SPDX-License-Identifier: AGPL-3.0-or-later")
	assert.Contains(t, string(onDisk), "Copyright (c) 2024 Acme Corp")

	// Managed license files were written and reported.
	require.FileExists(t, filepath.Join(root, "LICENSE"))
	require.FileExists(t, filepath.Join(root, "LICENSES", "AGPL-3.0-or-later.txt"))
	assert.Equal(t, "insert", findResult(t, rep.Files, "LICENSE").Action)

	// The default conventional commit message was used and the tree is now clean.
	assert.Equal(t, "chore: standardize license headers to AGPL-3.0-or-later", gitHeadSubject(t, root))
	cmd := exec.Command("git", "-C", root, "status", "--porcelain")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err)
	assert.Empty(t, strings.TrimSpace(string(out)), "commit should leave a clean tree")
}

// TestApplyWriteCustomCommitMessage verifies a non-empty CommitMessage overrides the
// default conventional message.
func TestApplyWriteCustomCommitMessage(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root)
	writeFile(t, root, "main.go", "package main\n")
	gitAddCommit(t, root)

	_, err := Apply(root, agplConfig(), Options{
		Write:         true,
		Commit:        true,
		CommitMessage: "licensing: apply AGPL headers",
	})
	require.NoError(t, err)
	assert.Equal(t, "licensing: apply AGPL headers", gitHeadSubject(t, root))
}

// TestApplyCommitErrorWithNothingToCommit drives the commit-error path: with
// --allow-dirty on an already-headered, committed file there is nothing new to
// stage, so git commit fails and Apply surfaces the error.
func TestApplyCommitErrorWithNothingToCommit(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root)
	writeFile(t, root, "main.go", "package main\n")
	gitAddCommit(t, root)

	// First write+commit applies the header.
	_, err := Apply(root, agplConfig(), Options{Write: true, Commit: true})
	require.NoError(t, err)

	// Second run is idempotent (action "none", nothing written), but --commit still
	// tries to commit with an empty index, which git rejects.
	_, err = Apply(root, agplConfig(), Options{Write: true, Commit: true, AllowDirty: true})
	require.Error(t, err)
}

// TestApplyWriteAtomicWriteError records a per-file AtomicWrite failure on the
// FileResult without aborting the run: the target source file is made unwritable by
// making its parent directory read-only so the temp-create inside it fails.
func TestApplyWriteAtomicWriteError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permission semantics differ on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory write permissions")
	}
	root := t.TempDir()
	gitInit(t, root)
	writeFile(t, root, "sub/main.go", "package main\n")
	gitAddCommit(t, root)

	// Make the directory containing the file read-only so AtomicWrite's CreateTemp
	// inside it fails. The gate passes (clean tree) and enumeration/render succeed;
	// only the on-disk write fails, which must be recorded, not fatal.
	subDir := filepath.Join(root, "sub")
	require.NoError(t, os.Chmod(subDir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(subDir, 0o755) })

	rep, err := Apply(root, agplConfig(), Options{Write: true})
	require.NoError(t, err)

	main := findResult(t, rep.Files, "sub/main.go")
	assert.Equal(t, "insert", main.Action)
	assert.NotEmpty(t, main.Err, "the write failure must be recorded on the file result")
}

// TestApplyPerFileReadError records a per-file read failure without aborting the
// run: a tracked file is made unreadable (mode 0000) so os.ReadFile fails.
func TestApplyPerFileReadError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("0000 file mode is not honored on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file read permissions")
	}
	root := t.TempDir()
	gitInit(t, root)
	writeFile(t, root, "readable.go", "package x\n")
	writeFile(t, root, "secret.go", "package x\n")
	gitAddCommit(t, root)

	secret := filepath.Join(root, "secret.go")
	require.NoError(t, os.Chmod(secret, 0o000))
	t.Cleanup(func() { _ = os.Chmod(secret, 0o644) })

	// Dry-run so the gate's clean-tree check is skipped (chmod does not dirty the
	// tree for git, but skipping the gate keeps the test focused on the read path).
	rep, err := Apply(root, agplConfig(), Options{Write: false})
	require.NoError(t, err)

	bad := findResult(t, rep.Files, "secret.go")
	assert.NotEmpty(t, bad.Err, "unreadable file must record a per-file error")
	assert.Equal(t, "none", bad.Action)
}

// TestApplyApplyFileError drives the per-file ApplyFile-error branch: a notice-only
// style on MIT (which ships no standard header) makes render.Header error, so
// ApplyFile returns an error that Apply records on the file result.
func TestApplyApplyFileError(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root)
	writeFile(t, root, "main.go", "package main\n")
	gitAddCommit(t, root)

	cfg := agplConfig()
	cfg.License = "MIT"
	cfg.Style = model.StyleNotice // MIT has no StandardHeader -> empty header -> error

	rep, err := Apply(root, cfg, Options{Write: false})
	require.NoError(t, err)

	main := findResult(t, rep.Files, "main.go")
	assert.NotEmpty(t, main.Err, "render error must be recorded on the file result")
	assert.Equal(t, "none", main.Action)
}

// TestApplyManageLicenseFileDryRunIntegration confirms ManageLicenseFile=true folds
// the two managed entries into the report alongside the source file on a dry-run.
// (The ManageLicenseFiles unknown-license / LicenseFile-empty error branches inside
// Apply are unreachable from here because Apply validates the license via the same
// spdx.Lookup before the file pass; those branches are covered directly by the
// ManageLicenseFiles unit tests above.)
func TestApplyManageLicenseFileDryRunIntegration(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root)
	writeFile(t, root, "main.go", "package main\n")
	gitAddCommit(t, root)

	rep, err := Apply(root, agplConfig(), Options{Write: false, ManageLicenseFile: true})
	require.NoError(t, err)

	// The two managed entries appear in the report alongside the source file.
	assert.Equal(t, "insert", findResult(t, rep.Files, "LICENSE").Action)
	assert.Equal(t, "insert", findResult(t, rep.Files, filepath.Join("LICENSES", "AGPL-3.0-or-later.txt")).Action)
}

// TestApplyManageLicenseFilesError covers Apply's ManageLicenseFiles-error guard via
// the manageLicenseFilesFn seam. Apply validates cfg.License with the same spdx.Lookup
// ManageLicenseFiles uses, so the real call cannot fail for a valid license; the seam
// forces the error so Apply's propagation branch is exercised.
func TestApplyManageLicenseFilesError(t *testing.T) {
	boom := errors.New("synthetic manage-license-files failure")
	orig := manageLicenseFilesFn
	t.Cleanup(func() { manageLicenseFilesFn = orig })
	manageLicenseFilesFn = func(string, model.Config, Options) ([]model.FileResult, error) {
		return nil, boom
	}

	root := t.TempDir()
	gitInit(t, root)
	writeFile(t, root, "main.go", "package main\n")
	gitAddCommit(t, root)

	_, err := Apply(root, agplConfig(), Options{Write: false, ManageLicenseFile: true})
	require.ErrorIs(t, err, boom)
}

func TestLicenseWorkflow(t *testing.T) {
	t.Run("dry-run returns managed diffs without writing", func(t *testing.T) {
		root := t.TempDir()
		files, err := License(root, agplConfig(), Options{})
		require.NoError(t, err)
		assert.NotEmpty(t, findResult(t, files, "LICENSE").Diff)
		_, statErr := os.Stat(filepath.Join(root, "LICENSE"))
		assert.True(t, os.IsNotExist(statErr))
	})

	t.Run("write gate error is surfaced", func(t *testing.T) {
		_, err := License(t.TempDir(), agplConfig(), Options{Write: true})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "non-git directory")
	})

	t.Run("manage error is surfaced", func(t *testing.T) {
		cfg := agplConfig()
		cfg.License = "NOPE-1.0"
		_, err := License(t.TempDir(), cfg, Options{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown license")
	})

	t.Run("write commit succeeds with custom message", func(t *testing.T) {
		root := t.TempDir()
		gitInit(t, root)
		writeFile(t, root, "main.go", "package main\n")
		gitAddCommit(t, root)

		_, err := License(root, agplConfig(), Options{
			Write:         true,
			Commit:        true,
			CommitMessage: "chore: add license files",
		})
		require.NoError(t, err)
		assert.Equal(t, "chore: add license files", gitHeadSubject(t, root))
	})

	t.Run("write commit with no changes errors", func(t *testing.T) {
		root := t.TempDir()
		gitInit(t, root)
		writeFile(t, root, "main.go", "package main\n")
		gitAddCommit(t, root)

		_, err := License(root, agplConfig(), Options{Write: true, Commit: true})
		require.NoError(t, err)

		_, err = License(root, agplConfig(), Options{Write: true, Commit: true})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no paths to commit")
	})
}

func TestChangedPaths(t *testing.T) {
	files := []model.FileResult{
		{Path: "z.go", Action: "insert"},
		{Path: "z.go", Action: "replace"},
		{Path: "none.go", Action: "none"},
		{Path: "skip.go", Action: "skip"},
		{Path: "empty.go"},
		{Path: "err.go", Action: "insert", Err: "write failed"},
		{Path: "a.go", Action: "replace"},
	}

	assert.Equal(t, []string{"a.go", "z.go"}, changedPaths(files))
}
