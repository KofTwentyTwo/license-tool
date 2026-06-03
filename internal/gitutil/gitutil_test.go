package gitutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// requireGit skips the test when git is not on PATH so the suite stays green on
// hosts without git rather than failing for an environmental reason.
func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
}

// gitInit creates a quiet, identity-configured repo. Identity and gpgsign are set
// explicitly so commits succeed in CI where no global git config exists.
func gitInit(t *testing.T, root string) {
	t.Helper()
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

// writeFile creates parent dirs and writes a file under root.
func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	abs := filepath.Join(root, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
	require.NoError(t, os.WriteFile(abs, []byte(content), 0o644))
}

// commitAt stages everything and creates a commit whose author and committer dates
// are pinned to year, making FirstCommitYear deterministic across runs and hosts.
func commitAt(t *testing.T, root, message, year string) {
	t.Helper()
	cmd := exec.Command("git", "-C", root, "add", "-A")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git add: %s", out)

	date := year + "-06-15T12:00:00 +0000"
	cmd = exec.Command("git", "-C", root, "commit", "-q", "-m", message)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_DATE="+date,
		"GIT_COMMITTER_DATE="+date,
	)
	out, err = cmd.CombinedOutput()
	require.NoError(t, err, "git commit: %s", out)
}

// newRepo returns a fresh temp dir initialized as a git repo.
func newRepo(t *testing.T) string {
	t.Helper()
	requireGit(t)
	root := t.TempDir()
	gitInit(t, root)
	return root
}

// TestRunErrorWrapsGit drives the run helper's error branch through a public caller
// and asserts the wrapped error names the failing git subcommand.
func TestRunErrorWrapsGit(t *testing.T) {
	requireGit(t)
	dir := t.TempDir() // not a git repo

	_, err := ListFiles(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "git ls-files",
		"wrapped error should name the git subcommand that failed")
}

// TestIsRepo covers both branches: a real working tree reports true, a plain dir
// reports false because git rev-parse exits non-zero outside a repo.
func TestIsRepo(t *testing.T) {
	requireGit(t)

	repo := newRepo(t)
	nonRepo := t.TempDir()

	cases := []struct {
		name string
		path string
		want bool
	}{
		{"inside work tree", repo, true},
		{"plain directory", nonRepo, false},
		{"nonexistent path", filepath.Join(nonRepo, "does-not-exist"), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, IsRepo(tc.path))
		})
	}
}

// TestListFiles covers the tracked-file enumeration: a populated repo returns the
// sorted tracked set (and never lists .gitignore'd untracked files), while an empty
// repo returns a nil slice with no error.
func TestListFiles(t *testing.T) {
	requireGit(t)

	t.Run("tracked files only", func(t *testing.T) {
		root := newRepo(t)
		writeFile(t, root, "a.go", "package a\n")
		writeFile(t, root, "sub/b.go", "package b\n")
		writeFile(t, root, ".gitignore", "ignored.txt\n")
		writeFile(t, root, "ignored.txt", "nope\n") // untracked + ignored
		commitAt(t, root, "init", "2020")

		files, err := ListFiles(root)
		require.NoError(t, err)
		assert.Equal(t, []string{".gitignore", "a.go", "sub/b.go"}, files)
		assert.NotContains(t, files, "ignored.txt")
	})

	t.Run("empty repo returns nil", func(t *testing.T) {
		root := newRepo(t) // initialized, nothing committed/added
		files, err := ListFiles(root)
		require.NoError(t, err)
		assert.Nil(t, files)
	})

	t.Run("non-repo errors", func(t *testing.T) {
		_, err := ListFiles(t.TempDir())
		require.Error(t, err)
	})
}

// TestIsClean covers all three outcomes: a committed tree is clean, an added-but-
// uncommitted file makes it dirty, and a non-repo surfaces the error path.
func TestIsClean(t *testing.T) {
	requireGit(t)

	t.Run("clean after commit", func(t *testing.T) {
		root := newRepo(t)
		writeFile(t, root, "a.go", "package a\n")
		commitAt(t, root, "init", "2020")

		clean, err := IsClean(root)
		require.NoError(t, err)
		assert.True(t, clean)
	})

	t.Run("dirty with uncommitted change", func(t *testing.T) {
		root := newRepo(t)
		writeFile(t, root, "a.go", "package a\n")
		commitAt(t, root, "init", "2020")
		writeFile(t, root, "b.go", "package b\n") // new untracked file -> dirty

		clean, err := IsClean(root)
		require.NoError(t, err)
		assert.False(t, clean)
	})

	t.Run("non-repo errors", func(t *testing.T) {
		_, err := IsClean(t.TempDir())
		require.Error(t, err)
	})
}

// TestFirstCommitYear covers the happy path (single pinned root commit), the
// earliest-of-multiple-roots selection across merged histories, the no-commits
// error, and the non-repo git error.
func TestFirstCommitYear(t *testing.T) {
	requireGit(t)

	t.Run("single root commit", func(t *testing.T) {
		root := newRepo(t)
		writeFile(t, root, "a.go", "package a\n")
		commitAt(t, root, "init", "2018")
		// A later commit must not change the first-commit year.
		writeFile(t, root, "b.go", "package b\n")
		commitAt(t, root, "second", "2022")

		year, err := FirstCommitYear(root)
		require.NoError(t, err)
		assert.Equal(t, 2018, year)
	})

	t.Run("earliest of multiple roots", func(t *testing.T) {
		root := newRepo(t)
		writeFile(t, root, "a.go", "package a\n")
		commitAt(t, root, "init", "2017")

		// Create a second, independent root commit on an orphan branch with an
		// earlier pinned year, then merge it so the repo has two root commits.
		runGit(t, root, nil, "checkout", "-q", "--orphan", "other")
		// orphan checkout leaves the index populated; clear it so we commit only new content.
		runGit(t, root, nil, "rm", "-rf", "--quiet", ".")
		writeFile(t, root, "c.go", "package c\n")
		commitAt(t, root, "other-root", "2015")

		runGit(t, root, nil, "checkout", "-q", "main")
		// Merge unrelated histories so both roots are reachable from HEAD.
		runGit(t, root, []string{
			"GIT_AUTHOR_DATE=2023-06-15T12:00:00 +0000",
			"GIT_COMMITTER_DATE=2023-06-15T12:00:00 +0000",
		}, "merge", "-q", "--allow-unrelated-histories", "--no-edit", "other")

		year, err := FirstCommitYear(root)
		require.NoError(t, err)
		assert.Equal(t, 2015, year, "should pick the earliest of the two root-commit years")
	})

	t.Run("no commits errors", func(t *testing.T) {
		root := newRepo(t) // initialized but empty: git log exits non-zero
		_, err := FirstCommitYear(root)
		require.Error(t, err)
	})

	t.Run("non-repo errors", func(t *testing.T) {
		_, err := FirstCommitYear(t.TempDir())
		require.Error(t, err)
	})
}

// TestFirstCommitYearDefensiveBranches exercises the defensive parse branches in
// FirstCommitYear that real `git log --date=format:%Y --format=%ad` output cannot
// reach (empty-but-successful output, a non-numeric line, and all-lines-unparseable).
// It substitutes the run seam to feed crafted git output, then restores the default.
func TestFirstCommitYearDefensiveBranches(t *testing.T) {
	orig := run
	t.Cleanup(func() { run = orig })

	cases := []struct {
		name      string
		out       string
		err       error
		wantYear  int
		wantError bool
	}{
		{"empty but successful output", "", nil, 0, true},
		{"non-numeric lines only", "notayear\n----", nil, 0, true},
		{"earliest skips unparseable lines", "garbage\n2014\n2019", nil, 2014, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			run = func(path string, args ...string) (string, error) {
				return tc.out, tc.err
			}
			year, err := FirstCommitYear("ignored")
			if tc.wantError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantYear, year)
		})
	}
}

// runGit is a test helper that runs git -C root with optional extra env and fails
// the test on a non-zero exit, surfacing combined output for diagnosis.
func runGit(t *testing.T, root string, extraEnv []string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	if extraEnv != nil {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
}

// TestCommit covers the success path (staged change becomes one commit), the
// nothing-to-commit error (git commit exits non-zero with an empty index), and the
// non-repo error from the initial add.
func TestCommit(t *testing.T) {
	requireGit(t)

	t.Run("stages and commits", func(t *testing.T) {
		root := newRepo(t)
		writeFile(t, root, "a.go", "package a\n")
		// Make an initial commit so HEAD exists, then a tracked modification.
		commitAt(t, root, "init", "2020")
		writeFile(t, root, "a.go", "package a // edited\n")

		err := Commit(root, "chore: license headers")
		require.NoError(t, err)

		clean, cerr := IsClean(root)
		require.NoError(t, cerr)
		assert.True(t, clean, "tree should be clean after Commit stages and commits all changes")

		msg, gerr := run(root, "log", "-1", "--format=%s")
		require.NoError(t, gerr)
		assert.Equal(t, "chore: license headers", msg)
	})

	t.Run("nothing to commit errors", func(t *testing.T) {
		root := newRepo(t)
		writeFile(t, root, "a.go", "package a\n")
		commitAt(t, root, "init", "2020")
		// Tree is clean: add -A stages nothing, git commit exits non-zero.
		err := Commit(root, "no-op commit")
		require.Error(t, err)
	})

	t.Run("non-repo errors on add", func(t *testing.T) {
		err := Commit(t.TempDir(), "msg")
		require.Error(t, err)
	})
}
