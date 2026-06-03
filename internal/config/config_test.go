package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KofTwentyTwo/license-tool/internal/model"
)

// withPromptIO swaps the package prompt seams for the duration of a test and
// restores them afterward, so prompt-driven cases run without a real terminal.
func withPromptIO(t *testing.T, input string) *strings.Builder {
	t.Helper()
	origR, origW := promptReader, promptWriter
	out := &strings.Builder{}
	promptReader = strings.NewReader(input)
	promptWriter = out
	t.Cleanup(func() {
		promptReader = origR
		promptWriter = origW
	})
	return out
}

// isolateXDG points $XDG_CONFIG_HOME at an empty temp dir so the user/global
// layer never picks up a developer's real ~/.config during tests.
func isolateXDG(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	return dir
}

func TestParseYearSpec(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    model.YearSpec
		wantErr bool
	}{
		{"current", "current", model.YearSpec{Kind: model.YearCurrent}, false},
		{"current mixed case", "Current", model.YearSpec{Kind: model.YearCurrent}, false},
		{"git", "git", model.YearSpec{Kind: model.YearGit}, false},
		{"git padded", "  git  ", model.YearSpec{Kind: model.YearGit}, false},
		{"explicit year", "2026", model.YearSpec{Kind: model.YearExplicit, Start: 2026}, false},
		{"range", "2021-2026", model.YearSpec{Kind: model.YearRange, Start: 2021, End: 2026}, false},
		{"range same year", "2026-2026", model.YearSpec{Kind: model.YearRange, Start: 2026, End: 2026}, false},
		{"empty", "", model.YearSpec{}, true},
		{"two digit year", "26", model.YearSpec{}, true},
		{"five digit year", "20260", model.YearSpec{}, true},
		{"non-numeric", "twenty", model.YearSpec{}, true},
		{"range end before start", "2026-2021", model.YearSpec{}, true},
		{"range bad start", "20x1-2026", model.YearSpec{}, true},
		{"range bad end", "2021-20x6", model.YearSpec{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseYearSpec(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseYearSpec(%q) = %+v, want error", tt.raw, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseYearSpec(%q) unexpected error: %v", tt.raw, err)
			}
			if got != tt.want {
				t.Fatalf("ParseYearSpec(%q) = %+v, want %+v", tt.raw, got, tt.want)
			}
		})
	}
}

func TestParseStyle(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    model.HeaderStyle
		wantErr bool
	}{
		{"reuse", "reuse", model.StyleReuse, false},
		{"notice", "notice", model.StyleNotice, false},
		{"reuse+notice", "reuse+notice", model.StyleReusePlusNotice, false},
		{"mixed case", "Reuse+Notice", model.StyleReusePlusNotice, false},
		{"padded", "  notice  ", model.StyleNotice, false},
		{"unknown", "fancy", model.StyleReusePlusNotice, true},
		{"empty", "", model.StyleReusePlusNotice, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseStyle(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseStyle(%q) = %v, want error", tt.raw, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseStyle(%q) unexpected error: %v", tt.raw, err)
			}
			if got != tt.want {
				t.Fatalf("ParseStyle(%q) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}

func TestDefaults(t *testing.T) {
	d := Defaults()
	if d.License != "" {
		t.Errorf("Defaults().License = %q, want empty (required, no safe default)", d.License)
	}
	if d.Holder != "" {
		t.Errorf("Defaults().Holder = %q, want empty (required, no safe default)", d.Holder)
	}
	if d.Year.Kind != model.YearGit {
		t.Errorf("Defaults().Year.Kind = %v, want YearGit", d.Year.Kind)
	}
	if d.Style != model.StyleReusePlusNotice {
		t.Errorf("Defaults().Style = %v, want StyleReusePlusNotice", d.Style)
	}
	if !d.ManageLicenseFile {
		t.Errorf("Defaults().ManageLicenseFile = false, want true")
	}
	wantFail := []model.FailCondition{
		model.FailOnMissingHeader,
		model.FailOnUnknownLicense,
		model.FailOnPolicyViolation,
	}
	if len(d.Policy.FailOn) != len(wantFail) {
		t.Fatalf("Defaults().Policy.FailOn = %v, want %v", d.Policy.FailOn, wantFail)
	}
	for i, fc := range wantFail {
		if d.Policy.FailOn[i] != fc {
			t.Errorf("Defaults().Policy.FailOn[%d] = %v, want %v", i, d.Policy.FailOn[i], fc)
		}
	}
}

func TestDecodeYAML(t *testing.T) {
	t.Run("full document", func(t *testing.T) {
		data := []byte(`
license: AGPL-3.0-or-later
holder: "Kingsrook, LLC"
year: git
style: reuse+notice
manage_license_file: true
exclude:
  - "**/generated/**"
policy:
  required: AGPL-3.0-or-later
  allow: [MIT, Apache-2.0]
  deny: [GPL-2.0-only]
  fail_on: [missing-header, unknown-license]
file_types:
  ".myext": { style: line, line: "// " }
`)
		fs, err := decodeYAML(data)
		if err != nil {
			t.Fatalf("decodeYAML error: %v", err)
		}
		if fs.License != "AGPL-3.0-or-later" || fs.Holder != "Kingsrook, LLC" {
			t.Errorf("scalars not decoded: %+v", fs)
		}
		if fs.ManageLicenseFile == nil || !*fs.ManageLicenseFile {
			t.Errorf("manage_license_file not decoded as true: %+v", fs.ManageLicenseFile)
		}
		if len(fs.Policy.Allow) != 2 || len(fs.FileTypes) != 1 {
			t.Errorf("nested keys not decoded: %+v", fs)
		}
	})

	t.Run("empty document", func(t *testing.T) {
		fs, err := decodeYAML([]byte(""))
		if err != nil {
			t.Fatalf("decodeYAML(empty) error: %v", err)
		}
		if fs.License != "" || fs.ManageLicenseFile != nil {
			t.Errorf("empty document should decode to zero schema, got %+v", fs)
		}
	})

	t.Run("unknown key rejected", func(t *testing.T) {
		// A typo'd key must fail rather than be silently ignored.
		_, err := decodeYAML([]byte("licence: MIT\n"))
		if err == nil {
			t.Fatal("decodeYAML with unknown key should error")
		}
	})

	t.Run("malformed yaml", func(t *testing.T) {
		_, err := decodeYAML([]byte("license: [unterminated\n"))
		if err == nil {
			t.Fatal("decodeYAML with malformed yaml should error")
		}
	})
}

func TestLoadFile(t *testing.T) {
	t.Run("valid file seeds from defaults", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, repoConfigName)
		writeFile(t, p, `
license: MIT
holder: Acme
style: reuse
`)
		cfg, err := LoadFile(p)
		if err != nil {
			t.Fatalf("LoadFile error: %v", err)
		}
		if cfg.License != "MIT" || cfg.Holder != "Acme" {
			t.Errorf("scalars not loaded: %+v", cfg)
		}
		if cfg.Style != model.StyleReuse {
			t.Errorf("style = %v, want StyleReuse", cfg.Style)
		}
		// Unset keys must carry the built-in defaults.
		if cfg.Year.Kind != model.YearGit {
			t.Errorf("unset year should default to YearGit, got %v", cfg.Year.Kind)
		}
		if !cfg.ManageLicenseFile {
			t.Errorf("unset manage_license_file should default to true")
		}
	})

	t.Run("missing file errors", func(t *testing.T) {
		_, err := LoadFile(filepath.Join(t.TempDir(), "nope.yaml"))
		if err == nil {
			t.Fatal("LoadFile of missing file should error")
		}
	})

	t.Run("invalid style errors", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, repoConfigName)
		writeFile(t, p, "style: bogus\n")
		if _, err := LoadFile(p); err == nil {
			t.Fatal("LoadFile with invalid style should error")
		}
	})

	t.Run("explicit manage_license_file false", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, repoConfigName)
		writeFile(t, p, "manage_license_file: false\n")
		cfg, err := LoadFile(p)
		if err != nil {
			t.Fatalf("LoadFile error: %v", err)
		}
		if cfg.ManageLicenseFile {
			t.Error("explicit false must override the default true")
		}
	})
}

func TestFileTypeOverrides(t *testing.T) {
	tests := []struct {
		name     string
		yaml     string
		wantErr  bool
		assertFn func(t *testing.T, cfg model.Config)
	}{
		{
			name: "line override",
			yaml: `file_types:
  ".myext": { style: line, line: "// " }`,
			assertFn: func(t *testing.T, cfg model.Config) {
				ft, ok := cfg.FileTypeOverrides[".myext"]
				if !ok {
					t.Fatalf("override .myext missing: %+v", cfg.FileTypeOverrides)
				}
				if ft.CommentStyle.Block || ft.CommentStyle.LinePrefix != "// " {
					t.Errorf("line override wrong: %+v", ft.CommentStyle)
				}
			},
		},
		{
			name: "block override",
			yaml: `file_types:
  ".blk": { style: block, open: "/*", close: "*/" }`,
			assertFn: func(t *testing.T, cfg model.Config) {
				ft := cfg.FileTypeOverrides[".blk"]
				if !ft.CommentStyle.Block || ft.CommentStyle.Open != "/*" || ft.CommentStyle.Close != "*/" {
					t.Errorf("block override wrong: %+v", ft.CommentStyle)
				}
			},
		},
		{
			name: "extension normalized without dot",
			yaml: `file_types:
  "MyExt": { style: line, line: "; " }`,
			assertFn: func(t *testing.T, cfg model.Config) {
				if _, ok := cfg.FileTypeOverrides[".myext"]; !ok {
					t.Errorf("extension not normalized to .myext: %+v", cfg.FileTypeOverrides)
				}
			},
		},
		{
			name:    "line missing prefix",
			yaml:    "file_types:\n  \".x\": { style: line }",
			wantErr: true,
		},
		{
			name:    "block missing delimiters",
			yaml:    "file_types:\n  \".x\": { style: block }",
			wantErr: true,
		},
		{
			name:    "missing style",
			yaml:    "file_types:\n  \".x\": { line: \"// \" }",
			wantErr: true,
		},
		{
			name:    "unknown style",
			yaml:    "file_types:\n  \".x\": { style: weird, line: \"// \" }",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			p := filepath.Join(dir, repoConfigName)
			writeFile(t, p, tt.yaml+"\n")
			cfg, err := LoadFile(p)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("LoadFile(%s) want error, got %+v", tt.name, cfg)
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadFile(%s) unexpected error: %v", tt.name, err)
			}
			tt.assertFn(t, cfg)
		})
	}
}

func TestLookupFunc(t *testing.T) {
	t.Run("no overrides returns builtin lookup", func(t *testing.T) {
		look := LookupFunc(model.Config{})
		ft, ok := look("Main.java")
		if !ok || ft.Name != "Java" {
			t.Errorf("builtin lookup failed for .java: ok=%v ft=%+v", ok, ft)
		}
	})

	t.Run("overrides layered onto builtin", func(t *testing.T) {
		cfg := model.Config{
			FileTypeOverrides: map[string]model.FileType{
				".myext": {Name: "custom", Extensions: []string{".myext"}, CommentStyle: model.CommentStyle{LinePrefix: "// "}},
			},
		}
		look := LookupFunc(cfg)
		if ft, ok := look("file.myext"); !ok || ft.Name != "custom" {
			t.Errorf("override lookup failed: ok=%v ft=%+v", ok, ft)
		}
		// Built-in coverage must remain intact alongside the override.
		if ft, ok := look("Main.java"); !ok || ft.Name != "Java" {
			t.Errorf("builtin lost after merge: ok=%v ft=%+v", ok, ft)
		}
	})
}

func TestParseFailConditions(t *testing.T) {
	t.Run("all known tokens", func(t *testing.T) {
		got, err := parseFailConditions([]string{"missing-header", "unknown-license", "policy-violation", "unresolved-dependency"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []model.FailCondition{
			model.FailOnMissingHeader,
			model.FailOnUnknownLicense,
			model.FailOnPolicyViolation,
			model.FailOnUnresolvedDependency,
		}
		if len(got) != len(want) {
			t.Fatalf("got %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("token %d = %v, want %v", i, got[i], want[i])
			}
		}
	})
	t.Run("unknown token errors", func(t *testing.T) {
		if _, err := parseFailConditions([]string{"missing-header", "explode"}); err == nil {
			t.Fatal("unknown fail_on token should error")
		}
	})
}

func TestResolveLayering(t *testing.T) {
	t.Run("flags override repo override user override defaults", func(t *testing.T) {
		xdg := isolateXDG(t)
		// User/global layer.
		userPath := filepath.Join(xdg, "license-tool", "config.yaml")
		writeFile(t, userPath, `
license: MIT
holder: GlobalHolder
style: reuse
year: current
`)
		// Repo layer overrides holder and style; inherits nothing else from user
		// except where it stays silent (license, year here).
		repo := t.TempDir()
		writeFile(t, filepath.Join(repo, repoConfigName), `
holder: RepoHolder
style: notice
`)
		// Flags override holder again and set year.
		flags := Flags{Holder: "FlagHolder", Year: "2026"}

		cfg, err := Resolve(repo, flags, Options{Interactive: false})
		if err != nil {
			t.Fatalf("Resolve error: %v", err)
		}
		if cfg.License != "MIT" {
			t.Errorf("license = %q, want MIT (from user layer, untouched above)", cfg.License)
		}
		if cfg.Holder != "FlagHolder" {
			t.Errorf("holder = %q, want FlagHolder (flag wins)", cfg.Holder)
		}
		if cfg.Style != model.StyleNotice {
			t.Errorf("style = %v, want StyleNotice (repo wins over user)", cfg.Style)
		}
		if cfg.Year.Kind != model.YearExplicit || cfg.Year.Start != 2026 {
			t.Errorf("year = %+v, want explicit 2026 (flag wins)", cfg.Year)
		}
	})

	t.Run("explicit config flag wins over discovery", func(t *testing.T) {
		isolateXDG(t)
		repo := t.TempDir()
		// Discovered repo file would set MIT; explicit --config sets Apache.
		writeFile(t, filepath.Join(repo, repoConfigName), "license: MIT\nholder: A\n")
		explicit := filepath.Join(t.TempDir(), "custom.yaml")
		writeFile(t, explicit, "license: Apache-2.0\nholder: B\n")

		cfg, err := Resolve(repo, Flags{ConfigPath: explicit}, Options{Interactive: false})
		if err != nil {
			t.Fatalf("Resolve error: %v", err)
		}
		if cfg.License != "Apache-2.0" || cfg.Holder != "B" {
			t.Errorf("explicit --config not honored: %+v", cfg)
		}
	})

	t.Run("missing explicit config is an error", func(t *testing.T) {
		isolateXDG(t)
		repo := t.TempDir()
		_, err := Resolve(repo, Flags{ConfigPath: filepath.Join(repo, "nope.yaml")}, Options{Interactive: false})
		if err == nil {
			t.Fatal("missing --config file should error")
		}
	})

	t.Run("excludes accumulate across layers", func(t *testing.T) {
		xdg := isolateXDG(t)
		writeFile(t, filepath.Join(xdg, "license-tool", "config.yaml"), "license: MIT\nholder: A\nexclude: [\"**/vendor/**\"]\n")
		repo := t.TempDir()
		writeFile(t, filepath.Join(repo, repoConfigName), "exclude: [\"**/generated/**\"]\n")
		cfg, err := Resolve(repo, Flags{Exclude: []string{"**/*.pb.go"}}, Options{Interactive: false})
		if err != nil {
			t.Fatalf("Resolve error: %v", err)
		}
		want := map[string]bool{"**/vendor/**": true, "**/generated/**": true, "**/*.pb.go": true}
		if len(cfg.Excludes) != len(want) {
			t.Fatalf("excludes = %v, want all three layers", cfg.Excludes)
		}
		for _, e := range cfg.Excludes {
			if !want[e] {
				t.Errorf("unexpected exclude %q", e)
			}
		}
	})
}

func TestResolveRequiredFields(t *testing.T) {
	// Required-field enforcement only applies to write operations, so every subtest
	// opts in with RequireApply: true (read-only audit/check tolerate empty fields).
	t.Run("non-tty missing required is hard error no hang", func(t *testing.T) {
		isolateXDG(t)
		repo := t.TempDir() // no repo config, no flags -> license+holder empty
		_, err := Resolve(repo, Flags{}, Options{Interactive: false, RequireApply: true})
		if !errors.Is(err, ErrMissingRequired) {
			t.Fatalf("err = %v, want ErrMissingRequired", err)
		}
	})

	t.Run("non-tty holder present license missing", func(t *testing.T) {
		isolateXDG(t)
		repo := t.TempDir()
		_, err := Resolve(repo, Flags{Holder: "Acme"}, Options{Interactive: false, RequireApply: true})
		if !errors.Is(err, ErrMissingRequired) {
			t.Fatalf("err = %v, want ErrMissingRequired", err)
		}
	})

	t.Run("tty prompts fill both fields", func(t *testing.T) {
		isolateXDG(t)
		out := withPromptIO(t, "MIT\nAcme Corp\n")
		repo := t.TempDir()
		cfg, err := Resolve(repo, Flags{}, Options{Interactive: true, RequireApply: true})
		if err != nil {
			t.Fatalf("Resolve error: %v", err)
		}
		if cfg.License != "MIT" || cfg.Holder != "Acme Corp" {
			t.Errorf("prompt did not fill fields: %+v", cfg)
		}
		// Prompts must go to the prompt writer (stderr), not stdout.
		if !strings.Contains(out.String(), "license") || !strings.Contains(out.String(), "holder") {
			t.Errorf("prompt text not written: %q", out.String())
		}
	})

	t.Run("tty prompts only for the missing field", func(t *testing.T) {
		isolateXDG(t)
		withPromptIO(t, "Acme Corp\n") // only holder will be asked
		repo := t.TempDir()
		cfg, err := Resolve(repo, Flags{License: "Apache-2.0"}, Options{Interactive: true, RequireApply: true})
		if err != nil {
			t.Fatalf("Resolve error: %v", err)
		}
		if cfg.License != "Apache-2.0" || cfg.Holder != "Acme Corp" {
			t.Errorf("partial prompt wrong: %+v", cfg)
		}
	})

	t.Run("tty but EOF stdin does not hang and hard-errors", func(t *testing.T) {
		isolateXDG(t)
		withPromptIO(t, "") // closed stdin: ReadString returns EOF immediately
		repo := t.TempDir()
		_, err := Resolve(repo, Flags{}, Options{Interactive: true, RequireApply: true})
		if !errors.Is(err, ErrMissingRequired) {
			t.Fatalf("err = %v, want ErrMissingRequired on EOF", err)
		}
	})

	t.Run("tty blank answer hard-errors", func(t *testing.T) {
		isolateXDG(t)
		withPromptIO(t, "   \n   \n") // whitespace-only answers
		repo := t.TempDir()
		_, err := Resolve(repo, Flags{}, Options{Interactive: true, RequireApply: true})
		if !errors.Is(err, ErrMissingRequired) {
			t.Fatalf("err = %v, want ErrMissingRequired on blank answers", err)
		}
	})
}

func TestResolveLicenseValidation(t *testing.T) {
	t.Run("valid spdx id passes", func(t *testing.T) {
		isolateXDG(t)
		repo := t.TempDir()
		cfg, err := Resolve(repo, Flags{License: "AGPL-3.0-or-later", Holder: "Kingsrook, LLC"}, Options{Interactive: false})
		if err != nil {
			t.Fatalf("Resolve error: %v", err)
		}
		if cfg.License != "AGPL-3.0-or-later" {
			t.Errorf("license = %q", cfg.License)
		}
	})

	t.Run("unknown spdx id rejected", func(t *testing.T) {
		isolateXDG(t)
		repo := t.TempDir()
		_, err := Resolve(repo, Flags{License: "NOT-A-REAL-LICENSE", Holder: "Acme"}, Options{Interactive: false})
		if err == nil {
			t.Fatal("unknown SPDX id should be rejected")
		}
		if errors.Is(err, ErrMissingRequired) {
			t.Fatalf("wrong error class: %v", err)
		}
	})
}

func TestResolveBadConfigSurfaces(t *testing.T) {
	t.Run("invalid year in repo config errors", func(t *testing.T) {
		isolateXDG(t)
		repo := t.TempDir()
		writeFile(t, filepath.Join(repo, repoConfigName), "license: MIT\nholder: A\nyear: nope\n")
		if _, err := Resolve(repo, Flags{}, Options{Interactive: false}); err == nil {
			t.Fatal("invalid year in config should error")
		}
	})

	t.Run("invalid fail_on in repo config errors", func(t *testing.T) {
		isolateXDG(t)
		repo := t.TempDir()
		writeFile(t, filepath.Join(repo, repoConfigName), "license: MIT\nholder: A\npolicy:\n  fail_on: [bogus]\n")
		if _, err := Resolve(repo, Flags{}, Options{Interactive: false}); err == nil {
			t.Fatal("invalid fail_on in config should error")
		}
	})

	t.Run("policy from config carried through", func(t *testing.T) {
		isolateXDG(t)
		repo := t.TempDir()
		writeFile(t, filepath.Join(repo, repoConfigName), `
license: AGPL-3.0-or-later
holder: Kingsrook, LLC
policy:
  required: AGPL-3.0-or-later
  allow: [AGPL-3.0-or-later, MIT]
  deny: [GPL-2.0-only]
  fail_on: [missing-header]
`)
		cfg, err := Resolve(repo, Flags{}, Options{Interactive: false})
		if err != nil {
			t.Fatalf("Resolve error: %v", err)
		}
		if cfg.Policy.Required != "AGPL-3.0-or-later" {
			t.Errorf("policy.required = %q", cfg.Policy.Required)
		}
		if len(cfg.Policy.Allow) != 2 || len(cfg.Policy.Deny) != 1 {
			t.Errorf("policy lists wrong: %+v", cfg.Policy)
		}
		if len(cfg.Policy.FailOn) != 1 || cfg.Policy.FailOn[0] != model.FailOnMissingHeader {
			t.Errorf("policy.fail_on overridden wrong: %+v", cfg.Policy.FailOn)
		}
	})
}

func TestUserConfigPath(t *testing.T) {
	t.Run("uses XDG_CONFIG_HOME when set", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg-test")
		got := userConfigPath()
		want := filepath.Join("/tmp/xdg-test", userConfigRel)
		if got != want {
			t.Errorf("userConfigPath = %q, want %q", got, want)
		}
	})

	t.Run("falls back to home/.config", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "")
		home, err := os.UserHomeDir()
		if err != nil {
			t.Skip("no home dir in this environment")
		}
		got := userConfigPath()
		want := filepath.Join(home, ".config", userConfigRel)
		if got != want {
			t.Errorf("userConfigPath = %q, want %q", got, want)
		}
	})
}

func TestNormalizeExt(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{".myext", ".myext"},
		{"myext", ".myext"},
		{".MyExt", ".myext"},
		{"  .MyExt  ", ".myext"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := normalizeExt(tt.in); got != tt.want {
			t.Errorf("normalizeExt(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// writeFile writes content to path, creating parent dirs, failing the test on error.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
