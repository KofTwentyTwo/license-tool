// Package report builds the audit/apply Report model and renders it to text
// (human), JSON (machine), and Markdown (report file). It also owns the check exit
// decision via the Report.Passed flag and the legal disclaimer banner.
//
// The audit and check subcommands call into here to do their work; cmd wires the
// flags. Build is the load-bearing core: it folds per-file results, dependency
// results, and policy verdicts into one Report with deterministic aggregate counts
// and stable ordering, so every renderer (text/JSON/Markdown) emits byte-identical
// output for identical inputs regardless of input order.
package report

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/KofTwentyTwo/license-tool/internal/model"
	"github.com/KofTwentyTwo/license-tool/internal/policy"
	"github.com/KofTwentyTwo/license-tool/internal/spdx"
)

// Disclaimer is the mandatory non-legal-advice banner every audit/check output
// must carry, per the requirements.
const Disclaimer = "This tool reports and enforces license metadata; it is not legal advice."

// noLicenseKey is the synthetic SPDX bucket used in LicenseCounts for managed
// source files that carry no detectable license id. WHY a sentinel rather than the
// empty string: an empty map key is invisible in rendered output and silently
// indistinguishable from "absent", so missing headers would never surface in the
// by-license breakdown. A named bucket makes the gap explicit and sortable.
const noLicenseKey = "(no-header)"

// Format selects the output rendering.
type Format int

const (
	// FormatText is the default human-readable terminal output.
	FormatText Format = iota
	// FormatJSON is machine-readable output.
	FormatJSON
	// FormatMarkdown is a Markdown report file.
	FormatMarkdown
)

// ParseFormat parses a --format token ("text"|"json"|"markdown") into a Format.
func ParseFormat(raw string) (Format, error) {
	switch raw {
	case "text", "":
		return FormatText, nil
	case "json":
		return FormatJSON, nil
	case "markdown", "md":
		return FormatMarkdown, nil
	default:
		return FormatText, fmt.Errorf("report: unknown format %q", raw)
	}
}

// String renders a Format as its canonical token, for diagnostics.
func (f Format) String() string {
	switch f {
	case FormatJSON:
		return "json"
	case FormatMarkdown:
		return "markdown"
	default:
		return "text"
	}
}

// Options configures an audit/check run that builds a Report.
type Options struct {
	// Format selects the output rendering.
	Format Format
	// IncludeDeps enables dependency-license resolution (audit only).
	IncludeDeps bool
	// ResolveDeps selects the resolver tier ("ondisk"|"tool"|"off").
	ResolveDeps string
	// AllowToolShellOut mirrors ResolveDeps=="tool" for the resolvers.
	AllowToolShellOut bool
}

// Build folds the raw outputs of an audit pass into a finished Report: it copies
// the per-file results and dependency results, derives the by-license / by-category
// / by-file-type aggregate counts, attaches the repo-level violation tokens, and
// sets Passed from the configured fail_on conditions.
//
// WHY this is a standalone, side-effect-free function (rather than only living
// inside Audit): the orchestration packages it would call (enumerate, detect,
// resolve) are independently developed, but the report shape is fixed now.
// Exposing Build lets the renderers, the integration layer, and tests all assemble
// a Report from already-computed inputs without re-running the whole pipeline, and
// guarantees one canonical aggregation/ordering implementation.
//
// Determinism: counts are derived purely from the inputs; every slice the renderers
// later walk is sorted by Render, so input ordering never leaks into output. Build
// itself preserves the caller's file/dependency ordering (callers that need
// stability sort before or rely on Render's sort), but the aggregate maps are
// order-independent by construction.
func Build(root string, cfg model.Config, files []model.FileResult, deps []model.DependencyLicense, repoViolations []policy.Violation) model.Report {
	r := model.Report{
		Root:           root,
		Config:         cfg,
		Files:          append([]model.FileResult(nil), files...),
		Dependencies:   append([]model.DependencyLicense(nil), deps...),
		LicenseCounts:  map[string]int{},
		CategoryCounts: map[string]int{},
		FileTypeCounts: map[string]int{},
	}

	// Collect every violation token in one ordered set: per-file violations already
	// live on each FileResult; repo-level violations are passed in. The Report's
	// Violations field is the repo-level set (file-scoped findings stay on the file),
	// matching how check evaluates fail_on against the union.
	allConditions := make([]model.FailCondition, 0)

	for i := range r.Files {
		fr := r.Files[i]

		// File-type breakdown counts every classified file, skipped or not, so the
		// report reflects the full tree composition (a JSON file still contributes to
		// "JSON: N" even though it is never edited).
		if fr.FileType != "" {
			r.FileTypeCounts[fr.FileType]++
		}

		// License + category breakdowns count only files that participate in license
		// management: skipped files (binary, uncommentable, unknown) carry no managed
		// header and must not inflate the by-license tally.
		if !fr.Skipped {
			id := fr.Detected.SPDXID
			if fr.Detected.Present && id != "" {
				r.LicenseCounts[id]++
				r.CategoryCounts[categoryToken(id)]++
			} else {
				// A managed-but-headerless file lands in the (none) bucket so missing
				// coverage is visible in the by-license view, and as unknown by category.
				r.LicenseCounts[noLicenseKey]++
				r.CategoryCounts[model.CategoryUnknown.String()]++
			}
		}

		// Fold each file's own violation tokens into the fail-on evaluation set. We
		// re-map the stable token back to its FailCondition so Passed sees both
		// file-scoped and repo-scoped findings.
		for _, tok := range fr.Violations {
			if c, ok := conditionFromToken(tok); ok {
				allConditions = append(allConditions, c)
			}
		}
	}

	// Dependencies that fail to resolve are a first-class report concern even before
	// policy runs: surface the unresolved condition so check can gate on it.
	for _, dep := range r.Dependencies {
		if dep.Resolution != model.ResolutionResolved {
			allConditions = append(allConditions, model.FailOnUnresolvedDependency)
		}
	}

	// Repo-level violation tokens (allow/deny/required/heterogeneity/incompatibility)
	// become the Report.Violations slice and also feed the fail-on evaluation.
	repoTokens := make([]string, 0, len(repoViolations))
	for _, v := range repoViolations {
		repoTokens = append(repoTokens, v.Token())
		allConditions = append(allConditions, v.Condition)
	}
	sort.Strings(repoTokens)
	r.Violations = dedupeStrings(repoTokens)

	// Synthesize Violation records for Passed's signature from the collected
	// conditions; Passed only inspects the condition, so the other fields are unused.
	verdict := make([]policy.Violation, 0, len(allConditions))
	for _, c := range allConditions {
		verdict = append(verdict, policy.Violation{Condition: c})
	}
	r.Passed = policy.Passed(verdict, cfg.Policy.FailOn)

	return r
}

// categoryToken returns the policy category token for an SPDX id, consulting the
// vendored curated set. Ids outside the curated rendering set classify as unknown,
// which is the honest answer: we have no vendored metadata to categorize them.
func categoryToken(id string) string {
	return classifyCategory(id).String()
}

// classifyCategory maps an SPDX id to its model.Category via the vendored snapshot;
// ids outside the curated set are CategoryUnknown.
func classifyCategory(id string) model.Category {
	if lic, ok := spdx.Lookup(id); ok {
		return lic.Category
	}
	return model.CategoryUnknown
}

// conditionFromToken maps a stable fail_on token back to its FailCondition. It is
// the inverse of FailCondition.String for the four known conditions.
func conditionFromToken(tok string) (model.FailCondition, bool) {
	switch tok {
	case model.FailOnMissingHeader.String():
		return model.FailOnMissingHeader, true
	case model.FailOnUnknownLicense.String():
		return model.FailOnUnknownLicense, true
	case model.FailOnPolicyViolation.String():
		return model.FailOnPolicyViolation, true
	case model.FailOnUnresolvedDependency.String():
		return model.FailOnUnresolvedDependency, true
	default:
		return 0, false
	}
}

// dedupeStrings returns s with adjacent duplicates removed. It assumes s is already
// sorted, which it is at every call site, so the dedupe is a single linear pass.
func dedupeStrings(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	out := s[:1]
	for _, v := range s[1:] {
		if v != out[len(out)-1] {
			out = append(out, v)
		}
	}
	return out
}

// SourceFile is one enumerated source file handed to the audit pipeline: its
// relative path, on-disk content, and resolved file-type name. It mirrors the
// minimum the report layer needs from the (separately built) enumerate package, so
// report does not compile-depend on that in-flight package.
type SourceFile struct {
	// Path is the file path relative to the scan root.
	Path string
	// FileType is the matched file type, supplying the name for reporting and the
	// comment/preserve rules for detection. Zero value when unmatched.
	FileType model.FileType
	// Content is the file's bytes, for header detection.
	Content []byte
	// Skip marks a file the enumerator excluded (binary, symlink, uncommentable, unknown).
	Skip bool
	// SkipReason explains a skip for the report.
	SkipReason string
}

// Pipeline injects the audit's collaborators as functions, so the report package
// stays decoupled from the concrete enumerate / detect / resolve implementations
// (which are built in parallel by other agents and must not be compile-time
// dependencies of report). The integration layer constructs a Pipeline that wires
// the real packages; tests construct one with fakes.
//
// WHY function injection rather than direct imports: the report shape is frozen now,
// but the pipeline packages are still in flight. Decoupling lets report build, test,
// and ship its model/render/aggregation contract independently, and gives the
// integration agent a single, explicit wiring point.
type Pipeline struct {
	// Enumerate returns the source files under root, classified and filtered. A hard
	// error aborts the audit (no file set means no report).
	Enumerate func(root string, excludes []string) ([]SourceFile, error)
	// Detect identifies a managed header in one file's content given its file type.
	// A per-file error is recorded on the FileResult and never aborts the run.
	Detect func(content []byte, ft model.FileType) (model.DetectedHeader, error)
	// ResolveDeps returns the dependency licenses under root, already resolved or
	// marked unresolved. Nil when dependency resolution is disabled.
	ResolveDeps func(root string, allowToolShellOut bool) ([]model.DependencyLicense, error)
}

// Audit builds the full audit Report for the repo rooted at path under cfg and opts
// using pipe's injected collaborators: enumerate, detect, resolve dependencies,
// classify, and apply policy, then fold everything through Build.
//
// It surfaces a hard failure from enumeration as an error (the run cannot proceed
// without a file set); per-file detection failures are recorded on the FileResult
// (Err) and never abort the whole audit, so one unreadable file does not blind the
// report to the rest of the tree. A nil field in pipe is treated as "stage absent":
// missing Enumerate is an error, missing Detect leaves headers undetected, missing
// ResolveDeps yields no dependencies.
func Audit(path string, cfg model.Config, opts Options, pipe Pipeline) (model.Report, error) {
	if pipe.Enumerate == nil {
		return model.Report{}, errors.New("report: Audit requires a Pipeline.Enumerate")
	}

	sources, err := pipe.Enumerate(path, cfg.Excludes)
	if err != nil {
		return model.Report{}, fmt.Errorf("report: enumerate %q: %w", path, err)
	}

	files := make([]model.FileResult, 0, len(sources))
	var allViolations []policy.Violation
	for _, s := range sources {
		fr := model.FileResult{
			Path:     s.Path,
			FileType: s.FileType.Name,
			Skipped:  s.Skip,
			Action:   "none",
		}
		if s.Skip {
			fr.SkipReason = s.SkipReason
			files = append(files, fr)
			continue
		}

		if pipe.Detect != nil {
			detected, derr := pipe.Detect(s.Content, s.FileType)
			if derr != nil {
				fr.Err = derr.Error()
				files = append(files, fr)
				continue
			}
			fr.Detected = detected
		}

		// Per-file policy verdicts attach their stable tokens to the file so the
		// by-file view and the fail-on evaluation both see them; the full structs are
		// retained for the attributable ViolationDetails.
		fileViolations := policy.EvaluateFile(cfg.Policy, fr)
		for _, v := range fileViolations {
			fr.Violations = append(fr.Violations, v.Token())
		}
		sort.Strings(fr.Violations)
		fr.Violations = dedupeStrings(fr.Violations)
		allViolations = append(allViolations, fileViolations...)

		files = append(files, fr)
	}

	var deps []model.DependencyLicense
	var repoViolations []policy.Violation

	if opts.IncludeDeps && opts.ResolveDeps != "off" && pipe.ResolveDeps != nil {
		found, rerr := pipe.ResolveDeps(path, opts.AllowToolShellOut)
		if rerr != nil {
			return model.Report{}, fmt.Errorf("report: resolve dependencies %q: %w", path, rerr)
		}
		deps = found
		for _, dep := range deps {
			repoViolations = append(repoViolations, policy.EvaluateDependency(cfg.Policy, dep)...)
		}
	}

	// Repo-level checks run over the distinct SPDX ids actually found in source.
	repoViolations = append(repoViolations, policy.EvaluateRepo(cfg.Policy, distinctSourceIDs(files))...)
	allViolations = append(allViolations, repoViolations...)

	r := Build(path, cfg, files, deps, repoViolations)
	r.ViolationDetails = toViolationDetails(allViolations)
	return r, nil
}

// toViolationDetails maps the full policy.Violation set (file + dependency + repo) to
// attributable model.ViolationDetail records, sorted and de-duplicated for stable
// output. WHY the report layer owns this mapping: model stays free of a policy import,
// and the rich attribution policy already produces is preserved instead of being
// flattened to bare tokens.
func toViolationDetails(vs []policy.Violation) []model.ViolationDetail {
	out := make([]model.ViolationDetail, 0, len(vs))
	for _, v := range vs {
		out = append(out, model.ViolationDetail{
			Condition: v.Condition,
			SPDXID:    v.SPDXID,
			Path:      v.Path,
			Message:   v.Message,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Condition != out[j].Condition {
			return out[i].Condition < out[j].Condition
		}
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Message < out[j].Message
	})
	return dedupeViolationDetails(out)
}

// dedupeViolationDetails removes adjacent duplicate findings; the input is pre-sorted,
// so a single linear pass suffices.
func dedupeViolationDetails(in []model.ViolationDetail) []model.ViolationDetail {
	if len(in) == 0 {
		return nil
	}
	out := in[:1]
	for _, v := range in[1:] {
		if v != out[len(out)-1] {
			out = append(out, v)
		}
	}
	return out
}

// distinctSourceIDs returns the sorted, unique set of SPDX ids detected across the
// source files, for repo-level policy evaluation.
func distinctSourceIDs(files []model.FileResult) []string {
	seen := map[string]bool{}
	for _, fr := range files {
		if !fr.Skipped && fr.Detected.Present && fr.Detected.SPDXID != "" {
			seen[fr.Detected.SPDXID] = true
		}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// RenderOptions tunes the human/JSON rendering of a report. The zero value is the
// default full report (every renderer's historical behavior).
type RenderOptions struct {
	// Summary suppresses the per-file list, the per-dependency list, and pending
	// diffs, leaving findings, the count rollups, and policy violations.
	Summary bool
	// GroupBy organizes the source-file listing under each value of a dimension
	// instead of a flat list. GroupNone is the flat default.
	GroupBy GroupDimension
	// SortByCount orders the count rollups and groups by descending count (ties by
	// key) instead of the default alphabetical key order.
	SortByCount bool
}

// Render writes the report in the requested format to w with default options. It is
// the backward-compatible entry point; callers needing summary/group-by use
// RenderWithOptions.
func Render(w io.Writer, r model.Report, format Format) error {
	return RenderWithOptions(w, r, format, RenderOptions{})
}

// RenderWithOptions writes the report honoring opts. All three renderers walk the
// model through a sorted view, so output is deterministic for identical inputs
// regardless of input ordering.
func RenderWithOptions(w io.Writer, r model.Report, format Format, opts RenderOptions) error {
	switch format {
	case FormatText:
		return renderText(w, r, opts)
	case FormatJSON:
		return renderJSON(w, r, opts)
	case FormatMarkdown:
		return renderMarkdown(w, r, opts)
	default:
		return fmt.Errorf("report: cannot render unknown format %d", format)
	}
}

// Check runs an audit and returns the process exit code per the report's Passed
// flag and the configured fail_on conditions (0 ok, 1 policy/check failure). A
// failure to build the report at all is an internal error (exit 4).
func Check(path string, cfg model.Config, opts Options, pipe Pipeline) (exitCode int, err error) {
	r, err := Audit(path, cfg, opts, pipe)
	if err != nil {
		return 4, err
	}
	if r.Passed {
		return 0, nil
	}
	return 1, nil
}

// ----- internal helpers the orchestration seam depends on -----

// readFileFn is the file reader Audit uses, indirected so tests can run the
// orchestration without touching disk. Production points it at os.ReadFile via the
// readFile wrapper below.
var readFile = func(path string) ([]byte, error) {
	return nil, errors.New("report: file reader not wired")
}

// classifier returns the file-type lookup honoring config overrides. It is wired by
// the integration layer; the default returns "no match" so enumerate (once real)
// still drives the run. WHY a package var: the report package must not import
// internal/filetype's package-global indexes through a closure it cannot inject in
// tests, and the integration agent swaps in filetype.Merge(cfg.FileTypeOverrides).
var classifierFn = func(cfg model.Config) func(string) (model.FileType, bool) {
	return func(string) (model.FileType, bool) { return model.FileType{}, false }
}

func classifier(cfg model.Config) func(string) (model.FileType, bool) {
	return classifierFn(cfg)
}

// ----- text renderer -----

func renderText(w io.Writer, r model.Report, opts RenderOptions) error {
	bw := &errWriter{w: w}

	bw.printf("license-tool audit report\n")
	bw.printf("%s\n\n", Disclaimer)
	bw.printf("root: %s\n", r.Root)
	bw.printf("license: %s   holder: %s   style: %s\n\n", orNone(r.Config.License), orNone(r.Config.Holder), r.Config.Style)

	// Findings summary first: a concise by-finding overview (coverage, license mix,
	// unknowns, copyleft, deps, policy) so a reader sees the verdict before the
	// detailed by-SPDX / by-category / by-file-type breakdowns below.
	renderFindings(bw, buildFindings(r))

	bw.printf("result: %s\n\n", passLabel(r.Passed))

	// The count rollups are always shown (cheap, and the core summary).
	renderCountSection(bw, "by SPDX id", r.LicenseCounts, "(no managed source files)", opts.SortByCount)
	renderCountSection(bw, "by category", r.CategoryCounts, "(none)", opts.SortByCount)
	renderCountSection(bw, "by file type", r.FileTypeCounts, "(none)", opts.SortByCount)

	// Source-file listing: flat by default, grouped under --group-by, omitted under
	// --summary (a bare --summary with no group-by shows only the rollups above).
	switch {
	case opts.GroupBy != GroupNone:
		renderGroupedFiles(bw, r, opts)
	case !opts.Summary:
		bw.printf("source files: %d\n", len(r.Files))
		for _, fr := range sortedFiles(r.Files) {
			bw.printf("  %s\n", fileLine(fr))
		}
		bw.printf("\n")
	}

	// Diffs and the per-dependency list are detail, suppressed in summary mode; the
	// findings block already reports dependency resolution counts.
	if !opts.Summary {
		renderTextDiffs(bw, r.Files)
		bw.printf("dependencies: %d\n", len(r.Dependencies))
		for _, dep := range sortedDeps(r.Dependencies) {
			bw.printf("  %s\n", depLine(dep))
		}
		bw.printf("\n")
	}

	renderTextViolations(bw, r)

	return bw.err
}

// renderTextViolations prints the attributable findings (condition + message) when
// available, falling back to the legacy repo-level tokens otherwise.
func renderTextViolations(bw *errWriter, r model.Report) {
	if len(r.ViolationDetails) > 0 {
		bw.printf("policy violations:\n")
		for _, v := range r.ViolationDetails {
			bw.printf("  [%s] %s\n", v.Condition, v.Message)
		}
		bw.printf("\n")
		return
	}
	if len(r.Violations) > 0 {
		bw.printf("policy violations:\n")
		for _, v := range r.Violations {
			bw.printf("  %s\n", v)
		}
		bw.printf("\n")
	}
}

// renderCountSection prints a "<title>:" header followed by the sorted counts, or
// emptyLabel when the map is empty.
func renderCountSection(bw *errWriter, title string, counts map[string]int, emptyLabel string, byCount bool) {
	bw.printf("%s:\n", title)
	if len(counts) == 0 {
		bw.printf("  %s\n", emptyLabel)
		bw.printf("\n")
		return
	}
	total := sumCounts(counts)
	for _, kv := range sortedCountsBy(counts, byCount) {
		bw.printf("  %-24s %4d  (%s)\n", kv.key, kv.count, percent(kv.count, total))
	}
	bw.printf("  %-24s %4d\n", "total", total)
	bw.printf("\n")
}

// sumCounts totals a count map.
func sumCounts(counts map[string]int) int {
	total := 0
	for _, v := range counts {
		total += v
	}
	return total
}

// percent formats n as a one-decimal percentage of total ("55.6%"); 0/0 is "0.0%".
func percent(n, total int) string {
	if total == 0 {
		return "0.0%"
	}
	return fmt.Sprintf("%.1f%%", float64(n)*100/float64(total))
}

// renderGroupedFiles prints the source files grouped under opts.GroupBy. Under
// --summary it prints per-group counts only; otherwise it nests the file lines. A
// trailing note reports skipped (uneditable) files, which are never grouped.
func renderGroupedFiles(bw *errWriter, r model.Report, opts RenderOptions) {
	groups, skipped := GroupFiles(r, opts.GroupBy)
	sortGroups(groups, opts.SortByCount)
	bw.printf("source files by %s:\n", opts.GroupBy)
	if len(groups) == 0 {
		bw.printf("  (no managed source files)\n")
	}
	for _, g := range groups {
		bw.printf("  %s (%d) [risk: %s]\n", g.Key, g.Count, g.Risk)
		if !opts.Summary {
			for _, fr := range g.Files {
				bw.printf("    %s\n", fileLine(fr))
			}
		}
	}
	if skipped > 0 {
		bw.printf("  (skipped: %d)\n", skipped)
	}
	bw.printf("\n")
}

func renderTextDiffs(bw *errWriter, files []model.FileResult) {
	diffFiles := make([]model.FileResult, 0)
	for _, fr := range sortedFiles(files) {
		if fr.Diff != "" {
			diffFiles = append(diffFiles, fr)
		}
	}
	if len(diffFiles) == 0 {
		return
	}

	bw.printf("pending diffs:\n")
	for _, fr := range diffFiles {
		bw.printf("%s\n", strings.TrimRight(fr.Diff, "\n"))
	}
	bw.printf("\n")
}

func fileLine(fr model.FileResult) string {
	switch {
	case fr.Err != "":
		return fmt.Sprintf("%s  [error: %s]", fr.Path, fr.Err)
	case fr.Skipped:
		return fmt.Sprintf("%s  [skipped: %s]", fr.Path, orNone(fr.SkipReason))
	case fr.Detected.Present && fr.Detected.SPDXID != "":
		return fmt.Sprintf("%s  [%s]", fr.Path, fr.Detected.SPDXID)
	default:
		return fmt.Sprintf("%s  [no managed header]", fr.Path)
	}
}

func depLine(dep model.DependencyLicense) string {
	coord := dep.Name
	if dep.Version != "" {
		coord = dep.Name + "@" + dep.Version
	}
	if dep.Resolution == model.ResolutionResolved {
		return fmt.Sprintf("%s/%s  [%s]", dep.Ecosystem, coord, orNone(dep.SPDXID))
	}
	return fmt.Sprintf("%s/%s  [unresolved: %s]", dep.Ecosystem, coord, orNone(dep.Reason))
}

// ----- markdown renderer -----

func renderMarkdown(w io.Writer, r model.Report, opts RenderOptions) error {
	bw := &errWriter{w: w}

	bw.printf("# license-tool audit report\n\n")
	bw.printf("> %s\n\n", Disclaimer)
	bw.printf("- **Root:** `%s`\n", r.Root)
	bw.printf("- **License:** `%s`\n", orNone(r.Config.License))
	bw.printf("- **Holder:** %s\n", orNone(r.Config.Holder))
	bw.printf("- **Style:** `%s`\n", r.Config.Style)
	bw.printf("- **Result:** %s\n\n", passLabel(r.Passed))

	renderMarkdownFindings(bw, buildFindings(r))

	renderMarkdownCountTable(bw, "By SPDX id", "SPDX id", r.LicenseCounts, opts.SortByCount)
	renderMarkdownCountTable(bw, "By category", "Category", r.CategoryCounts, opts.SortByCount)
	renderMarkdownCountTable(bw, "By file type", "File type", r.FileTypeCounts, opts.SortByCount)

	switch {
	case opts.GroupBy != GroupNone:
		renderMarkdownGroups(bw, r, opts)
	case !opts.Summary:
		bw.printf("## Source files (%d)\n\n", len(r.Files))
		bw.printf("| Path | File type | Status |\n| --- | --- | --- |\n")
		for _, fr := range sortedFiles(r.Files) {
			bw.printf("| `%s` | %s | %s |\n", fr.Path, orNone(fr.FileType), mdFileStatus(fr))
		}
		bw.printf("\n")
	}

	if !opts.Summary {
		bw.printf("## Dependencies (%d)\n\n", len(r.Dependencies))
		bw.printf("| Ecosystem | Name | Version | License | Resolution |\n| --- | --- | --- | --- | --- |\n")
		for _, dep := range sortedDeps(r.Dependencies) {
			bw.printf("| %s | `%s` | %s | `%s` | %s |\n",
				dep.Ecosystem, orNone(dep.Name), orNone(dep.Version), orNone(dep.SPDXID), dep.Resolution)
		}
		bw.printf("\n")
	}

	renderMarkdownViolations(bw, r)

	return bw.err
}

// renderMarkdownViolations renders an attributable findings table (condition, license,
// location, detail) when details are present, falling back to the legacy token list.
func renderMarkdownViolations(bw *errWriter, r model.Report) {
	if len(r.ViolationDetails) > 0 {
		bw.printf("## Policy violations\n\n")
		bw.printf("| Condition | License | Location | Detail |\n| --- | --- | --- | --- |\n")
		for _, v := range r.ViolationDetails {
			bw.printf("| %s | `%s` | %s | %s |\n", v.Condition, orNone(v.SPDXID), orNone(v.Path), v.Message)
		}
		bw.printf("\n")
		return
	}
	if len(r.Violations) > 0 {
		bw.printf("## Policy violations\n\n")
		for _, v := range r.Violations {
			bw.printf("- %s\n", v)
		}
		bw.printf("\n")
	}
}

// renderMarkdownFindings renders the at-a-glance summary (coverage, license mix,
// risk, copyleft, dependency resolution, policy) so Markdown reports carry the same
// overview the text renderer does.
func renderMarkdownFindings(bw *errWriter, f Findings) {
	bw.printf("## Findings\n\n")
	bw.printf("- **Source files:** %d (headered %d, missing %d)\n", f.SourceTotal, f.SourceHeadered, f.SourceMissing)
	bw.printf("- **License types:** %s\n", f.licenseSummary())
	bw.printf("- **Risk:** %s\n", f.riskSummary())
	bw.printf("- **Copyleft:** %s\n", f.copyleftSummary())
	bw.printf("- **Unknown/unrecognized:** %d\n", f.UnknownCount)
	if f.DepsScanned {
		bw.printf("- **Dependencies:** %d (resolved %d, unresolved %d)\n", f.DepsTotal, f.DepsResolved, f.DepsUnresolved)
	}
	bw.printf("- **Policy:** %s\n\n", f.policySummary())
}

// renderMarkdownCountTable renders one count rollup as a table with a percentage
// column and a total row.
func renderMarkdownCountTable(bw *errWriter, heading, keyHeader string, counts map[string]int, byCount bool) {
	bw.printf("## %s\n\n", heading)
	bw.printf("| %s | Files | %% |\n| --- | --- | --- |\n", keyHeader)
	total := sumCounts(counts)
	for _, kv := range sortedCountsBy(counts, byCount) {
		bw.printf("| `%s` | %d | %s |\n", kv.key, kv.count, percent(kv.count, total))
	}
	bw.printf("| **total** | **%d** | |\n\n", total)
}

// renderMarkdownGroups renders the grouped source-file view. Under --summary it emits
// a single per-group count table; otherwise a per-group heading with a file table.
func renderMarkdownGroups(bw *errWriter, r model.Report, opts RenderOptions) {
	groups, skipped := GroupFiles(r, opts.GroupBy)
	sortGroups(groups, opts.SortByCount)
	bw.printf("## Source files by %s\n\n", opts.GroupBy)

	if opts.Summary {
		bw.printf("| %s | Files | Risk |\n| --- | --- | --- |\n", titleCase(opts.GroupBy.String()))
		for _, g := range groups {
			bw.printf("| `%s` | %d | %s |\n", g.Key, g.Count, g.Risk)
		}
		bw.printf("\n")
	} else {
		for _, g := range groups {
			bw.printf("### `%s` (%d) — risk: %s\n\n", g.Key, g.Count, g.Risk)
			bw.printf("| Path | File type | Status |\n| --- | --- | --- |\n")
			for _, fr := range g.Files {
				bw.printf("| `%s` | %s | %s |\n", fr.Path, orNone(fr.FileType), mdFileStatus(fr))
			}
			bw.printf("\n")
		}
	}

	if skipped > 0 {
		bw.printf("_Skipped (uneditable): %d_\n\n", skipped)
	}
}

// titleCase upper-cases the first byte of an ASCII word for a table header.
func titleCase(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func mdFileStatus(fr model.FileResult) string {
	switch {
	case fr.Err != "":
		return "error: " + fr.Err
	case fr.Skipped:
		return "skipped: " + orNone(fr.SkipReason)
	case fr.Detected.Present && fr.Detected.SPDXID != "":
		return fr.Detected.SPDXID
	default:
		return "no managed header"
	}
}

// ----- json renderer -----

// jsonReport is the stable machine schema. WHY a dedicated DTO rather than marshaling
// model.Report directly: the model is the in-memory contract and may carry fields
// whose names or grouping do not suit a public JSON API. Pinning the schema here
// decouples wire compatibility from internal refactors, and lets us emit the
// disclaimer, a schema version, and sorted aggregates explicitly.
type jsonReport struct {
	Schema           string           `json:"schema"`
	Disclaimer       string           `json:"disclaimer"`
	Root             string           `json:"root"`
	Passed           bool             `json:"passed"`
	Config           jsonConfig       `json:"config"`
	Findings         jsonFindings     `json:"findings"`
	LicenseCounts    map[string]int   `json:"licenseCounts"`
	CategoryCounts   map[string]int   `json:"categoryCounts"`
	FileTypeCounts   map[string]int   `json:"fileTypeCounts"`
	Groups           []jsonGroup      `json:"groups,omitempty"`
	Files            []jsonFile       `json:"files"`
	Dependencies     []jsonDependency `json:"dependencies"`
	Violations       []string         `json:"violations"`
	ViolationDetails []jsonViolation  `json:"violationDetails"`
}

// jsonViolation is one attributable finding in the machine schema.
type jsonViolation struct {
	Condition string `json:"condition"`
	SPDXID    string `json:"spdxId,omitempty"`
	Path      string `json:"path,omitempty"`
	Message   string `json:"message"`
}

// jsonSummaryReport is the trimmed schema emitted under --summary: counts, optional
// groups, and violations, but no per-file or per-dependency detail. A dedicated
// struct keeps the default (full) jsonReport byte-identical to its historical shape.
type jsonSummaryReport struct {
	Schema           string          `json:"schema"`
	Disclaimer       string          `json:"disclaimer"`
	Root             string          `json:"root"`
	Passed           bool            `json:"passed"`
	Config           jsonConfig      `json:"config"`
	Findings         jsonFindings    `json:"findings"`
	LicenseCounts    map[string]int  `json:"licenseCounts"`
	CategoryCounts   map[string]int  `json:"categoryCounts"`
	FileTypeCounts   map[string]int  `json:"fileTypeCounts"`
	Groups           []jsonGroup     `json:"groups,omitempty"`
	Violations       []string        `json:"violations"`
	ViolationDetails []jsonViolation `json:"violationDetails"`
}

// jsonFindings is the machine form of the at-a-glance summary, including the explicit
// riskLevel/worstCategory so a model can branch on a value instead of parsing strings.
type jsonFindings struct {
	SourceTotal    int      `json:"sourceTotal"`
	SourceHeadered int      `json:"sourceHeadered"`
	SourceMissing  int      `json:"sourceMissing"`
	UnknownCount   int      `json:"unknownCount"`
	Copyleft       []string `json:"copyleft"`
	RiskLevel      string   `json:"riskLevel"`
	WorstCategory  string   `json:"worstCategory,omitempty"`
	DepsScanned    bool     `json:"depsScanned"`
	DepsTotal      int      `json:"depsTotal"`
	DepsResolved   int      `json:"depsResolved"`
	DepsUnresolved int      `json:"depsUnresolved"`
}

// jsonGroup is one grouped bucket in the machine schema. Files is omitted under
// --summary (counts only).
type jsonGroup struct {
	Key   string     `json:"key"`
	Count int        `json:"count"`
	Risk  string     `json:"risk"`
	Files []jsonFile `json:"files,omitempty"`
}

type jsonConfig struct {
	License string `json:"license"`
	Holder  string `json:"holder"`
	Style   string `json:"style"`
}

type jsonFile struct {
	Path       string   `json:"path"`
	FileType   string   `json:"fileType"`
	Skipped    bool     `json:"skipped"`
	SkipReason string   `json:"skipReason,omitempty"`
	HasHeader  bool     `json:"hasHeader"`
	SPDXID     string   `json:"spdxId,omitempty"`
	Holder     string   `json:"holder,omitempty"`
	Year       string   `json:"year,omitempty"`
	Action     string   `json:"action,omitempty"`
	Diff       string   `json:"diff,omitempty"`
	Violations []string `json:"violations,omitempty"`
	Error      string   `json:"error,omitempty"`
}

type jsonDependency struct {
	Ecosystem  string `json:"ecosystem"`
	Name       string `json:"name"`
	Version    string `json:"version,omitempty"`
	SPDXID     string `json:"spdxId,omitempty"`
	Resolution string `json:"resolution"`
	Reason     string `json:"reason,omitempty"`
}

func renderJSON(w io.Writer, r model.Report, opts RenderOptions) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	// Map keys are marshaled in sorted order by encoding/json, and every slice is
	// pre-sorted, so the emitted JSON is byte-stable for identical reports.

	var groups []jsonGroup
	if opts.GroupBy != GroupNone {
		groups = buildJSONGroups(r, opts)
	}
	details := toJSONViolations(r.ViolationDetails)
	findings := toJSONFindings(buildFindings(r))

	if opts.Summary {
		return enc.Encode(jsonSummaryReport{
			Schema:           "license-tool/report/v1",
			Disclaimer:       Disclaimer,
			Root:             r.Root,
			Passed:           r.Passed,
			Config:           jsonConfig{License: r.Config.License, Holder: r.Config.Holder, Style: r.Config.Style.String()},
			Findings:         findings,
			LicenseCounts:    nonNilCounts(r.LicenseCounts),
			CategoryCounts:   nonNilCounts(r.CategoryCounts),
			FileTypeCounts:   nonNilCounts(r.FileTypeCounts),
			Groups:           groups,
			Violations:       nonNilStrings(r.Violations),
			ViolationDetails: details,
		})
	}

	out := jsonReport{
		Schema:           "license-tool/report/v1",
		Disclaimer:       Disclaimer,
		Root:             r.Root,
		Passed:           r.Passed,
		Config:           jsonConfig{License: r.Config.License, Holder: r.Config.Holder, Style: r.Config.Style.String()},
		Findings:         findings,
		LicenseCounts:    nonNilCounts(r.LicenseCounts),
		CategoryCounts:   nonNilCounts(r.CategoryCounts),
		FileTypeCounts:   nonNilCounts(r.FileTypeCounts),
		Groups:           groups,
		Violations:       nonNilStrings(r.Violations),
		ViolationDetails: details,
	}

	out.Files = make([]jsonFile, 0, len(r.Files))
	for _, fr := range sortedFiles(r.Files) {
		out.Files = append(out.Files, toJSONFile(fr))
	}

	out.Dependencies = make([]jsonDependency, 0, len(r.Dependencies))
	for _, dep := range sortedDeps(r.Dependencies) {
		out.Dependencies = append(out.Dependencies, toJSONDependency(dep))
	}

	return enc.Encode(out)
}

// buildJSONGroups builds the machine grouping for opts.GroupBy. File detail is
// included unless --summary asks for counts only.
func buildJSONGroups(r model.Report, opts RenderOptions) []jsonGroup {
	groups, _ := GroupFiles(r, opts.GroupBy)
	sortGroups(groups, opts.SortByCount)
	out := make([]jsonGroup, 0, len(groups))
	for _, g := range groups {
		jg := jsonGroup{Key: g.Key, Count: g.Count, Risk: g.Risk}
		if !opts.Summary {
			jg.Files = make([]jsonFile, 0, len(g.Files))
			for _, fr := range g.Files {
				jg.Files = append(jg.Files, toJSONFile(fr))
			}
		}
		out = append(out, jg)
	}
	return out
}

func toJSONFile(fr model.FileResult) jsonFile {
	return jsonFile{
		Path:       fr.Path,
		FileType:   fr.FileType,
		Skipped:    fr.Skipped,
		SkipReason: fr.SkipReason,
		HasHeader:  fr.Detected.Present,
		SPDXID:     fr.Detected.SPDXID,
		Holder:     fr.Detected.Holder,
		Year:       fr.Detected.Year,
		Action:     fr.Action,
		Diff:       fr.Diff,
		Violations: nonNilStrings(fr.Violations),
		Error:      fr.Err,
	}
}

func toJSONFindings(f Findings) jsonFindings {
	return jsonFindings{
		SourceTotal:    f.SourceTotal,
		SourceHeadered: f.SourceHeadered,
		SourceMissing:  f.SourceMissing,
		UnknownCount:   f.UnknownCount,
		Copyleft:       nonNilStrings(f.Copyleft),
		RiskLevel:      f.RiskLevel,
		WorstCategory:  f.WorstCategory,
		DepsScanned:    f.DepsScanned,
		DepsTotal:      f.DepsTotal,
		DepsResolved:   f.DepsResolved,
		DepsUnresolved: f.DepsUnresolved,
	}
}

func toJSONViolations(details []model.ViolationDetail) []jsonViolation {
	out := make([]jsonViolation, 0, len(details))
	for _, v := range details {
		out = append(out, jsonViolation{
			Condition: v.Condition.String(),
			SPDXID:    v.SPDXID,
			Path:      v.Path,
			Message:   v.Message,
		})
	}
	return out
}

func toJSONDependency(dep model.DependencyLicense) jsonDependency {
	return jsonDependency{
		Ecosystem:  dep.Ecosystem,
		Name:       dep.Name,
		Version:    dep.Version,
		SPDXID:     dep.SPDXID,
		Resolution: dep.Resolution.String(),
		Reason:     dep.Reason,
	}
}

// ----- ordering + formatting helpers -----

type countKV struct {
	key   string
	count int
}

// sortedCounts returns the map entries ordered by key, so every renderer walks the
// aggregates in one canonical order.
func sortedCounts(m map[string]int) []countKV {
	out := make([]countKV, 0, len(m))
	for k, v := range m {
		out = append(out, countKV{key: k, count: v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key < out[j].key })
	return out
}

// sortedCountsBy returns the entries key-sorted (default) or by descending count with
// key ties broken alphabetically when byCount is set.
func sortedCountsBy(m map[string]int, byCount bool) []countKV {
	out := sortedCounts(m)
	if byCount {
		sort.SliceStable(out, func(i, j int) bool { return out[i].count > out[j].count })
	}
	return out
}

// sortedFiles returns a copy of files ordered by path, the stable key for the
// per-file views. Ties on path (which should not occur for a real tree) fall back
// to FileType then detected id to stay total.
func sortedFiles(files []model.FileResult) []model.FileResult {
	out := append([]model.FileResult(nil), files...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		if out[i].FileType != out[j].FileType {
			return out[i].FileType < out[j].FileType
		}
		return out[i].Detected.SPDXID < out[j].Detected.SPDXID
	})
	return out
}

// sortedDeps returns a copy of deps ordered by (ecosystem, name, version), the
// natural total order for a dependency listing.
func sortedDeps(deps []model.DependencyLicense) []model.DependencyLicense {
	out := append([]model.DependencyLicense(nil), deps...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Ecosystem != out[j].Ecosystem {
			return out[i].Ecosystem < out[j].Ecosystem
		}
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Version < out[j].Version
	})
	return out
}

func passLabel(passed bool) string {
	if passed {
		return "PASS"
	}
	return "FAIL"
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

// nonNilCounts returns a non-nil copy so JSON emits {} rather than null for an empty
// aggregate, keeping the schema stable for machine consumers.
func nonNilCounts(m map[string]int) map[string]int {
	if m == nil {
		return map[string]int{}
	}
	return m
}

// nonNilStrings returns a non-nil copy so JSON emits [] rather than null for an
// empty list.
func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// errWriter accumulates the first write error so the renderers can stay linear
// (printf, printf, ...) without an error check after every line; Render returns the
// captured error at the end.
type errWriter struct {
	w   io.Writer
	err error
}

func (e *errWriter) printf(format string, args ...any) {
	if e.err != nil {
		return
	}
	_, e.err = fmt.Fprintf(e.w, format, args...)
}
