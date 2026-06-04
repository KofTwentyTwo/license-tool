// Package model defines the shared types and interfaces that flow between every
// other package in license-tool. It is deliberately dependency-free (stdlib only)
// so that config, enumerate, detect, render, resolve, policy, report, and applier
// can all depend on it without import cycles.
//
// WHY this package is frozen: downstream feature packages are built in parallel
// against these types. Changing a field or signature here ripples into every
// consumer, so the contract is fixed up front and treated as load-bearing.
package model

// License is a single SPDX-identified license, as vendored from the SPDX
// license-list-data snapshot and classified into a Category. Text is the full
// license body (for the top-level LICENSE / LICENSES/<id>.txt files); StandardHeader
// is the per-file notice block to embed in source headers when the style asks for it.
type License struct {
	// SPDXID is the canonical SPDX identifier, e.g. "AGPL-3.0-or-later".
	SPDXID string

	// Name is the human-readable license name, e.g. "GNU Affero General Public License v3.0 or later".
	Name string

	// Category is the copyleft/permissive classification used by policy checks.
	Category Category

	// Text is the full, verbatim license text from the SPDX snapshot.
	Text string

	// StandardHeader is the per-file notice block (with placeholders or canonical
	// wording) emitted when style includes a notice. Empty when the license defines none.
	StandardHeader string
}

// Category classifies a license by the obligations it imposes, driving policy
// heterogeneity checks and the curated incompatibility table.
type Category int

const (
	// CategoryUnknown is the zero value: classification could not be determined.
	CategoryUnknown Category = iota
	// CategoryPermissive covers MIT/Apache/BSD/ISC/Unlicense/CC0-style licenses.
	CategoryPermissive
	// CategoryWeakCopyleft covers file/library-scoped copyleft (LGPL, MPL).
	CategoryWeakCopyleft
	// CategoryStrongCopyleft covers project-scoped copyleft (GPL, AGPL).
	CategoryStrongCopyleft
	// CategoryNetworkCopyleft covers network-use copyleft (AGPL); AGPL is also strong.
	CategoryNetworkCopyleft
	// CategoryProprietary covers all-rights-reserved / non-open licenses.
	CategoryProprietary
)

// String renders the Category as the lowercase, hyphenated token used in config
// and reports (e.g. "weak-copyleft").
func (c Category) String() string {
	switch c {
	case CategoryPermissive:
		return "permissive"
	case CategoryWeakCopyleft:
		return "weak-copyleft"
	case CategoryStrongCopyleft:
		return "strong-copyleft"
	case CategoryNetworkCopyleft:
		return "network-copyleft"
	case CategoryProprietary:
		return "proprietary"
	default:
		return "unknown"
	}
}

// HeaderStyle selects which parts of the canonical header are emitted into a file.
type HeaderStyle int

const (
	// StyleReuse emits only REUSE tags (SPDX-FileCopyrightText + SPDX-License-Identifier).
	StyleReuse HeaderStyle = iota
	// StyleNotice emits only the full standard notice block (no REUSE tags).
	StyleNotice
	// StyleReusePlusNotice emits REUSE tags followed by the full notice block (default).
	StyleReusePlusNotice
)

// String renders the HeaderStyle as the config token (e.g. "reuse+notice").
func (s HeaderStyle) String() string {
	switch s {
	case StyleReuse:
		return "reuse"
	case StyleNotice:
		return "notice"
	case StyleReusePlusNotice:
		return "reuse+notice"
	default:
		return "reuse+notice"
	}
}

// YearKind enumerates how the copyright year in a header is determined.
type YearKind int

const (
	// YearCurrent uses the current calendar year.
	YearCurrent YearKind = iota
	// YearExplicit uses a single fixed year (Start).
	YearExplicit
	// YearRange uses an explicit Start-End range.
	YearRange
	// YearGit derives a first-commit-year-to-current range from git history.
	YearGit
)

// YearSpec captures the year policy. Kind selects the strategy; Start/End hold the
// explicit values for YearExplicit (Start only) and YearRange (Start and End).
type YearSpec struct {
	Kind  YearKind
	Start int
	End   int
}

// YearResolver resolves a YearSpec to the concrete year string rendered into a
// header (e.g. "2021-2026"). The repoPath lets the git strategy inspect history;
// nowYear is injected so callers (and tests) control the notion of "current".
//
// Implemented by internal/render (or internal/gitutil); declared here so the
// interface is part of the frozen contract.
type YearResolver interface {
	Resolve(repoPath string, nowYear int) (string, error)
}

// CommentStyle describes how to wrap header text into a file's comments. A file
// type is either block-commented (Block true, using Open/Close delimiters) or
// line-commented (Block false, using LinePrefix on every line).
type CommentStyle struct {
	// Block selects block comments (Open/Close) over line comments (LinePrefix).
	Block bool
	// Open is the block-comment opener, e.g. "/*" (block styles only).
	Open string
	// Close is the block-comment closer, e.g. "*/" (block styles only).
	Close string
	// LinePrefix is prepended to each header line, e.g. "// " or "# " (line styles only).
	LinePrefix string
}

// PreserveKind enumerates the leading constructs that must remain at the very top
// of a file; the license header is inserted after any present preserve-first
// constructs (and before constructs like a Go/Java package declaration).
type PreserveKind int

const (
	// PreserveShebang is a "#!" interpreter line (shell, python, etc.).
	PreserveShebang PreserveKind = iota
	// PreserveXMLDecl is an "<?xml ... ?>" declaration.
	PreserveXMLDecl
	// PreservePHPOpen is a "<?php" opening tag.
	PreservePHPOpen
	// PreserveBOM is a UTF-8 byte-order mark.
	PreserveBOM
	// PreserveCodingPragma is an encoding pragma, e.g. "# -*- coding: utf-8 -*-".
	PreserveCodingPragma
	// PreservePackageDecl is a package/module declaration (Go, Java, Kotlin) that
	// the header must precede rather than follow.
	PreservePackageDecl
	// PreserveGoBuildConstraint is a Go "//go:build" or "// +build" constraint
	// block that must remain in the file's leading comment group.
	PreserveGoBuildConstraint
	// PreserveCSSCharset is a CSS "@charset" rule that must lead the stylesheet.
	PreserveCSSCharset
	// PreserveDoctype is an HTML/XML "<!DOCTYPE ...>" declaration.
	PreserveDoctype
)

// PreserveRule is a single preserve-first directive in a FileType's ordered list.
// Before marks constructs the header is placed BEFORE (e.g. package decl); when
// false the header is placed AFTER the construct (e.g. shebang, xml-decl).
type PreserveRule struct {
	// Kind is the construct this rule matches.
	Kind PreserveKind
	// Before is true when the header must precede this construct (package decl),
	// false when the header follows it (shebang, BOM, xml-decl, php-open, pragma).
	Before bool
}

// FileType is one entry in the data-driven comment-syntax table. Skip marks
// formats that cannot carry a comment (e.g. JSON): they are reported, never edited.
type FileType struct {
	// Name is the human label, e.g. "Java", "YAML".
	Name string
	// Extensions are matched against the file's extension, e.g. ".java" (lowercase, with dot).
	Extensions []string
	// Filenames are matched against the exact base name, e.g. "Dockerfile".
	Filenames []string
	// CommentStyle describes how to wrap the header for this type.
	CommentStyle CommentStyle
	// PreserveFirst is the ordered list of preserve-first rules for this type.
	PreserveFirst []PreserveRule
	// Skip marks an uncommentable type: detection/report only, never written.
	Skip bool
}

// DetectedHeader is the result of scanning a single file's leading region for a
// managed license header. Present is false when no managed header was found
// (StartByte/EndByte are then meaningless). ViaSentinel records that detection
// matched our own sentinel rather than a fingerprint, which raises confidence
// for safe replacement.
type DetectedHeader struct {
	// Present is true when a managed/replaceable license header was identified.
	Present bool
	// SPDXID is the license id extracted from the header, if any.
	SPDXID string
	// Holder is the copyright holder extracted from the header, if any.
	Holder string
	// Year is the copyright year/range extracted from the header, if any.
	Year string
	// StartByte is the byte offset (inclusive) where the header block begins.
	StartByte int
	// EndByte is the byte offset (exclusive) where the header block ends.
	EndByte int
	// ViaSentinel is true when detection matched our sentinel (highest confidence).
	ViaSentinel bool
}

// Config is the fully-resolved configuration after layering flags > repo config >
// user/global config > built-in defaults. It is both the apply input and the
// check expectation for a repo.
type Config struct {
	// License is the target SPDX id, e.g. "AGPL-3.0-or-later".
	License string
	// Holder is the copyright holder text.
	Holder string
	// Year is the resolved year policy.
	Year YearSpec
	// Style selects which header parts are emitted.
	Style HeaderStyle
	// ManageLicenseFile enables writing top-level LICENSE + LICENSES/<id>.txt.
	ManageLicenseFile bool
	// Includes are glob patterns that restrict source-file processing when non-empty.
	Includes []string
	// Excludes are gitignore-style patterns added on top of .gitignore.
	Excludes []string
	// Policy drives audit classification and check exit codes.
	Policy Policy
	// FileTypeOverrides are user additions/overrides to the built-in file-type table,
	// keyed by extension (e.g. ".myext").
	FileTypeOverrides map[string]FileType
}

// Policy is the config-defined license policy enforced by check and surfaced by audit.
type Policy struct {
	// Required is the SPDX id every managed file is expected to carry, if set.
	Required string
	// Allow is the allowlist of acceptable SPDX ids; empty means "no allowlist".
	Allow []string
	// Deny is the denylist of forbidden SPDX ids.
	Deny []string
	// FailOn lists the conditions that cause check to exit non-zero.
	FailOn []FailCondition
}

// FailCondition enumerates the conditions check can gate CI on.
type FailCondition int

const (
	// FailOnMissingHeader fails when a managed file lacks a license header.
	FailOnMissingHeader FailCondition = iota
	// FailOnUnknownLicense fails when a detected license cannot be classified.
	FailOnUnknownLicense
	// FailOnPolicyViolation fails on an allow/deny/required violation.
	FailOnPolicyViolation
	// FailOnUnresolvedDependency fails when a dependency license cannot be resolved.
	FailOnUnresolvedDependency
)

// String renders the FailCondition as its config token (e.g. "missing-header").
func (f FailCondition) String() string {
	switch f {
	case FailOnMissingHeader:
		return "missing-header"
	case FailOnUnknownLicense:
		return "unknown-license"
	case FailOnPolicyViolation:
		return "policy-violation"
	case FailOnUnresolvedDependency:
		return "unresolved-dependency"
	default:
		return "unknown"
	}
}

// DependencyResolution is the outcome of resolving a single dependency's license.
type DependencyResolution int

const (
	// ResolutionUnresolved is the zero value: the license could not be determined
	// and was not guessed.
	ResolutionUnresolved DependencyResolution = iota
	// ResolutionResolved means a license was determined from metadata or a tool.
	ResolutionResolved
)

// String renders the DependencyResolution as a report token.
func (r DependencyResolution) String() string {
	switch r {
	case ResolutionResolved:
		return "resolved"
	default:
		return "unresolved"
	}
}

// DependencyLicense is one third-party dependency and its resolved (or unresolved)
// license, produced by an ecosystem Resolver.
type DependencyLicense struct {
	// Ecosystem is the package ecosystem, e.g. "maven", "npm", "gradle".
	Ecosystem string
	// Name is the dependency's package coordinate/name.
	Name string
	// Version is the dependency's version, if known.
	Version string
	// SPDXID is the resolved SPDX id; empty when unresolved.
	SPDXID string
	// Resolution records whether the license was resolved or left unresolved.
	Resolution DependencyResolution
	// Reason explains an unresolved result (e.g. "no metadata on disk; tool not run").
	Reason string
}

// ResolveOptions tunes a Resolver run, selecting the resolution tier and tracing.
type ResolveOptions struct {
	// AllowToolShellOut permits shelling out to the ecosystem's native tool when
	// on-disk metadata is insufficient. When false, resolution is on-disk only.
	AllowToolShellOut bool
	// Verbose enables diagnostic logging during resolution.
	Verbose bool
}

// Resolver detects and resolves dependency licenses for one ecosystem. Detect is
// a cheap manifest-presence check; Resolve does the work. Implementations live in
// internal/resolve; the interface is part of the frozen contract so audit can
// iterate over a slice of Resolvers uniformly.
type Resolver interface {
	// Ecosystem returns the resolver's ecosystem label, e.g. "maven".
	Ecosystem() string
	// Detect reports whether this ecosystem is present at path (manifest check).
	Detect(path string) bool
	// Resolve returns the dependency licenses found at path under the given options.
	Resolve(path string, opts ResolveOptions) ([]DependencyLicense, error)
}

// FileResult is the per-file outcome of an audit or apply pass over one source file.
type FileResult struct {
	// Path is the file path relative to the scan root.
	Path string
	// FileType is the matched file-type name, e.g. "Java"; empty when unmatched.
	FileType string
	// Skipped is true when the file was not processed (binary, uncommentable, unmatched).
	Skipped bool
	// SkipReason explains a skip (e.g. "uncommentable", "binary", "unknown type").
	SkipReason string
	// Detected is the header detection result for the file.
	Detected DetectedHeader
	// Action is what apply did or would do: "none", "insert", "replace", "skip".
	Action string
	// Diff is the unified diff for an apply dry-run; empty otherwise.
	Diff string
	// Violations lists policy violation tokens attached to this file.
	Violations []string
	// Err holds a per-file error message; empty on success.
	Err string
}

// Report is the top-level audit/apply result model rendered to text/JSON/Markdown.
type Report struct {
	// Root is the absolute path that was scanned.
	Root string
	// Config is the effective configuration the run used.
	Config Config
	// Files holds the per-file results.
	Files []FileResult
	// Dependencies holds resolved/unresolved dependency licenses (audit only).
	Dependencies []DependencyLicense
	// LicenseCounts maps SPDX id to the number of source files carrying it.
	LicenseCounts map[string]int
	// CategoryCounts maps category token to source-file count.
	CategoryCounts map[string]int
	// FileTypeCounts maps file-type name to count.
	FileTypeCounts map[string]int
	// Violations lists repo-level policy violation tokens.
	Violations []string
	// Passed is true when no fail_on condition tripped (drives check exit code).
	Passed bool
}
