// Package enumerate produces the ordered set of files a run will process. Inside a
// git repo it uses git ls-files (inheriting .gitignore); otherwise it walks the
// tree honoring .gitignore (via go-gitignore) plus config excludes. It skips
// symlinks and binaries (null-byte / UTF-8-validity heuristic) and classifies each
// surviving file via the file-type table.
package enumerate

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode/utf8"

	ignore "github.com/sabhiram/go-gitignore"

	"github.com/KofTwentyTwo/license-tool/internal/model"
)

// binaryScanLimit caps how many bytes IsBinary inspects. WHY: the null-byte/UTF-8
// heuristic is decisive within the first few KiB for real source files, and reading
// the whole of a multi-megabyte blob just to confirm it is binary wastes I/O. A
// truncated UTF-8 sequence at the boundary is tolerated (see IsBinary).
const binaryScanLimit = 8192

// Options controls enumeration scope and filtering.
type Options struct {
	// Includes are glob patterns that, when non-empty, restrict the set to matches.
	Includes []string
	// Excludes are gitignore-style patterns removed from the set (config + flags).
	Excludes []string
	// NoGitignore disables .gitignore inheritance on the non-git walk path.
	NoGitignore bool
	// Force permits enumerating a non-git directory (otherwise apply refuses).
	Force bool
}

// Entry is one enumerated file with its resolved file type and skip disposition.
type Entry struct {
	// Path is the path relative to the scan root.
	Path string
	// AbsPath is the absolute path on disk.
	AbsPath string
	// FileType is the matched type; valid only when Skip is false and the type matched.
	FileType model.FileType
	// Skip marks a file excluded from processing (binary, symlink, unknown, uncommentable).
	Skip bool
	// SkipReason explains a skip for the report ("binary", "symlink", "unknown type", "uncommentable").
	SkipReason string
}

// ContentClassifier classifies a path using the leading file bytes when the path
// alone is not enough, such as extensionless scripts with a shebang.
type ContentClassifier func(path string, head []byte) (model.FileType, bool)

// skip reasons surfaced to the report. WHY centralized: detect.go and report.go key
// off these exact tokens, so they are defined once to keep callers in sync.
const (
	reasonSymlink       = "symlink"
	reasonBinary        = "binary"
	reasonUnknownType   = "unknown type"
	reasonUncommentable = "uncommentable"
)

// filepathAbs and filepathRel are seams for the standard-library path helpers so
// tests can drive their (in practice unreachable) error returns. filepath.Abs only
// errors when os.Getwd fails, and filepath.Rel only errors on a base/target
// mismatch that WalkDir's contract precludes; production always uses the real
// functions, so swapping these in a test changes nothing about runtime behavior.
var (
	filepathAbs = filepath.Abs
	filepathRel = filepath.Rel
)

// Enumerate returns the files under root to process, classified and filtered.
// classify is the file-type lookup (filetype.Lookup or a filetype.Merge closure)
// so config overrides are honored.
//
// WHY two discovery strategies: git ls-files is the authoritative honorer of
// .gitignore (nested, global, and per-repo excludes), so we defer to it whenever
// root is inside a working tree. Only outside git do we approximate those semantics
// with a manual walk, which is necessarily a smaller subset of git's behavior.
func Enumerate(root string, opts Options, classify func(path string) (model.FileType, bool)) ([]Entry, error) {
	return enumerate(root, opts, func(path string, _ []byte) (model.FileType, bool) {
		return classify(path)
	}, false)
}

// WithContent is the content-aware variant of Enumerate. It reads the leading
// bytes once, passes them to classify, and reuses them for the binary check.
func WithContent(root string, opts Options, classify ContentClassifier) ([]Entry, error) {
	return enumerate(root, opts, classify, true)
}

func enumerate(root string, opts Options, classify ContentClassifier, contentAware bool) ([]Entry, error) {
	absRoot, err := filepathAbs(root)
	if err != nil {
		return nil, err
	}

	var relPaths []string
	if isGitRepo(absRoot) {
		relPaths, err = gitListFiles(absRoot)
	} else {
		relPaths, err = walkListFiles(absRoot, opts)
	}
	if err != nil {
		return nil, err
	}

	// Config/flag excludes apply uniformly on both paths. On the git path they layer
	// on top of git's own .gitignore handling; on the walk path they layer on top of
	// the .gitignore matcher already applied during the walk.
	var excluder *ignore.GitIgnore
	if len(opts.Excludes) > 0 {
		excluder = ignore.CompileIgnoreLines(opts.Excludes...)
	}

	entries := make([]Entry, 0, len(relPaths))
	for _, rel := range relPaths {
		// Normalize to forward slashes so glob/gitignore matching is OS-independent;
		// MatchesPath itself also normalizes, but Includes use filepath.Match.
		rel = filepath.ToSlash(rel)

		if !matchesIncludes(rel, opts.Includes) {
			continue
		}
		if excluder != nil && excluder.MatchesPath(rel) {
			continue
		}

		abs := filepath.Join(absRoot, filepath.FromSlash(rel))
		var entry Entry
		if contentAware {
			entry = classifyEntryContent(rel, abs, classify)
		} else {
			entry = classifyEntry(rel, abs, func(path string) (model.FileType, bool) {
				ft, ok := classify(path, nil)
				return ft, ok
			})
		}
		entries = append(entries, entry)
	}

	return entries, nil
}

func classifyEntryContent(rel, abs string, classify ContentClassifier) Entry {
	entry := Entry{Path: rel, AbsPath: abs}

	info, err := os.Lstat(abs)
	if err != nil {
		entry.Skip = true
		entry.SkipReason = reasonUnknownType
		return entry
	}
	if info.Mode()&os.ModeSymlink != 0 {
		entry.Skip = true
		entry.SkipReason = reasonSymlink
		return entry
	}
	if !info.Mode().IsRegular() {
		entry.Skip = true
		entry.SkipReason = reasonUnknownType
		return entry
	}

	head, rerr := readHead(abs)
	ft, ok := classify(rel, head)
	if !ok {
		entry.Skip = true
		entry.SkipReason = reasonUnknownType
		return entry
	}
	entry.FileType = ft

	if ft.Skip {
		entry.Skip = true
		entry.SkipReason = reasonUncommentable
		return entry
	}

	if rerr == nil && IsBinary(head) {
		entry.Skip = true
		entry.SkipReason = reasonBinary
		return entry
	}

	return entry
}

// classifyEntry resolves one path into an Entry, applying the skip ladder in the
// order that matters: symlink (never follow) before binary (never read past a NUL)
// before type classification (unknown vs uncommentable). git ls-files can list a
// path that no longer exists on disk (e.g. a staged deletion); a stat error there is
// treated as a skip rather than aborting the whole enumeration.
func classifyEntry(rel, abs string, classify func(path string) (model.FileType, bool)) Entry {
	entry := Entry{Path: rel, AbsPath: abs}

	info, err := os.Lstat(abs)
	if err != nil {
		entry.Skip = true
		entry.SkipReason = reasonUnknownType
		return entry
	}
	if info.Mode()&os.ModeSymlink != 0 {
		entry.Skip = true
		entry.SkipReason = reasonSymlink
		return entry
	}
	// Non-regular files (directories, devices, sockets) cannot carry a header. git
	// ls-files only yields blobs and gitlinks, but the walk path and submodule links
	// can surface a directory; guard rather than attempt to read it.
	if !info.Mode().IsRegular() {
		entry.Skip = true
		entry.SkipReason = reasonUnknownType
		return entry
	}

	ft, ok := classify(rel)
	if !ok {
		// Unknown type: do not even read the bytes; nothing downstream can act on it.
		entry.Skip = true
		entry.SkipReason = reasonUnknownType
		return entry
	}
	entry.FileType = ft

	if ft.Skip {
		// Uncommentable format (JSON and friends): matched but never editable.
		entry.Skip = true
		entry.SkipReason = reasonUncommentable
		return entry
	}

	// Binary check last among reads: a known type whose bytes look binary (e.g. a
	// minified blob saved as .js) is still skipped to avoid corrupting it.
	if content, rerr := readHead(abs); rerr == nil && IsBinary(content) {
		entry.Skip = true
		entry.SkipReason = reasonBinary
		return entry
	}

	return entry
}

// readHead reads up to binaryScanLimit bytes, enough for the binary heuristic.
func readHead(abs string) ([]byte, error) {
	f, err := os.Open(abs)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	buf := make([]byte, binaryScanLimit)
	n, err := f.Read(buf)
	if err != nil && n == 0 {
		return nil, err
	}
	return buf[:n], nil
}

// matchesIncludes reports whether rel satisfies the include globs. An empty include
// set matches everything (the common case); otherwise a path must match at least one
// glob, tested against both the full relative path and its base name so a bare
// "*.go" works regardless of directory depth.
func matchesIncludes(rel string, includes []string) bool {
	if len(includes) == 0 {
		return true
	}
	base := filepath.Base(rel)
	for _, pat := range includes {
		if ok, _ := filepath.Match(pat, rel); ok {
			return true
		}
		if ok, _ := filepath.Match(pat, base); ok {
			return true
		}
	}
	return false
}

// isGitRepo reports whether path is inside a git working tree. WHY shell out rather
// than look for a .git directory: git rev-parse correctly resolves worktrees, nested
// repos, and the GIT_DIR/GIT_WORK_TREE environment, which a bare directory probe
// would miss. A missing git binary or a non-repo both yield a non-zero exit, which
// we read as "not a repo" and fall back to the manual walk.
func isGitRepo(path string) bool {
	cmd := exec.Command("git", "-C", path, "rev-parse", "--is-inside-work-tree")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

// gitListFiles returns repo-relative paths of files under path via git ls-files.
// WHY -z: paths may contain spaces or other shell-significant bytes, so the NUL
// terminator is the only safe record separator. WHY tracked-only (no -o): the audit
// target is the committed source set; untracked-but-not-ignored scratch files are
// out of scope and would create churn.
func gitListFiles(path string) ([]string, error) {
	cmd := exec.Command("git", "-C", path, "ls-files", "-z")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	parts := bytes.Split(out, []byte{0})
	paths := make([]string, 0, len(parts))
	for _, p := range parts {
		if len(p) == 0 {
			continue
		}
		paths = append(paths, string(p))
	}
	return paths, nil
}

// walkListFiles walks the tree rooted at path, returning repo-relative paths and
// honoring .gitignore (unless disabled) plus config excludes. WHY a single root-level
// .gitignore matcher rather than per-directory stacking: this is the explicit
// non-git fallback, where matching git's full nested-ignore semantics is out of
// scope (the doc points at git ls-files for that). The matcher still covers the
// common case of a top-level .gitignore, and config excludes layer on top in
// Enumerate. Symlinked directories are not descended (skipped, like symlinked files).
func walkListFiles(path string, opts Options) ([]string, error) {
	var matcher *ignore.GitIgnore
	if !opts.NoGitignore {
		if m, err := ignore.CompileIgnoreFile(filepath.Join(path, ".gitignore")); err == nil {
			matcher = m
		}
	}

	var paths []string
	err := filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, rerr := filepathRel(path, p)
		if rerr != nil {
			return rerr
		}
		if rel == "." {
			return nil
		}
		relSlash := filepath.ToSlash(rel)

		if d.IsDir() {
			// The .git directory is never part of the source set and can be huge;
			// prune it before any per-entry work.
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			// An ignored directory is pruned wholesale so we never descend into it,
			// matching git's "excluded directory is not listed" behavior.
			if matcher != nil && matcher.MatchesPath(relSlash+"/") {
				return filepath.SkipDir
			}
			return nil
		}

		// Files: symlinks are recorded so the caller can report them, rather than
		// silently dropped; classifyEntry sets the symlink skip reason.
		if matcher != nil && matcher.MatchesPath(relSlash) {
			return nil
		}
		paths = append(paths, rel)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return paths, nil
}

// IsBinary reports whether content looks binary (contains a null byte or is not
// valid UTF-8), so such files are skipped and reported rather than edited.
//
// WHY this heuristic: license headers are inserted into text, and a NUL byte is the
// single most reliable signal of a binary blob (no valid UTF-8 text file contains
// one). A final, possibly-truncated multi-byte rune at the scan boundary is
// tolerated so a legitimately UTF-8 file is not misjudged binary merely because the
// 8 KiB window split a character.
func IsBinary(content []byte) bool {
	if len(content) == 0 {
		return false
	}
	if bytes.IndexByte(content, 0) >= 0 {
		return true
	}
	if utf8.Valid(content) {
		return false
	}
	// Drop a trailing partial rune and re-test: a truncated read can leave an
	// incomplete sequence that is otherwise valid UTF-8.
	trimmed := content
	for i := 0; i < utf8.UTFMax && len(trimmed) > 0; i++ {
		trimmed = trimmed[:len(trimmed)-1]
		if utf8.Valid(trimmed) {
			return false
		}
	}
	return true
}
