package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/KofTwentyTwo/license-tool/internal/applier"
	"github.com/KofTwentyTwo/license-tool/internal/config"
	"github.com/KofTwentyTwo/license-tool/internal/detect"
	"github.com/KofTwentyTwo/license-tool/internal/enumerate"
	"github.com/KofTwentyTwo/license-tool/internal/model"
	"github.com/KofTwentyTwo/license-tool/internal/report"
	"github.com/KofTwentyTwo/license-tool/internal/resolve"
	"github.com/KofTwentyTwo/license-tool/internal/spdx"
)

// sharedFlags holds the flags common to every subcommand. A pointer to one of
// these is bound on the root persistent flag set so subcommands read the same
// values.
type sharedFlags struct {
	configPath  string
	include     []string
	exclude     []string
	noGitignore bool
	quiet       bool
	verbose     bool
}

// newRootCmd assembles the full command tree. WHY a constructor that takes
// buildInfo: it keeps the ldflags vars in main.go and lets tests build an isolated
// tree with synthetic build metadata.
func newRootCmd(info buildInfo) *cobra.Command {
	shared := &sharedFlags{}

	root := &cobra.Command{
		Use:           "license-tool",
		Short:         "Audit and standardize license headers and metadata across codebases",
		Long:          "license-tool audits and standardizes source-file license headers and top-level license files across many codebases. It is license-agnostic: the target license, holder, year policy, and header style are all inputs.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	pf := root.PersistentFlags()
	pf.StringVar(&shared.configPath, "config", "", "path to a .license-tool.yaml (overrides discovery)")
	pf.StringArrayVar(&shared.include, "include", nil, "glob of files to include (repeatable)")
	pf.StringArrayVar(&shared.exclude, "exclude", nil, "glob of files to exclude (repeatable)")
	pf.BoolVar(&shared.noGitignore, "no-gitignore", false, "do not inherit .gitignore on the non-git walk")
	pf.BoolVarP(&shared.quiet, "quiet", "q", false, "suppress non-essential output")
	pf.BoolVarP(&shared.verbose, "verbose", "v", false, "verbose diagnostic output")

	root.AddCommand(
		newAuditCmd(shared),
		newCheckCmd(shared),
		newApplyCmd(shared),
		newLicenseCmd(shared),
		newInitCmd(shared),
		newVersionCmd(info),
	)
	return root
}

// argPath returns the path argument, defaulting to "." when none is given.
func argPath(args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return "."
}

// auditFlags are the flags specific to audit and check.
type auditFlags struct {
	format      string
	output      string
	deps        bool
	resolveDeps string
	failOn      []string
}

func newAuditCmd(shared *sharedFlags) *cobra.Command {
	f := &auditFlags{}
	cmd := &cobra.Command{
		Use:   "audit [path]",
		Short: "Report licenses across dependencies and source files (read-only)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := argPath(args)
			cfg, err := config.Resolve(path, sharedToFlags(shared), config.Options{Interactive: false})
			if err != nil {
				return err
			}
			format, err := report.ParseFormat(f.format)
			if err != nil {
				return err
			}
			r, err := report.Audit(path, cfg, report.Options{
				Format:            format,
				IncludeDeps:       f.deps,
				ResolveDeps:       f.resolveDeps,
				AllowToolShellOut: f.resolveDeps == "tool",
			}, buildAuditPipeline(cfg, shared))
			if err != nil {
				return err
			}
			return report.Render(cmd.OutOrStdout(), r, format)
		},
	}
	bindAuditFlags(cmd, f, false)
	return cmd
}

func newCheckCmd(shared *sharedFlags) *cobra.Command {
	f := &auditFlags{}
	cmd := &cobra.Command{
		Use:   "check [path]",
		Short: "Gate CI: non-zero exit on policy violation",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := argPath(args)
			cfg, err := config.Resolve(path, sharedToFlags(shared), config.Options{Interactive: false})
			if err != nil {
				return err
			}
			format, err := report.ParseFormat(f.format)
			if err != nil {
				return err
			}
			code, err := report.Check(path, cfg, report.Options{
				Format:            format,
				IncludeDeps:       f.deps,
				ResolveDeps:       f.resolveDeps,
				AllowToolShellOut: f.resolveDeps == "tool",
			}, buildAuditPipeline(cfg, shared))
			if err != nil {
				return err
			}
			if code != 0 {
				os.Exit(code)
			}
			return nil
		},
	}
	bindAuditFlags(cmd, f, true)
	return cmd
}

// bindAuditFlags binds the audit/check flag set. check forces a machine-friendly
// default format and exposes --fail-on.
func bindAuditFlags(cmd *cobra.Command, f *auditFlags, isCheck bool) {
	defaultFormat := "text"
	cmd.Flags().StringVar(&f.format, "format", defaultFormat, "output format: text|json|markdown")
	cmd.Flags().StringVar(&f.output, "output", "", "write report to file instead of stdout")
	cmd.Flags().BoolVar(&f.deps, "deps", true, "resolve dependency licenses")
	cmd.Flags().StringVar(&f.resolveDeps, "resolve-deps", "ondisk", "dependency resolution tier: ondisk|tool|off")
	if isCheck {
		cmd.Flags().StringArrayVar(&f.failOn, "fail-on", []string{"missing-header", "unknown-license", "policy-violation"}, "conditions that cause a non-zero exit")
	}
}

// applyFlags are the flags specific to apply and license.
type applyFlags struct {
	license       string
	holder        string
	year          string
	style         string
	write         bool
	allowDirty    bool
	force         bool
	commit        bool
	commitMessage string
}

func newApplyCmd(shared *sharedFlags) *cobra.Command {
	f := &applyFlags{}
	cmd := &cobra.Command{
		Use:   "apply [path]",
		Short: "Add or update canonical headers (dry-run unless --write)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := argPath(args)
			cfg, err := config.Resolve(path, applyToFlags(shared, f), config.Options{Interactive: isTTY(), RequireApply: true})
			if err != nil {
				return err
			}
			// config.Resolve already validates the merged license (which equals f.license
			// whenever the flag is set) against the vendored SPDX list, so no second check
			// is needed here.
			r, err := applier.Apply(path, cfg, applier.Options{
				Write:             f.write,
				AllowDirty:        f.allowDirty,
				Force:             f.force,
				Commit:            f.commit,
				CommitMessage:     f.commitMessage,
				ManageLicenseFile: cfg.ManageLicenseFile,
			})
			if err != nil {
				return err
			}
			return report.Render(cmd.OutOrStdout(), r, report.FormatText)
		},
	}
	bindApplyFlags(cmd, f)
	return cmd
}

func newLicenseCmd(shared *sharedFlags) *cobra.Command {
	f := &applyFlags{}
	cmd := &cobra.Command{
		Use:   "license [path]",
		Short: "Manage the top-level LICENSE and LICENSES/<id>.txt files",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := argPath(args)
			cfg, err := config.Resolve(path, applyToFlags(shared, f), config.Options{Interactive: isTTY(), RequireApply: true})
			if err != nil {
				return err
			}
			// config.Resolve already validates the merged license (which equals f.license
			// whenever the flag is set) against the vendored SPDX list, so no second check
			// is needed here.
			results, err := applier.ManageLicenseFiles(path, cfg, applier.Options{
				Write:             f.write,
				AllowDirty:        f.allowDirty,
				Force:             f.force,
				ManageLicenseFile: true,
			})
			if err != nil {
				return err
			}
			for _, fr := range results {
				fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", fr.Path, fr.Action)
			}
			return nil
		},
	}
	bindApplyFlags(cmd, f)
	return cmd
}

// bindApplyFlags binds the apply/license flag set shared by both commands.
func bindApplyFlags(cmd *cobra.Command, f *applyFlags) {
	cmd.Flags().StringVar(&f.license, "license", "", "target SPDX license id (validated against the vendored list)")
	cmd.Flags().StringVar(&f.holder, "holder", "", "copyright holder text")
	cmd.Flags().StringVar(&f.year, "year", "", "year policy: current|YYYY|YYYY-YYYY|git (default: git, or per config)")
	cmd.Flags().StringVar(&f.style, "style", "", "header style: reuse|notice|reuse+notice")
	cmd.Flags().BoolVar(&f.write, "write", false, "apply changes (without it: dry-run + unified diff)")
	cmd.Flags().BoolVar(&f.allowDirty, "allow-dirty", false, "permit writing to a dirty git working tree")
	cmd.Flags().BoolVar(&f.force, "force", false, "permit writing in a non-git directory")
	cmd.Flags().BoolVar(&f.commit, "commit", false, "make one atomic conventional commit per repo")
	cmd.Flags().StringVar(&f.commitMessage, "commit-message", "", "commit message template (with --commit)")
}

// interactiveCollect is a package-level seam over collectInteractive so tests can
// drive the init command's non-interactive flow (and inject answers) without a real
// terminal. Production always points at the huh-backed collectInteractive.
var interactiveCollect = collectInteractive

// answersToConfig validates and converts collected init answers into a model.Config,
// starting from the built-in Defaults so unset answers carry the documented default
// behavior. WHY validation lives here, not in the wizard: the wizard is the
// interactive shell (excluded from coverage); answersToConfig is the single tested
// gate that both the TTY and flag-only paths funnel through, so an invalid license
// or empty holder is rejected identically regardless of how the answers arrived.
func answersToConfig(a initAnswers) (model.Config, error) {
	if !spdx.Validate(a.License) {
		return model.Config{}, fmt.Errorf("init: %q is not a recognized SPDX license identifier", a.License)
	}
	if a.Holder == "" {
		return model.Config{}, fmt.Errorf("init: copyright holder is required")
	}
	cfg := config.Defaults()
	cfg.License = a.License
	cfg.Holder = a.Holder
	if a.Year != "" {
		ys, err := config.ParseYearSpec(a.Year)
		if err != nil {
			return model.Config{}, err
		}
		cfg.Year = ys
	}
	if a.Style != "" {
		st, err := config.ParseStyle(a.Style)
		if err != nil {
			return model.Config{}, err
		}
		cfg.Style = st
	}
	cfg.ManageLicenseFile = a.ManageLicenseFile
	cfg.Excludes = a.Excludes
	return cfg, nil
}

func newInitCmd(shared *sharedFlags) *cobra.Command {
	f := &applyFlags{}
	cmd := &cobra.Command{
		Use:   "init [path]",
		Short: "Scaffold a .license-tool.yaml (interactive on a TTY)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := argPath(args)
			out := cmd.OutOrStdout()
			a := initAnswers{
				License:           f.license,
				Holder:            f.holder,
				Year:              f.year,
				Style:             f.style,
				ManageLicenseFile: true,
				Excludes:          shared.exclude,
			}
			if err := interactiveCollect(&a, isTTY()); err != nil {
				return err
			}
			cfg, err := answersToConfig(a)
			if err != nil {
				return err
			}
			target, err := config.WriteFile(path, cfg, f.force)
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "wrote %s\n", target)
			return nil
		},
	}
	// bindApplyFlags already registers --force (bound to f.force); init reuses that
	// same flag to mean "overwrite an existing .license-tool.yaml", so we do not
	// re-register it here (cobra panics on a duplicate flag name).
	bindApplyFlags(cmd, f)
	return cmd
}

func newVersionCmd(info buildInfo) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version, commit, and build date",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintln(cmd.OutOrStdout(), info.String())
			if v := spdx.ListVersion(); v != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "SPDX license list: %s\n", v)
			}
		},
	}
}

// buildAuditPipeline wires the real enumerate / detect / resolve packages into the
// report.Pipeline that audit and check run. Enumerate reads each non-skipped file's
// content (the report layer needs the bytes for detection); Detect is the detector
// directly; ResolveDeps iterates the ecosystem resolvers detected at the root and
// concatenates their findings (unresolved entries included, never guessed).
func buildAuditPipeline(cfg model.Config, shared *sharedFlags) report.Pipeline {
	classify := config.ContentLookupFunc(cfg)
	return report.Pipeline{
		Enumerate: func(root string, excludes []string) ([]report.SourceFile, error) {
			entries, err := enumerate.EnumerateContent(root, enumerate.Options{
				Includes:    shared.include,
				Excludes:    excludes,
				NoGitignore: shared.noGitignore,
			}, classify)
			if err != nil {
				return nil, err
			}
			out := make([]report.SourceFile, 0, len(entries))
			for _, e := range entries {
				sf := report.SourceFile{
					Path:       e.Path,
					FileType:   e.FileType,
					Skip:       e.Skip,
					SkipReason: e.SkipReason,
				}
				if !e.Skip {
					content, rerr := os.ReadFile(e.AbsPath)
					if rerr != nil {
						sf.Skip = true
						sf.SkipReason = "read error: " + rerr.Error()
					} else {
						sf.Content = content
					}
				}
				out = append(out, sf)
			}
			return out, nil
		},
		Detect: detect.Detect,
		ResolveDeps: func(root string, allowToolShellOut bool) ([]model.DependencyLicense, error) {
			var out []model.DependencyLicense
			for _, r := range resolve.Detected(root) {
				deps, rerr := r.Resolve(root, model.ResolveOptions{AllowToolShellOut: allowToolShellOut})
				if rerr != nil {
					return nil, rerr
				}
				out = append(out, deps...)
			}
			return out, nil
		},
	}
}

// sharedToFlags adapts the CLI's sharedFlags into the config package's Flags.
func sharedToFlags(s *sharedFlags) config.Flags {
	return config.Flags{
		ConfigPath:  s.configPath,
		Include:     s.include,
		Exclude:     s.exclude,
		NoGitignore: s.noGitignore,
	}
}

// applyToFlags adapts the shared flags plus the apply/license-specific identity
// flags (license, holder, year, style) into config.Flags, so a write command's
// --license/--holder/etc. actually reach config resolution. Empty fields stay unset
// so the config-file and default layers still apply.
func applyToFlags(s *sharedFlags, f *applyFlags) config.Flags {
	return config.Flags{
		ConfigPath:  s.configPath,
		License:     f.license,
		Holder:      f.holder,
		Year:        f.year,
		Style:       f.style,
		Include:     s.include,
		Exclude:     s.exclude,
		NoGitignore: s.noGitignore,
	}
}

// isTTY reports whether stdin is an interactive terminal, gating interactive
// prompts (off in CI). WHY term.IsTerminal over an os.ModeCharDevice bit check:
// the mode-bit test treats every character device as a terminal, so redirecting
// from /dev/null (or any other char device, common in CI and shell pipelines)
// wrongly enters the interactive wizard. term.IsTerminal issues the actual
// terminal ioctl, so 'init </dev/null' is correctly seen as non-interactive.
func isTTY() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}
