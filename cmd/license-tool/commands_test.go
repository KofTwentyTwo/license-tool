package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/KofTwentyTwo/license-tool/internal/config"
	"github.com/KofTwentyTwo/license-tool/internal/model"
	"github.com/KofTwentyTwo/license-tool/internal/spdx"
)

// testBuildInfo is the synthetic ldflags metadata every test tree is built with.
var testBuildInfo = buildInfo{version: "1.2.3", commit: "deadbee", date: "2026-01-02"}

// isolateEnv pins $XDG_CONFIG_HOME at an empty temp dir so the user/global config
// layer never picks up a developer's real ~/.config, and forces a non-interactive
// stdin so isTTY() is deterministically false (no prompts) for the run. The git
// identity vars keep any committing deterministic.
func isolateEnv(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("GIT_AUTHOR_NAME", "Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")
	forceNonTTYStdin(t)
}

// forceNonTTYStdin replaces os.Stdin with a closed file descriptor so isTTY()
// returns false deterministically regardless of how the test runner wires stdin.
// A closed fd makes os.Stdin.Stat() error, which isTTY treats as "not a terminal".
func forceNonTTYStdin(t *testing.T) {
	t.Helper()
	orig := os.Stdin
	f, err := os.Open(os.DevNull)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	os.Stdin = f
	t.Cleanup(func() { os.Stdin = orig })
}

// runRoot builds an isolated command tree, runs it with args, and returns the
// combined stdout+stderr text plus the execution error. Output is captured on a
// single buffer so assertions can inspect everything the command emitted.
func runRoot(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := newRootCmd(testBuildInfo)
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

// writeFile writes content under dir/rel, creating parent directories, and returns
// the absolute path.
func writeFile(t *testing.T, dir, rel, content string) string {
	t.Helper()
	abs := filepath.Join(dir, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
	require.NoError(t, os.WriteFile(abs, []byte(content), 0o644))
	return abs
}

const configYAML = "license: AGPL-3.0-or-later\nholder: Acme, LLC\nyear: \"2026\"\n"

// fixtureDir creates a temp directory with a committed .license-tool.yaml and a
// headerless Go source file.
func fixtureDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, dir, ".license-tool.yaml", configYAML)
	writeFile(t, dir, "main.go", "package main\n\nfunc main() {}\n")
	return dir
}

// initGitRepo initializes a clean git repo at dir with all current files committed,
// using the deterministic identity set by isolateEnv.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init"},
		{"add", "-A"},
		{"commit", "-m", "initial"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, b)
		}
	}
}

func TestNewRootCmd(t *testing.T) {
	root := newRootCmd(testBuildInfo)
	assert.Equal(t, "license-tool", root.Use)
	assert.True(t, root.SilenceUsage)
	assert.True(t, root.SilenceErrors)

	want := []string{"audit", "check", "apply", "license", "init", "version"}
	got := make([]string, 0, len(root.Commands()))
	for _, c := range root.Commands() {
		got = append(got, c.Name())
	}
	for _, name := range want {
		assert.Contains(t, got, name, "command %q should be registered", name)
	}

	// Persistent flags are bound on the root.
	for _, f := range []string{"config", "include", "exclude", "no-gitignore", "quiet", "verbose"} {
		assert.NotNil(t, root.PersistentFlags().Lookup(f), "persistent flag %q", f)
	}
}

func TestArgPath(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"no args defaults to dot", nil, "."},
		{"empty slice defaults to dot", []string{}, "."},
		{"first arg used", []string{"/some/path"}, "/some/path"},
		{"only first arg used", []string{"a", "b"}, "a"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, argPath(tt.args))
		})
	}
}

func TestVersionCommand(t *testing.T) {
	isolateEnv(t)
	out, err := runRoot(t, "version")
	require.NoError(t, err)
	assert.Contains(t, out, "license-tool 1.2.3 (commit deadbee, built 2026-01-02)")
	// ListVersion is non-empty for the vendored snapshot, so the SPDX line prints.
	assert.Contains(t, out, "SPDX license list: "+spdx.ListVersion())
}

func TestAuditCommand(t *testing.T) {
	isolateEnv(t)

	t.Run("text format default", func(t *testing.T) {
		dir := fixtureDir(t)
		out, err := runRoot(t, "audit", dir, "--deps=false")
		require.NoError(t, err)
		assert.Contains(t, out, "license-tool audit report")
		assert.Contains(t, out, "AGPL-3.0-or-later")
		assert.Contains(t, out, "main.go")
	})

	t.Run("json format", func(t *testing.T) {
		dir := fixtureDir(t)
		out, err := runRoot(t, "audit", dir, "--format", "json", "--deps=false")
		require.NoError(t, err)
		assert.Contains(t, out, `"schema": "license-tool/report/v1"`)
	})

	t.Run("markdown format", func(t *testing.T) {
		dir := fixtureDir(t)
		out, err := runRoot(t, "audit", dir, "--format", "markdown", "--deps=false")
		require.NoError(t, err)
		assert.Contains(t, out, "# license-tool audit report")
	})

	t.Run("default path resolves to dot", func(t *testing.T) {
		// No path arg: argPath returns ".". Run from an isolated empty dir so the
		// audit sees only its own (absent) config and produces a clean report.
		dir := t.TempDir()
		writeFile(t, dir, "main.go", "package main\n")
		cwd, err := os.Getwd()
		require.NoError(t, err)
		require.NoError(t, os.Chdir(dir))
		t.Cleanup(func() { _ = os.Chdir(cwd) })

		out, err := runRoot(t, "audit", "--deps=false")
		require.NoError(t, err)
		assert.Contains(t, out, "license-tool audit report")
	})

	t.Run("config resolve error", func(t *testing.T) {
		dir := fixtureDir(t)
		_, err := runRoot(t, "audit", dir, "--config", filepath.Join(dir, "missing.yaml"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--config file not found")
	})

	t.Run("bad format", func(t *testing.T) {
		dir := fixtureDir(t)
		_, err := runRoot(t, "audit", dir, "--format", "xml", "--deps=false")
		require.Error(t, err)
		assert.Contains(t, err.Error(), `unknown format "xml"`)
	})

	t.Run("audit error from enumerate (missing path)", func(t *testing.T) {
		_, err := runRoot(t, "audit", filepath.Join(t.TempDir(), "does-not-exist"), "--deps=false")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "enumerate")
	})

	t.Run("deps resolution surfaces unresolved dependency", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, ".license-tool.yaml", configYAML)
		writeFile(t, dir, "package.json", `{"name":"x","dependencies":{"left-pad":"^1.0.0"}}`)
		out, err := runRoot(t, "audit", dir, "--format", "json")
		require.NoError(t, err)
		assert.Contains(t, out, "left-pad")
		assert.Contains(t, out, "unresolved")
	})

	t.Run("resolve dependency error aborts audit", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, ".license-tool.yaml", configYAML)
		// A malformed pom.xml makes the Maven resolver return a hard error, which the
		// ResolveDeps closure propagates and report.Audit surfaces.
		writeFile(t, dir, "pom.xml", "<<<not-xml>>>")
		_, err := runRoot(t, "audit", dir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "resolve dependencies")
	})

	t.Run("skipped binary file is enumerated without content read", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, ".license-tool.yaml", configYAML)
		writeFile(t, dir, "main.go", "package main\n")
		// A file with NUL bytes is classified binary and skipped by the enumerator.
		require.NoError(t, os.WriteFile(filepath.Join(dir, "blob.bin"), []byte{0x00, 0x01, 0x02, 0x00}, 0o644))
		out, err := runRoot(t, "audit", dir, "--deps=false")
		require.NoError(t, err)
		assert.Contains(t, out, "blob.bin")
		assert.Contains(t, out, "skipped")
	})

	t.Run("unreadable file becomes read-error skip", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, ".license-tool.yaml", configYAML)
		bad := filepath.Join(dir, "secret.go")
		require.NoError(t, os.WriteFile(bad, []byte("package main\n"), 0o000))
		t.Cleanup(func() { _ = os.Chmod(bad, 0o644) })
		out, err := runRoot(t, "audit", dir, "--deps=false")
		require.NoError(t, err)
		assert.Contains(t, out, "read error")
	})
}

func TestCheckCommand(t *testing.T) {
	isolateEnv(t)

	t.Run("passing policy returns nil", func(t *testing.T) {
		// fail_on excludes missing-header etc., so a headerless tree still passes and
		// check returns code 0 (nil error, no os.Exit).
		dir := t.TempDir()
		writeFile(t, dir, ".license-tool.yaml",
			"license: AGPL-3.0-or-later\nholder: Acme, LLC\npolicy:\n  fail_on: [unresolved-dependency]\n")
		writeFile(t, dir, "main.go", "package main\n")
		_, err := runRoot(t, "check", dir, "--deps=false")
		require.NoError(t, err)
	})

	t.Run("config resolve error", func(t *testing.T) {
		dir := fixtureDir(t)
		_, err := runRoot(t, "check", dir, "--config", filepath.Join(dir, "missing.yaml"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--config file not found")
	})

	t.Run("bad format", func(t *testing.T) {
		dir := fixtureDir(t)
		_, err := runRoot(t, "check", dir, "--format", "xml", "--deps=false")
		require.Error(t, err)
		assert.Contains(t, err.Error(), `unknown format "xml"`)
	})

	t.Run("check internal error from enumerate", func(t *testing.T) {
		_, err := runRoot(t, "check", filepath.Join(t.TempDir(), "nope"), "--deps=false")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "enumerate")
	})

	t.Run("fail-on flag is bound on check but not audit", func(t *testing.T) {
		root := newRootCmd(testBuildInfo)
		for _, c := range root.Commands() {
			switch c.Name() {
			case "check":
				assert.NotNil(t, c.Flags().Lookup("fail-on"), "check exposes --fail-on")
			case "audit":
				assert.Nil(t, c.Flags().Lookup("fail-on"), "audit does not expose --fail-on")
			}
		}
	})
}

// TestCheckFailingExits exercises the os.Exit(code) branch of the check command via
// the standard subprocess re-exec idiom: the parent runs the test binary with a
// sentinel env var set, the child performs the failing check (which calls os.Exit),
// and the parent asserts the observed non-zero exit code.
func TestCheckFailingExits(t *testing.T) {
	if os.Getenv("LICENSE_TOOL_CHECK_EXIT_CHILD") == "1" {
		// Child: a headerless tree under the default fail_on fails the policy gate,
		// so check calls os.Exit(1).
		dir := os.Getenv("LICENSE_TOOL_CHECK_EXIT_DIR")
		root := newRootCmd(testBuildInfo)
		root.SetArgs([]string{"check", dir, "--deps=false"})
		_ = root.Execute()
		return
	}

	isolateEnv(t)
	dir := t.TempDir()
	writeFile(t, dir, ".license-tool.yaml", configYAML)
	writeFile(t, dir, "main.go", "package main\n\nfunc main() {}\n")

	// os.Args[0] is the test binary path chosen by `go test`, never user input; this
	// is the standard subprocess idiom for covering an os.Exit code path.
	// nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command
	cmd := exec.Command(os.Args[0], "-test.run", "^TestCheckFailingExits$") //nolint:gosec
	cmd.Env = append(os.Environ(),
		"LICENSE_TOOL_CHECK_EXIT_CHILD=1",
		"LICENSE_TOOL_CHECK_EXIT_DIR="+dir,
		"XDG_CONFIG_HOME="+t.TempDir(),
	)
	out, err := cmd.CombinedOutput()

	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr, "child should exit non-zero; output:\n%s", out)
	assert.Equal(t, 1, exitErr.ExitCode(), "check failure exits with code 1")
}

func TestApplyCommand(t *testing.T) {
	isolateEnv(t)

	t.Run("dry-run reports without writing", func(t *testing.T) {
		dir := fixtureDir(t)
		initGitRepo(t, dir)
		out, err := runRoot(t, "apply", dir)
		require.NoError(t, err)
		assert.Contains(t, out, "license-tool audit report")

		// Dry run leaves the source untouched.
		content, rerr := os.ReadFile(filepath.Join(dir, "main.go"))
		require.NoError(t, rerr)
		assert.NotContains(t, string(content), "GNU Affero")
	})

	t.Run("write applies headers", func(t *testing.T) {
		dir := fixtureDir(t)
		initGitRepo(t, dir)
		out, err := runRoot(t, "apply", dir, "--write")
		require.NoError(t, err)
		assert.Contains(t, out, "license-tool audit report")

		content, rerr := os.ReadFile(filepath.Join(dir, "main.go"))
		require.NoError(t, rerr)
		assert.Contains(t, string(content), "GNU Affero")
	})

	t.Run("config resolve error", func(t *testing.T) {
		dir := fixtureDir(t)
		_, err := runRoot(t, "apply", dir, "--config", filepath.Join(dir, "missing.yaml"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--config file not found")
	})

	t.Run("invalid license id rejected by config", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "main.go", "package main\n")
		_, err := runRoot(t, "apply", dir, "--license", "NOT-A-LICENSE", "--holder", "Acme")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not a recognized SPDX license identifier")
	})

	t.Run("apply error: write in non-git dir without force", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, ".license-tool.yaml", configYAML)
		writeFile(t, dir, "main.go", "package main\n")
		_, err := runRoot(t, "apply", dir, "--write")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "non-git directory without --force")
	})
}

func TestLicenseCommand(t *testing.T) {
	isolateEnv(t)

	t.Run("dry-run lists managed license files", func(t *testing.T) {
		dir := fixtureDir(t)
		out, err := runRoot(t, "license", dir)
		require.NoError(t, err)
		assert.Contains(t, out, "LICENSE:")
		assert.Contains(t, out, filepath.Join("LICENSES", "AGPL-3.0-or-later.txt"))
	})

	t.Run("write creates LICENSE files", func(t *testing.T) {
		dir := fixtureDir(t)
		initGitRepo(t, dir)
		out, err := runRoot(t, "license", dir, "--write")
		require.NoError(t, err)
		assert.Contains(t, out, "LICENSE:")

		_, statErr := os.Stat(filepath.Join(dir, "LICENSE"))
		require.NoError(t, statErr)
		_, statErr = os.Stat(filepath.Join(dir, "LICENSES", "AGPL-3.0-or-later.txt"))
		require.NoError(t, statErr)
	})

	t.Run("config resolve error", func(t *testing.T) {
		dir := fixtureDir(t)
		_, err := runRoot(t, "license", dir, "--config", filepath.Join(dir, "missing.yaml"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--config file not found")
	})

	t.Run("invalid license id rejected by config", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "main.go", "package main\n")
		_, err := runRoot(t, "license", dir, "--license", "NOT-A-LICENSE", "--holder", "Acme")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not a recognized SPDX license identifier")
	})

	t.Run("valid but non-curated license surfaces a manage error", func(t *testing.T) {
		// Zlib is a real SPDX id (passes config validation and spdx.Validate) but is
		// outside the curated rendering set, so ManageLicenseFiles' spdx.Lookup fails
		// and the command returns that error.
		dir := t.TempDir()
		writeFile(t, dir, "main.go", "package main\n")
		_, err := runRoot(t, "license", dir, "--license", "Zlib", "--holder", "Acme")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown license")
	})
}

func TestInitCommand(t *testing.T) {
	isolateEnv(t)

	t.Run("flags scaffold config file", func(t *testing.T) {
		// Non-TTY stdin (forced by isolateEnv) skips the wizard, so init scaffolds a
		// .license-tool.yaml purely from the --license/--holder flags.
		dir := t.TempDir()
		out, err := runRoot(t, "init", dir, "--license", "MIT", "--holder", "Acme")
		require.NoError(t, err)
		target := filepath.Join(dir, ".license-tool.yaml")
		assert.Contains(t, out, "wrote "+target)
		// The scaffold must round-trip back through the config loader.
		cfg, lerr := config.LoadFile(target)
		require.NoError(t, lerr)
		assert.Equal(t, "MIT", cfg.License)
		assert.Equal(t, "Acme", cfg.Holder)
	})

	t.Run("missing license errors", func(t *testing.T) {
		// No --license flag and a non-TTY stdin: answersToConfig rejects the empty
		// license rather than writing a header with no identity.
		dir := t.TempDir()
		_, err := runRoot(t, "init", dir, "--holder", "Acme")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "SPDX license identifier")
	})

	t.Run("existing file needs force", func(t *testing.T) {
		dir := t.TempDir()
		_, err := runRoot(t, "init", dir, "--license", "MIT", "--holder", "Acme")
		require.NoError(t, err)
		// A second run without --force must refuse to clobber the committed config.
		_, err = runRoot(t, "init", dir, "--license", "Apache-2.0", "--holder", "Other")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "already exists")
		// With --force it overwrites in place.
		_, err = runRoot(t, "init", dir, "--license", "Apache-2.0", "--holder", "Other", "--force")
		require.NoError(t, err)
		cfg, lerr := config.LoadFile(filepath.Join(dir, ".license-tool.yaml"))
		require.NoError(t, lerr)
		assert.Equal(t, "Apache-2.0", cfg.License)
	})
}

// TestAnswersToConfig drives the single tested gate that both the TTY wizard and
// the flag-only path funnel through: the valid case plus each rejection (unknown
// license, empty holder, bad year, bad style). WHY exhaustive here: the wizard shell
// is excluded from coverage, so this is the only place the validation/parse arms are
// exercised, and they must reject identically regardless of how answers arrived.
func TestAnswersToConfig(t *testing.T) {
	t.Run("valid answers build a config from defaults", func(t *testing.T) {
		cfg, err := answersToConfig(initAnswers{
			License:           "MIT",
			Holder:            "Acme, LLC",
			Year:              "2021-2026",
			Style:             "reuse",
			ManageLicenseFile: false,
			Excludes:          []string{"**/vendor/**"},
		})
		require.NoError(t, err)
		assert.Equal(t, "MIT", cfg.License)
		assert.Equal(t, "Acme, LLC", cfg.Holder)
		assert.Equal(t, model.YearRange, cfg.Year.Kind)
		assert.Equal(t, 2021, cfg.Year.Start)
		assert.Equal(t, 2026, cfg.Year.End)
		assert.Equal(t, model.StyleReuse, cfg.Style)
		assert.False(t, cfg.ManageLicenseFile)
		assert.Equal(t, []string{"**/vendor/**"}, cfg.Excludes)
	})

	t.Run("empty year and style keep the built-in defaults", func(t *testing.T) {
		// Unset year/style answers must leave the Defaults() values untouched, so the
		// year/style parse branches are skipped (not defaulted to a parse of "").
		cfg, err := answersToConfig(initAnswers{License: "MIT", Holder: "Acme"})
		require.NoError(t, err)
		assert.Equal(t, model.YearGit, cfg.Year.Kind)
		assert.Equal(t, model.StyleReusePlusNotice, cfg.Style)
	})

	t.Run("unknown license rejected", func(t *testing.T) {
		_, err := answersToConfig(initAnswers{License: "NOT-A-LICENSE", Holder: "Acme"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not a recognized SPDX license identifier")
	})

	t.Run("empty holder rejected", func(t *testing.T) {
		_, err := answersToConfig(initAnswers{License: "MIT", Holder: ""})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "copyright holder is required")
	})

	t.Run("bad year rejected", func(t *testing.T) {
		_, err := answersToConfig(initAnswers{License: "MIT", Holder: "Acme", Year: "not-a-year"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "year")
	})

	t.Run("bad style rejected", func(t *testing.T) {
		_, err := answersToConfig(initAnswers{License: "MIT", Holder: "Acme", Style: "fancy"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown style")
	})
}

// TestInitCommandInteractiveCollectError covers the init RunE branch where the
// interactiveCollect seam returns an error: the command must propagate it verbatim
// and write nothing. The seam is overridden so the failure is deterministic without
// a real terminal (the huh wizard itself is excluded from coverage).
func TestInitCommandInteractiveCollectError(t *testing.T) {
	isolateEnv(t)

	orig := interactiveCollect
	wantErr := errors.New("wizard aborted")
	interactiveCollect = func(_ *initAnswers, _ bool) error { return wantErr }
	t.Cleanup(func() { interactiveCollect = orig })

	dir := t.TempDir()
	_, err := runRoot(t, "init", dir, "--license", "MIT", "--holder", "Acme")
	require.ErrorIs(t, err, wantErr, "init must propagate the interactiveCollect error")

	// The collect failure must abort before any file is written.
	_, statErr := os.Stat(filepath.Join(dir, ".license-tool.yaml"))
	assert.True(t, os.IsNotExist(statErr), "no config should be written when collect fails")
}

func TestSharedToFlags(t *testing.T) {
	s := &sharedFlags{
		configPath:  "/cfg.yaml",
		include:     []string{"*.go"},
		exclude:     []string{"vendor/**"},
		noGitignore: true,
	}
	got := sharedToFlags(s)
	assert.Equal(t, "/cfg.yaml", got.ConfigPath)
	assert.Equal(t, []string{"*.go"}, got.Include)
	assert.Equal(t, []string{"vendor/**"}, got.Exclude)
	assert.True(t, got.NoGitignore)
	// Identity fields are not carried by the shared adapter.
	assert.Empty(t, got.License)
	assert.Empty(t, got.Holder)
}

func TestApplyToFlags(t *testing.T) {
	s := &sharedFlags{
		configPath:  "/cfg.yaml",
		include:     []string{"src/**"},
		exclude:     []string{"build/**"},
		noGitignore: true,
	}
	f := &applyFlags{
		license: "MIT",
		holder:  "Acme, LLC",
		year:    "2026",
		style:   "reuse",
	}
	got := applyToFlags(s, f)
	assert.Equal(t, "/cfg.yaml", got.ConfigPath)
	assert.Equal(t, "MIT", got.License)
	assert.Equal(t, "Acme, LLC", got.Holder)
	assert.Equal(t, "2026", got.Year)
	assert.Equal(t, "reuse", got.Style)
	assert.Equal(t, []string{"src/**"}, got.Include)
	assert.Equal(t, []string{"build/**"}, got.Exclude)
	assert.True(t, got.NoGitignore)
}

func TestIsTTY(t *testing.T) {
	orig := os.Stdin
	t.Cleanup(func() { os.Stdin = orig })

	t.Run("character device is a TTY", func(t *testing.T) {
		// /dev/null is a character device, exercising the ModeCharDevice branch.
		f, err := os.Open(os.DevNull)
		require.NoError(t, err)
		t.Cleanup(func() { _ = f.Close() })
		os.Stdin = f
		assert.True(t, isTTY())
	})

	t.Run("regular file is not a TTY", func(t *testing.T) {
		dir := t.TempDir()
		f, err := os.Create(filepath.Join(dir, "stdin.txt"))
		require.NoError(t, err)
		t.Cleanup(func() { _ = f.Close() })
		os.Stdin = f
		assert.False(t, isTTY())
	})

	t.Run("stat error is not a TTY", func(t *testing.T) {
		// A closed descriptor makes Stat fail, exercising the err != nil branch.
		f, err := os.Open(os.DevNull)
		require.NoError(t, err)
		require.NoError(t, f.Close())
		os.Stdin = f
		assert.False(t, isTTY())
	})
}

func TestBuildInfoString(t *testing.T) {
	b := buildInfo{version: "9.9.9", commit: "abc1234", date: "2026-02-03"}
	assert.Equal(t, "license-tool 9.9.9 (commit abc1234, built 2026-02-03)", b.String())
	assert.True(t, strings.HasPrefix(b.String(), "license-tool "))
}
