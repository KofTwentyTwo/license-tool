// Package gitutil wraps the git operations the tool relies on: enumerating tracked
// files, checking working-tree cleanliness before a write, deriving the
// first-commit year for the git year policy, and making the optional atomic commit.
//
// WHY shell out to git rather than use a pure-Go git library: git ls-files is the
// authoritative honorer of .gitignore (including nested and global ignores), and
// matching its semantics in Go would be a reimplementation we would have to keep
// in sync. The non-git fallback path lives in internal/enumerate.
package gitutil

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// run executes git in path's working tree and returns trimmed stdout. WHY -C: it
// runs git as if started in path without changing the caller's working directory,
// so concurrent callers do not race on a shared cwd.
func run(path string, args ...string) (string, error) {
	full := append([]string{"-C", path}, args...)
	cmd := exec.Command("git", full...)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}

// IsRepo reports whether path is inside a git working tree.
func IsRepo(path string) bool {
	out, err := run(path, "rev-parse", "--is-inside-work-tree")
	return err == nil && out == "true"
}

// ListFiles returns repo-relative paths of all git-tracked files under path, as
// produced by `git ls-files` (which inherits .gitignore correctly).
func ListFiles(path string) ([]string, error) {
	out, err := run(path, "ls-files")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// IsClean reports whether the working tree at path has no uncommitted changes.
// Apply refuses to write to a dirty tree unless --allow-dirty is set.
func IsClean(path string) (bool, error) {
	out, err := run(path, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return out == "", nil
}

// FirstCommitYear returns the four-digit year of the repository's first (root)
// commit at path, for the git year policy (first-commit-year-to-current range).
func FirstCommitYear(path string) (int, error) {
	// --max-parents=0 selects root commit(s); %ad with a year-only date format
	// yields just the year. A repo can have multiple roots; take the earliest.
	out, err := run(path, "log", "--max-parents=0", "--date=format:%Y", "--format=%ad")
	if err != nil {
		return 0, err
	}
	if out == "" {
		return 0, fmt.Errorf("gitutil: no commits in %q", path)
	}
	earliest := 0
	for _, line := range strings.Split(out, "\n") {
		y, perr := strconv.Atoi(strings.TrimSpace(line))
		if perr != nil {
			continue
		}
		if earliest == 0 || y < earliest {
			earliest = y
		}
	}
	if earliest == 0 {
		return 0, fmt.Errorf("gitutil: could not parse first-commit year in %q", path)
	}
	return earliest, nil
}

// Commit stages all changes under path and creates a single commit with message.
// Used by apply --commit to make one atomic conventional commit per repo. A commit
// with nothing staged is reported as an error by git and surfaced here.
func Commit(path, message string) error {
	if _, err := run(path, "add", "-A"); err != nil {
		return err
	}
	if _, err := run(path, "commit", "-m", message); err != nil {
		return err
	}
	return nil
}
