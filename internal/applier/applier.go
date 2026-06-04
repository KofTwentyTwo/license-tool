// Package applier performs Mode B: it renders the canonical header per file,
// computes a unified diff, and (with --write) atomically replaces the identified
// header block or inserts one at the correct offset. It preserves preserve-first
// prefixes, line endings (LF/CRLF), and the trailing newline, never deletes
// non-header content, requires a clean git tree (unless overridden), and offers an
// opt-in single conventional commit per repo. It also manages the top-level
// LICENSE and LICENSES/<id>.txt files.
package applier

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/hexops/gotextdiff"
	"github.com/hexops/gotextdiff/myers"
	"github.com/hexops/gotextdiff/span"

	"github.com/KofTwentyTwo/license-tool/internal/config"
	"github.com/KofTwentyTwo/license-tool/internal/detect"
	"github.com/KofTwentyTwo/license-tool/internal/enumerate"
	"github.com/KofTwentyTwo/license-tool/internal/gitutil"
	"github.com/KofTwentyTwo/license-tool/internal/model"
	"github.com/KofTwentyTwo/license-tool/internal/render"
	"github.com/KofTwentyTwo/license-tool/internal/report"
	"github.com/KofTwentyTwo/license-tool/internal/spdx"
)

// Options controls an apply / license run.
type Options struct {
	// Write applies changes; when false the run is a dry-run producing diffs only.
	Write bool
	// Includes restricts source-file processing to matching globs.
	Includes []string
	// AllowDirty permits writing to a dirty git working tree (--allow-dirty).
	AllowDirty bool
	// Force permits writing in a non-git directory (--force).
	Force bool
	// NoGitignore disables .gitignore inheritance on the non-git walk path.
	NoGitignore bool
	// Commit makes one atomic conventional commit per repo after a successful write.
	Commit bool
	// CommitMessage is the commit message template (--commit-message); empty = default.
	CommitMessage string
	// ManageLicenseFile writes top-level LICENSE + LICENSES/<id>.txt when true.
	ManageLicenseFile bool
}

// HeaderRenderFunc renders the comment-wrapped header for a file; ApplyFile calls
// it so the render package stays the single source of header text.
type HeaderRenderFunc func(ft model.FileType) (string, error)

// manageLicenseFilesFn is the seam Apply uses to manage the top-level license
// files. It defaults to ManageLicenseFiles; because Apply validates cfg.License via
// the same spdx.Lookup beforehand, the only way ManageLicenseFiles errors here is a
// render failure that cannot occur for a looked-up license, so a test reassigns
// this seam to exercise Apply's error-propagation guard.
var manageLicenseFilesFn = ManageLicenseFiles

// detectFn is the seam ApplyFile uses to detect an existing header. detect.Detect
// errors only when its (currently infallible) preserve-boundary step fails, so this
// seam lets ApplyFile's detect-error guard be exercised without changing behavior.
var detectFn = detect.Detect

// Apply renders and (optionally) writes headers across the repo rooted at path
// under cfg and opts, returning the per-file results (including dry-run diffs).
// Flow: clean-tree gate, resolve license + year, enumerate, per-file render/splice,
// optional atomic write, optional license-file management, optional commit.
func Apply(path string, cfg model.Config, opts Options) (model.Report, error) {
	if opts.Write {
		if err := gateWrite(path, opts); err != nil {
			return model.Report{}, err
		}
	}

	license, ok := spdx.Lookup(cfg.License)
	if !ok {
		return model.Report{}, fmt.Errorf("applier: unknown license %q", cfg.License)
	}

	year, err := render.NewYearResolver(cfg.Year).Resolve(path, time.Now().Year())
	if err != nil {
		return model.Report{}, err
	}

	renderHeader := func(ft model.FileType) (string, error) {
		return render.Header(render.HeaderInput{
			License:  license,
			Holder:   cfg.Holder,
			Year:     year,
			Style:    cfg.Style,
			FileType: ft,
		})
	}

	entries, err := enumerate.EnumerateContent(path, enumerate.Options{
		Includes:    opts.Includes,
		Excludes:    cfg.Excludes,
		NoGitignore: opts.NoGitignore,
		Force:       opts.Force,
	}, config.ContentLookupFunc(cfg))
	if err != nil {
		return model.Report{}, err
	}

	files := make([]model.FileResult, 0, len(entries))
	for _, e := range entries {
		fr := model.FileResult{Path: e.Path, FileType: e.FileType.Name, Skipped: e.Skip, Action: "none"}
		if e.Skip {
			fr.SkipReason = e.SkipReason
			files = append(files, fr)
			continue
		}
		content, rerr := os.ReadFile(e.AbsPath)
		if rerr != nil {
			fr.Err = rerr.Error()
			files = append(files, fr)
			continue
		}
		// Record the pre-apply header state for the report's by-license view.
		if d, derr := detect.Detect(content, e.FileType); derr == nil {
			fr.Detected = d
		}
		newContent, diff, action, aerr := ApplyFile(content, e.FileType, renderHeader)
		if aerr != nil {
			fr.Err = aerr.Error()
			files = append(files, fr)
			continue
		}
		fr.Action = action
		if !opts.Write {
			fr.Diff = diff
		}
		if opts.Write && action != "none" && action != "skip" {
			if werr := AtomicWrite(e.AbsPath, newContent); werr != nil {
				fr.Err = werr.Error()
			}
		}
		files = append(files, fr)
	}

	if opts.ManageLicenseFile {
		lf, lerr := manageLicenseFilesFn(path, cfg, opts)
		if lerr != nil {
			return model.Report{}, lerr
		}
		files = append(files, lf...)
	}

	if opts.Write && opts.Commit {
		if cerr := commitChangedFiles(path, cfg, opts, files, "chore: standardize license headers to %s"); cerr != nil {
			return model.Report{}, cerr
		}
	}

	return report.Build(path, cfg, files, nil, nil), nil
}

// License manages only the top-level LICENSE files with the same write gate and
// scoped commit semantics as Apply.
func License(path string, cfg model.Config, opts Options) ([]model.FileResult, error) {
	if opts.Write {
		if err := gateWrite(path, opts); err != nil {
			return nil, err
		}
	}

	files, err := ManageLicenseFiles(path, cfg, opts)
	if err != nil {
		return nil, err
	}

	if opts.Write && opts.Commit {
		if err := commitChangedFiles(path, cfg, opts, files, "chore: standardize license files to %s"); err != nil {
			return nil, err
		}
	}
	return files, nil
}

// ApplyFile renders and computes the new content for a single file in memory,
// returning the new bytes, the unified diff, and the action ("none"|"insert"|
// "replace"|"skip"). It performs no disk I/O so it is safe to unit-test; Apply
// handles atomic on-disk replacement.
func ApplyFile(content []byte, ft model.FileType, in HeaderRenderFunc) (newContent []byte, diff string, action string, err error) {
	if ft.Skip {
		return content, "", "skip", nil
	}
	headerLF, herr := in(ft)
	if herr != nil {
		return content, "", "none", herr
	}
	detected, derr := detectFn(content, ft)
	if derr != nil {
		return content, "", "none", derr
	}
	newContent, action = render.Splice(content, ft, headerLF, detected)
	diff = unifiedDiff(ft.Name, content, newContent)
	return newContent, diff, action, nil
}

// licenseFileFn and licensesEntryFn are seams over the render entry points so tests
// can drive the render-failure branches. Every curated license that spdx.Lookup
// returns carries non-empty text, so these renderers never fail in production from a
// looked-up license; the seams let those guards be exercised without changing
// runtime behavior.
var (
	licenseFileFn   = render.LicenseFile
	licensesEntryFn = render.LicensesEntry
)

// ManageLicenseFiles writes (or, on dry-run, diffs) the top-level LICENSE file and
// the LICENSES/<id>.txt REUSE entry for cfg's license at the repo root.
func ManageLicenseFiles(path string, cfg model.Config, opts Options) ([]model.FileResult, error) {
	license, ok := spdx.Lookup(cfg.License)
	if !ok {
		return nil, fmt.Errorf("applier: unknown license %q", cfg.License)
	}

	body, err := licenseFileFn(license)
	if err != nil {
		return nil, err
	}
	entry, err := licensesEntryFn(license)
	if err != nil {
		return nil, err
	}

	return []model.FileResult{
		writeManaged(path, "LICENSE", []byte(body), opts),
		writeManaged(path, filepath.Join("LICENSES", cfg.License+".txt"), []byte(entry), opts),
	}, nil
}

// writeManaged computes the action/diff for a managed whole-file (LICENSE or a
// LICENSES entry) and, on --write, atomically writes it. It never errors the run;
// a write failure is recorded on the result so one bad path does not abort apply.
func writeManaged(root, rel string, want []byte, opts Options) model.FileResult {
	abs := filepath.Join(root, rel)
	fr := model.FileResult{Path: rel, FileType: "license", Action: "none"}

	existing, rerr := os.ReadFile(abs)
	switch {
	case rerr == nil && string(existing) == string(want):
		return fr
	case rerr == nil:
		fr.Action = "replace"
	default:
		fr.Action = "insert"
	}
	fr.Diff = unifiedDiff(rel, existing, want)
	if opts.Write {
		fr.Diff = ""
	}

	if opts.Write {
		if mkerr := os.MkdirAll(filepath.Dir(abs), 0o755); mkerr != nil {
			fr.Err = mkerr.Error()
			return fr
		}
		if werr := AtomicWrite(abs, want); werr != nil {
			fr.Err = werr.Error()
		}
	}
	return fr
}

// tmpWrite, tmpClose, and chmodFn are seams over the temp-file write/close and the
// chmod step so tests can drive the otherwise-environment-dependent failure
// branches (a short write, a close error, a chmod refusal) deterministically.
// Production always uses the real os operations, so runtime behavior is unchanged.
var (
	tmpWrite = func(f *os.File, b []byte) (int, error) { return f.Write(b) }
	tmpClose = func(f *os.File) error { return f.Close() }
	chmodFn  = os.Chmod
)

// AtomicWrite writes content to path via a temp-file-then-rename so a crash never
// leaves a half-written source file. It preserves the original file mode.
func AtomicWrite(path string, content []byte) error {
	dir := filepath.Dir(path)
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}

	tmp, err := os.CreateTemp(dir, ".license-tool-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// Best-effort cleanup; a no-op once the rename below succeeds.
	defer os.Remove(tmpName)

	if _, err := tmpWrite(tmp, content); err != nil {
		_ = tmpClose(tmp)
		return err
	}
	if err := tmpClose(tmp); err != nil {
		return err
	}
	if err := chmodFn(tmpName, mode); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func commitChangedFiles(path string, cfg model.Config, opts Options, files []model.FileResult, defaultMessage string) error {
	msg := opts.CommitMessage
	if msg == "" {
		msg = fmt.Sprintf(defaultMessage, cfg.License)
	}
	return gitutil.CommitPaths(path, msg, changedPaths(files))
}

func changedPaths(files []model.FileResult) []string {
	seen := map[string]bool{}
	paths := make([]string, 0, len(files))
	for _, fr := range files {
		if fr.Err != "" || fr.Action == "none" || fr.Action == "skip" || fr.Action == "" {
			continue
		}
		if seen[fr.Path] {
			continue
		}
		seen[fr.Path] = true
		paths = append(paths, fr.Path)
	}
	sort.Strings(paths)
	return paths
}

// gateWrite enforces the write-safety policy: in a git repo, refuse a dirty tree
// unless --allow-dirty; outside a git repo, refuse unless --force.
func gateWrite(path string, opts Options) error {
	if gitutil.IsRepo(path) {
		if opts.AllowDirty {
			return nil
		}
		clean, err := gitutil.IsClean(path)
		if err != nil {
			return err
		}
		if !clean {
			return errors.New("applier: refusing to write to a dirty git tree; commit or stash first, or pass --allow-dirty")
		}
		return nil
	}
	if !opts.Force {
		return errors.New("applier: refusing to write in a non-git directory without --force")
	}
	return nil
}

// unifiedDiff returns a unified diff of before->after labeled with name, or "" when
// they are identical.
func unifiedDiff(name string, before, after []byte) string {
	if string(before) == string(after) {
		return ""
	}
	edits := myers.ComputeEdits(span.URIFromPath(name), string(before), string(after))
	return fmt.Sprint(gotextdiff.ToUnified(name, name, string(before), edits))
}
