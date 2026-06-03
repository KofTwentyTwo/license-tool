// Package config resolves the effective configuration by layering, in precedence
// order: command-line flags > repo .license-tool.yaml > user/global config
// ($XDG_CONFIG_HOME/license-tool/config.yaml) > built-in defaults.
//
// Missing required fields (license, holder) are prompted for on a TTY and are a
// hard error (no hang) in CI / non-TTY environments.
package config

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/KofTwentyTwo/license-tool/internal/filetype"
	"github.com/KofTwentyTwo/license-tool/internal/model"
	"github.com/KofTwentyTwo/license-tool/internal/spdx"
)

// repoConfigName is the committed, per-repo config file discovered at the scan
// root. It doubles as the apply input and the check expectation for a repo.
const repoConfigName = ".license-tool.yaml"

// userConfigRel is the user/global config path relative to $XDG_CONFIG_HOME.
const userConfigRel = "license-tool/config.yaml"

// ErrMissingRequired is returned when a required field (license or holder) is
// absent after all layers are merged and prompting is not permitted (CI /
// non-TTY). WHY a sentinel: callers map this to usage exit code 2 and must be
// able to distinguish it from an I/O or parse failure without string matching.
var ErrMissingRequired = errors.New("config: required field missing (license and holder are required); set them in .license-tool.yaml, $XDG_CONFIG_HOME/license-tool/config.yaml, or via flags")

// promptReader/promptWriter are package-level seams so tests can drive the
// interactive prompt without a real terminal. They default to stdin/stderr.
// WHY stderr for prompts: prompt text must not contaminate machine-readable
// stdout (JSON reports), so questions and echoes go to stderr.
var (
	promptReader io.Reader = os.Stdin
	promptWriter io.Writer = os.Stderr
)

// Flags carries the raw, parsed command-line values that override config-file
// layers. Pointer/zero semantics: an empty string or nil slice means "unset", so
// the layering logic can tell "not provided" from "explicitly set".
type Flags struct {
	// ConfigPath is an explicit --config file, overriding discovery. Empty = discover.
	ConfigPath string
	// License is --license; empty means unset.
	License string
	// Holder is --holder; empty means unset.
	Holder string
	// Year is the raw --year spec ("current"|"YYYY"|"YYYY-YYYY"|"git"); empty = unset.
	Year string
	// Style is the raw --style token ("reuse"|"notice"|"reuse+notice"); empty = unset.
	Style string
	// Include / Exclude are repeatable --include / --exclude globs.
	Include []string
	Exclude []string
	// NoGitignore disables .gitignore inheritance when true (--no-gitignore).
	NoGitignore bool
}

// Options tunes config resolution, chiefly whether interactive prompting is allowed.
type Options struct {
	// Interactive permits TTY prompts for missing required fields. When false
	// (CI / non-TTY), missing required fields are a hard error.
	Interactive bool
	// RequireApply marks a write operation (apply/license/init) that needs the
	// license and holder identity fields. Read-only audit/check leave it false and
	// tolerate an unconfigured license.
	RequireApply bool
}

// Defaults returns the built-in default configuration (the lowest precedence
// layer): AGPL-3.0-or-later profile, year=git, style=reuse+notice, manage license
// file on, and the default fail_on set.
//
// WHY License/Holder stay empty here: they are required identity fields with no
// safe default; leaving them empty lets the layering distinguish "not configured"
// (which must be filled by a higher layer, a prompt, or hard-error) from a value
// a user actively chose. The AGPL profile is expressed only through the policy and
// style defaults, never by silently assuming a license the user did not declare.
func Defaults() model.Config {
	return model.Config{
		License:           "",
		Holder:            "",
		Year:              model.YearSpec{Kind: model.YearGit},
		Style:             model.StyleReusePlusNotice,
		ManageLicenseFile: true,
		Excludes:          nil,
		Policy: model.Policy{
			FailOn: []model.FailCondition{
				model.FailOnMissingHeader,
				model.FailOnUnknownLicense,
				model.FailOnPolicyViolation,
			},
		},
		FileTypeOverrides: nil,
	}
}

// Resolve layers flags over the repo config, user/global config, and built-in
// defaults for the repo rooted at path, returning the effective model.Config.
//
// Precedence (high to low): flags > repo .license-tool.yaml > user/global config
// > built-in defaults. Lower layers seed the base; each higher layer overrides
// only the fields it actually sets. After merging, required fields (license,
// holder) are prompted for on a TTY (opts.Interactive) or hard-error otherwise.
// The target license is validated against the vendored SPDX list.
func Resolve(path string, flags Flags, opts Options) (model.Config, error) {
	cfg := Defaults()

	// User/global layer (lowest file layer): absent file is not an error.
	if p := userConfigPath(); p != "" {
		if fileExists(p) {
			fs, err := loadSchema(p)
			if err != nil {
				return model.Config{}, err
			}
			if err := mergeSchema(&cfg, fs); err != nil {
				return model.Config{}, fmt.Errorf("config: %s: %w", p, err)
			}
		}
	}

	// Repo layer: explicit --config wins; otherwise discover .license-tool.yaml at
	// the scan root. An explicit --config that is missing IS an error (the user
	// named a file that does not exist); a missing discovered file is not.
	repoPath, explicit := repoConfigPath(path, flags.ConfigPath)
	if repoPath != "" {
		if explicit && !fileExists(repoPath) {
			return model.Config{}, fmt.Errorf("config: --config file not found: %s", repoPath)
		}
		if fileExists(repoPath) {
			fs, err := loadSchema(repoPath)
			if err != nil {
				return model.Config{}, err
			}
			if err := mergeSchema(&cfg, fs); err != nil {
				return model.Config{}, fmt.Errorf("config: %s: %w", repoPath, err)
			}
		}
	}

	// Flag layer (highest): only non-empty flags override.
	if err := mergeFlags(&cfg, flags); err != nil {
		return model.Config{}, err
	}

	// Required identity fields (license, holder) are needed only by write operations
	// (apply/license/init). Read-only audit/check tolerate their absence: an empty
	// license simply means "no expected identity to enforce".
	if opts.RequireApply {
		if err := fillRequired(&cfg, opts.Interactive); err != nil {
			return model.Config{}, err
		}
	}

	// Validate any resolved license against the vendored SPDX index. An empty license
	// is valid on a read-only run (nothing to validate). WHY here and not per-layer:
	// only the final, merged value matters.
	if cfg.License != "" && !spdx.Validate(cfg.License) {
		return model.Config{}, fmt.Errorf("config: %q is not a recognized SPDX license identifier", cfg.License)
	}

	return cfg, nil
}

// LoadFile parses a single .license-tool.yaml (or user/global config) file into a
// model.Config without layering. It seeds from Defaults so unset keys carry the
// built-in defaults, mirroring how a single layer behaves inside Resolve. Used by
// Resolve indirectly and by tests directly.
func LoadFile(filename string) (model.Config, error) {
	fs, err := loadSchema(filename)
	if err != nil {
		return model.Config{}, err
	}
	cfg := Defaults()
	if err := mergeSchema(&cfg, fs); err != nil {
		return model.Config{}, fmt.Errorf("config: %s: %w", filename, err)
	}
	return cfg, nil
}

// RenderFile serializes cfg into the on-disk .license-tool.yaml byte form. It is
// pure (no I/O) so callers can preview the scaffold or test the exact bytes without
// touching the filesystem. WHY it mirrors fileSchema rather than marshaling the
// model directly: the YAML keys, the *bool nullability of manage_license_file, and
// the string tokenization of enums (style, year, fail_on) are the documented file
// contract, and round-tripping through fileSchema keeps init's output identical to
// what mergeSchema reads back.
func RenderFile(cfg model.Config) ([]byte, error) {
	fs := fileSchema{
		License:           cfg.License,
		Holder:            cfg.Holder,
		Year:              yearSpecRaw(cfg.Year),
		Style:             cfg.Style.String(),
		ManageLicenseFile: &cfg.ManageLicenseFile,
		Exclude:           cfg.Excludes,
	}
	// Only emit a policy block when the config actually carries policy intent, so a
	// freshly scaffolded file is not cluttered with an empty policy the user did not set.
	if cfg.Policy.Required != "" || len(cfg.Policy.Allow) > 0 || len(cfg.Policy.Deny) > 0 || len(cfg.Policy.FailOn) > 0 {
		fs.Policy = policySchema{
			Required: cfg.Policy.Required,
			Allow:    cfg.Policy.Allow,
			Deny:     cfg.Policy.Deny,
			FailOn:   failConditionsToTokens(cfg.Policy.FailOn),
		}
	}
	return yaml.Marshal(fs)
}

// renderFile is a package-level seam over RenderFile so WriteFile's render-error
// branch is reachable in tests. RenderFile cannot fail for any real model.Config
// (yaml.Marshal of the fixed fileSchema is total), so the only way to exercise the
// error path is to inject a failing renderer here. Production always points at the
// real RenderFile, leaving runtime behavior unchanged.
var renderFile = RenderFile

// WriteFile renders cfg and writes it to <path>/.license-tool.yaml, returning the
// written target path. WHY the force guard: init must never silently clobber a
// committed config, so an existing target is a hard error unless the caller opted in
// with force (the --force flag). The 0o644 mode matches a normal committed text file.
func WriteFile(path string, cfg model.Config, force bool) (string, error) {
	target := filepath.Join(path, repoConfigName)
	if !force && fileExists(target) {
		return "", fmt.Errorf("config: %s already exists (use --force)", target)
	}
	data, err := renderFile(cfg)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(target, data, 0o644); err != nil {
		return "", fmt.Errorf("config: write %s: %w", target, err)
	}
	return target, nil
}

// yearSpecRaw renders a YearSpec back to its config token, the inverse of
// ParseYearSpec. WHY YearGit is the default arm: it is the built-in default Kind, so
// any unrecognized/zero-value spec serializes to the safe "git" token rather than an
// empty string that would round-trip back to a parse error.
func yearSpecRaw(y model.YearSpec) string {
	switch y.Kind {
	case model.YearCurrent:
		return "current"
	case model.YearExplicit:
		return strconv.Itoa(y.Start)
	case model.YearRange:
		return strconv.Itoa(y.Start) + "-" + strconv.Itoa(y.End)
	default: // model.YearGit and any zero value.
		return "git"
	}
}

// failConditionsToTokens renders fail conditions back to their config tokens, the
// inverse of parseFailConditions, so a scaffolded fail_on list round-trips exactly.
func failConditionsToTokens(fcs []model.FailCondition) []string {
	out := make([]string, 0, len(fcs))
	for _, fc := range fcs {
		out = append(out, fc.String())
	}
	return out
}

// loadSchema reads and decodes a YAML config file into the on-disk schema.
func loadSchema(filename string) (fileSchema, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return fileSchema{}, fmt.Errorf("config: read %s: %w", filename, err)
	}
	fs, err := decodeYAML(data)
	if err != nil {
		return fileSchema{}, fmt.Errorf("config: parse %s: %w", filename, err)
	}
	return fs, nil
}

// fileSchema is the on-disk shape of a .license-tool.yaml file, decoded with
// yaml.v3 before being merged into a model.Config. It is the authoritative mapping
// between the documented YAML keys and the in-memory config.
type fileSchema struct {
	License           string                          `yaml:"license"`
	Holder            string                          `yaml:"holder"`
	Year              string                          `yaml:"year"`
	Style             string                          `yaml:"style"`
	ManageLicenseFile *bool                           `yaml:"manage_license_file"`
	Exclude           []string                        `yaml:"exclude"`
	Policy            policySchema                    `yaml:"policy"`
	FileTypes         map[string]fileTypeOverrideYAML `yaml:"file_types"`
}

type policySchema struct {
	Required string   `yaml:"required"`
	Allow    []string `yaml:"allow"`
	Deny     []string `yaml:"deny"`
	FailOn   []string `yaml:"fail_on"`
}

type fileTypeOverrideYAML struct {
	Style string `yaml:"style"`
	Line  string `yaml:"line"`
	Open  string `yaml:"open"`
	Close string `yaml:"close"`
}

// decodeYAML parses raw .license-tool.yaml bytes into the file schema. KnownFields
// is enabled so a typo'd key (e.g. "licence:") is a hard error rather than a
// silently-ignored field that leaves the user wondering why their setting had no
// effect.
func decodeYAML(data []byte) (fileSchema, error) {
	var fs fileSchema
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&fs); err != nil {
		// An empty document is valid (all-defaults config); io.EOF means no document.
		if errors.Is(err, io.EOF) {
			return fileSchema{}, nil
		}
		return fileSchema{}, err
	}
	return fs, nil
}

// mergeSchema layers a decoded file schema onto cfg, overriding only the keys the
// file actually set. WHY presence-by-emptiness for scalars: YAML has no "unset"
// for absent string keys, so an empty string is treated as "not provided" and the
// lower layer's value is preserved; manage_license_file uses a *bool so explicit
// false is distinguishable from absent.
func mergeSchema(cfg *model.Config, fs fileSchema) error {
	if fs.License != "" {
		cfg.License = fs.License
	}
	if fs.Holder != "" {
		cfg.Holder = fs.Holder
	}
	if fs.Year != "" {
		ys, err := ParseYearSpec(fs.Year)
		if err != nil {
			return err
		}
		cfg.Year = ys
	}
	if fs.Style != "" {
		st, err := ParseStyle(fs.Style)
		if err != nil {
			return err
		}
		cfg.Style = st
	}
	if fs.ManageLicenseFile != nil {
		cfg.ManageLicenseFile = *fs.ManageLicenseFile
	}
	if len(fs.Exclude) > 0 {
		// Excludes accumulate: a layer's excludes add to, rather than replace, the
		// lower layers' so a repo cannot accidentally lose user/global exclusions.
		cfg.Excludes = append(cfg.Excludes, fs.Exclude...)
	}
	if err := mergePolicy(&cfg.Policy, fs.Policy); err != nil {
		return err
	}
	if len(fs.FileTypes) > 0 {
		if cfg.FileTypeOverrides == nil {
			cfg.FileTypeOverrides = make(map[string]model.FileType)
		}
		for ext, ov := range fs.FileTypes {
			ft, err := fileTypeFromYAML(ext, ov)
			if err != nil {
				return err
			}
			cfg.FileTypeOverrides[normalizeExt(ext)] = ft
		}
	}
	return nil
}

// mergePolicy layers a policy schema onto the running policy. Each sub-field is
// overridden only when the file provides it.
func mergePolicy(p *model.Policy, ps policySchema) error {
	if ps.Required != "" {
		p.Required = ps.Required
	}
	if len(ps.Allow) > 0 {
		p.Allow = append([]string(nil), ps.Allow...)
	}
	if len(ps.Deny) > 0 {
		p.Deny = append([]string(nil), ps.Deny...)
	}
	if len(ps.FailOn) > 0 {
		fc, err := parseFailConditions(ps.FailOn)
		if err != nil {
			return err
		}
		p.FailOn = fc
	}
	return nil
}

// mergeFlags layers non-empty flag values onto cfg (the highest-precedence layer).
func mergeFlags(cfg *model.Config, flags Flags) error {
	if flags.License != "" {
		cfg.License = flags.License
	}
	if flags.Holder != "" {
		cfg.Holder = flags.Holder
	}
	if flags.Year != "" {
		ys, err := ParseYearSpec(flags.Year)
		if err != nil {
			return err
		}
		cfg.Year = ys
	}
	if flags.Style != "" {
		st, err := ParseStyle(flags.Style)
		if err != nil {
			return err
		}
		cfg.Style = st
	}
	if len(flags.Exclude) > 0 {
		cfg.Excludes = append(cfg.Excludes, flags.Exclude...)
	}
	return nil
}

// fileTypeFromYAML builds a model.FileType from a config file_types override. The
// override is keyed by extension; "style: line" requires a "line" prefix, while
// "style: block" requires "open"/"close" delimiters.
func fileTypeFromYAML(ext string, ov fileTypeOverrideYAML) (model.FileType, error) {
	norm := normalizeExt(ext)
	ft := model.FileType{
		Name:       "custom" + norm,
		Extensions: []string{norm},
	}
	switch strings.ToLower(strings.TrimSpace(ov.Style)) {
	case "line":
		if ov.Line == "" {
			return model.FileType{}, fmt.Errorf("config: file_types %q: style=line requires a non-empty \"line\" prefix", ext)
		}
		ft.CommentStyle = model.CommentStyle{Block: false, LinePrefix: ov.Line}
	case "block":
		if ov.Open == "" || ov.Close == "" {
			return model.FileType{}, fmt.Errorf("config: file_types %q: style=block requires \"open\" and \"close\" delimiters", ext)
		}
		ft.CommentStyle = model.CommentStyle{Block: true, Open: ov.Open, Close: ov.Close}
	case "":
		return model.FileType{}, fmt.Errorf("config: file_types %q: missing \"style\" (expected line or block)", ext)
	default:
		return model.FileType{}, fmt.Errorf("config: file_types %q: unknown style %q (expected line or block)", ext, ov.Style)
	}
	return ft, nil
}

// normalizeExt lowercases and ensures a leading dot, so ".MyExt", "myext", and
// ".myext" all key the same override slot that filetype.Merge expects.
func normalizeExt(ext string) string {
	e := strings.ToLower(strings.TrimSpace(ext))
	if e == "" {
		return e
	}
	if !strings.HasPrefix(e, ".") {
		e = "." + e
	}
	return e
}

// LookupFunc returns the file-type lookup for cfg, layering the config's
// file_types overrides onto the built-in table via the frozen filetype.Merge.
// WHY a thin wrapper: callers (enumerate/apply) get an override-aware lookup
// without reaching into both packages and re-deriving the merge themselves.
func LookupFunc(cfg model.Config) func(path string) (model.FileType, bool) {
	if len(cfg.FileTypeOverrides) == 0 {
		return filetype.Lookup
	}
	return filetype.Merge(cfg.FileTypeOverrides)
}

// ParseYearSpec parses a raw --year/config token into a model.YearSpec.
// Accepts "current", "git", "YYYY", and "YYYY-YYYY".
func ParseYearSpec(raw string) (model.YearSpec, error) {
	s := strings.TrimSpace(raw)
	switch strings.ToLower(s) {
	case "current":
		return model.YearSpec{Kind: model.YearCurrent}, nil
	case "git":
		return model.YearSpec{Kind: model.YearGit}, nil
	}
	if s == "" {
		return model.YearSpec{}, fmt.Errorf("config: empty year spec (expected current|git|YYYY|YYYY-YYYY)")
	}
	// Range "YYYY-YYYY".
	if before, after, ok := strings.Cut(s, "-"); ok {
		start, err := parseYear(before)
		if err != nil {
			return model.YearSpec{}, fmt.Errorf("config: invalid year range %q: %w", raw, err)
		}
		end, err := parseYear(after)
		if err != nil {
			return model.YearSpec{}, fmt.Errorf("config: invalid year range %q: %w", raw, err)
		}
		if end < start {
			return model.YearSpec{}, fmt.Errorf("config: invalid year range %q: end %d precedes start %d", raw, end, start)
		}
		return model.YearSpec{Kind: model.YearRange, Start: start, End: end}, nil
	}
	// Single explicit year "YYYY".
	y, err := parseYear(s)
	if err != nil {
		return model.YearSpec{}, fmt.Errorf("config: invalid year %q (expected current|git|YYYY|YYYY-YYYY)", raw)
	}
	return model.YearSpec{Kind: model.YearExplicit, Start: y}, nil
}

// parseYear parses a 4-digit calendar year. WHY the 4-digit clamp: it rejects
// junk like "21" or "20260" that strconv would otherwise accept as plausible
// years, keeping the year string we render unambiguous.
func parseYear(s string) (int, error) {
	t := strings.TrimSpace(s)
	if len(t) != 4 {
		return 0, fmt.Errorf("year must be 4 digits, got %q", s)
	}
	y, err := strconv.Atoi(t)
	if err != nil {
		return 0, fmt.Errorf("year must be numeric, got %q", s)
	}
	return y, nil
}

// ParseStyle parses a raw --style/config token into a model.HeaderStyle.
// Accepts "reuse", "notice", "reuse+notice".
func ParseStyle(raw string) (model.HeaderStyle, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "reuse":
		return model.StyleReuse, nil
	case "notice":
		return model.StyleNotice, nil
	case "reuse+notice":
		return model.StyleReusePlusNotice, nil
	default:
		return model.StyleReusePlusNotice, fmt.Errorf("config: unknown style %q (expected reuse|notice|reuse+notice)", raw)
	}
}

// parseFailConditions parses the config fail_on tokens into model.FailCondition
// values, rejecting unknown tokens so a typo cannot silently weaken the CI gate.
func parseFailConditions(tokens []string) ([]model.FailCondition, error) {
	out := make([]model.FailCondition, 0, len(tokens))
	for _, t := range tokens {
		switch strings.ToLower(strings.TrimSpace(t)) {
		case "missing-header":
			out = append(out, model.FailOnMissingHeader)
		case "unknown-license":
			out = append(out, model.FailOnUnknownLicense)
		case "policy-violation":
			out = append(out, model.FailOnPolicyViolation)
		case "unresolved-dependency":
			out = append(out, model.FailOnUnresolvedDependency)
		default:
			return nil, fmt.Errorf("config: unknown fail_on condition %q", t)
		}
	}
	return out, nil
}

// fillRequired ensures license and holder are set, prompting on a TTY when
// interactive is true and hard-erroring (no hang) otherwise. WHY prompt order
// (license then holder): it matches the YAML key order and the init command's
// scaffold, so an operator sees a predictable sequence.
func fillRequired(cfg *model.Config, interactive bool) error {
	if cfg.License != "" && cfg.Holder != "" {
		return nil
	}
	if !interactive {
		return ErrMissingRequired
	}

	r := bufio.NewReader(promptReader)
	if cfg.License == "" {
		v, err := prompt(r, "Target SPDX license identifier (e.g. AGPL-3.0-or-later)")
		if err != nil {
			return err
		}
		cfg.License = v
	}
	if cfg.Holder == "" {
		v, err := prompt(r, "Copyright holder (e.g. Kingsrook, LLC)")
		if err != nil {
			return err
		}
		cfg.Holder = v
	}
	// A prompt the operator answered with whitespace/EOF still leaves a required
	// field empty; treat that as a hard failure rather than writing a blank header.
	if cfg.License == "" || cfg.Holder == "" {
		return ErrMissingRequired
	}
	return nil
}

// prompt writes a question to promptWriter and reads one trimmed line from r. An
// EOF before any input (closed stdin) yields an empty string and no error, which
// fillRequired converts into ErrMissingRequired so the tool never hangs.
func prompt(r *bufio.Reader, question string) (string, error) {
	fmt.Fprintf(promptWriter, "%s: ", question)
	line, err := r.ReadString('\n')
	line = strings.TrimSpace(line)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return line, nil
		}
		return "", fmt.Errorf("config: read prompt response: %w", err)
	}
	return line, nil
}

// userConfigPath returns the user/global config path under $XDG_CONFIG_HOME,
// falling back to ~/.config per the XDG spec. Returns "" when no home/XDG dir can
// be determined (the layer is simply skipped).
func userConfigPath() string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, userConfigRel)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", userConfigRel)
}

// repoConfigPath resolves the repo-layer config path. An explicit --config wins
// and is returned with explicit=true (a missing explicit file is an error the
// caller surfaces); otherwise the discovered .license-tool.yaml at the scan root
// is returned with explicit=false (a missing discovered file is silently skipped).
func repoConfigPath(scanPath, configFlag string) (path string, explicit bool) {
	if configFlag != "" {
		return configFlag, true
	}
	return filepath.Join(scanPath, repoConfigName), false
}

// fileExists reports whether path names an existing regular-readable file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}
