package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/KofTwentyTwo/license-tool/internal/config"
	"github.com/KofTwentyTwo/license-tool/internal/enumerate"
	"github.com/KofTwentyTwo/license-tool/internal/model"
	"github.com/KofTwentyTwo/license-tool/internal/report"
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

func executeRoot(t *testing.T, args ...string) (stdout string, stderr string, code int) {
	t.Helper()
	root := newRootCmd(testBuildInfo)
	var out bytes.Buffer
	var errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs(args)
	code = execute(root)
	return out.String(), errOut.String(), code
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
const configNoManagedYAML = configYAML + "manage_license_file: false\n"

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
		{"config", "commit.gpgsign", "false"},
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

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v failed:\n%s", args, out)
	return strings.TrimSpace(string(out))
}

func jsonFileEntry(t *testing.T, out, path string) map[string]any {
	t.Helper()
	var raw map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &raw))
	files, ok := raw["files"].([]any)
	require.True(t, ok)
	for _, file := range files {
		entry, ok := file.(map[string]any)
		require.True(t, ok)
		if entry["path"] == path {
			return entry
		}
	}
	t.Fatalf("no JSON file entry for %q in %v", path, files)
	return nil
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

func TestExecutePrintsReturnedUsageErrors(t *testing.T) {
	isolateEnv(t)
	dir := fixtureDir(t)

	stdout, stderr, code := executeRoot(t, "audit", dir, "--format", "xml", "--deps=false")

	assert.Equal(t, 2, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, `unknown format "xml"`)
}

func TestExecuteReturnsZeroOnSuccess(t *testing.T) {
	isolateEnv(t)
	stdout, stderr, code := executeRoot(t, "version")

	assert.Equal(t, 0, code)
	assert.Contains(t, stdout, "license-tool 1.2.3")
	assert.Empty(t, stderr)
}

func TestExecuteMapsUntypedCobraErrors(t *testing.T) {
	isolateEnv(t)
	stdout, stderr, code := executeRoot(t, "bogus")

	assert.Equal(t, 2, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, `unknown command "bogus"`)
}

func TestExecuteMapsWriteRefusals(t *testing.T) {
	isolateEnv(t)
	dir := t.TempDir()
	writeFile(t, dir, ".license-tool.yaml", configYAML)
	writeFile(t, dir, "main.go", "package main\n")

	stdout, stderr, code := executeRoot(t, "apply", dir, "--write")

	assert.Equal(t, 3, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "non-git directory without --force")
}

func TestExecuteMapsInternalErrors(t *testing.T) {
	isolateEnv(t)
	missing := filepath.Join(t.TempDir(), "does-not-exist")

	stdout, stderr, code := executeRoot(t, "check", missing, "--deps=false")

	assert.Equal(t, 4, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "enumerate")
}

func TestWithExitCodeNilError(t *testing.T) {
	assert.NoError(t, withExitCode(exitUsage, nil))
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

	t.Run("tool config file is excluded from source coverage", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, ".license-tool.yaml", configYAML)
		writeFile(t, dir, "main.go", "package main\n\nfunc main() {}\n")

		out, err := runRoot(t, "audit", dir, "--format", "json", "--deps=false")
		require.NoError(t, err)

		var got struct {
			Findings struct {
				SourceTotal   int `json:"sourceTotal"`
				SourceMissing int `json:"sourceMissing"`
			} `json:"findings"`
		}
		require.NoError(t, json.Unmarshal([]byte(out), &got))
		assert.Equal(t, 1, got.Findings.SourceTotal, "only main.go is coverable source; the tool's own config is not")
		assert.Equal(t, 1, got.Findings.SourceMissing, "the headerless config file must not inflate sourceMissing")

		entry := jsonFileEntry(t, out, ".license-tool.yaml")
		assert.Equal(t, true, entry["skipped"], ".license-tool.yaml should be skipped, not counted as source")
		assert.Equal(t, "tool config", entry["skipReason"])
		// Detection and policy never run on the skipped config, so its declared license
		// does not leak into the report as a detected header or a violation.
		assert.Equal(t, false, entry["hasHeader"], "no header is detected on the skipped config")
		assert.NotContains(t, entry, "spdxId", "the config's declared license must not surface as a detected id")
		assert.NotContains(t, entry, "violations", "the skipped config carries no policy violations")
	})

	t.Run("tool config is excluded when nested and under --only", func(t *testing.T) {
		// Basename match excludes the config at any depth, and a headerless config must
		// never leak into the --only=missing problem list.
		dir := t.TempDir()
		writeFile(t, dir, ".license-tool.yaml", configYAML)
		writeFile(t, dir, filepath.Join("sub", ".license-tool.yaml"), configYAML)
		writeFile(t, dir, "main.go", "package main\n\nfunc main() {}\n")

		out, err := runRoot(t, "audit", dir, "--format", "json", "--deps=false", "--only", "missing")
		require.NoError(t, err)

		var got struct {
			Findings struct {
				SourceTotal int `json:"sourceTotal"`
			} `json:"findings"`
			Files []map[string]any `json:"files"`
		}
		require.NoError(t, json.Unmarshal([]byte(out), &got))
		assert.Equal(t, 1, got.Findings.SourceTotal, "both root and nested config are excluded; only main.go counts")

		// The --only=missing listing lists the headerless main.go but neither config.
		listed := map[string]bool{}
		for _, f := range got.Files {
			listed[f["path"].(string)] = true
		}
		assert.True(t, listed["main.go"], "the headerless main.go is a missing-header problem file")
		assert.False(t, listed[".license-tool.yaml"], "the root config must not appear in --only=missing")
		assert.False(t, listed[filepath.ToSlash(filepath.Join("sub", ".license-tool.yaml"))], "the nested config must not appear in --only=missing")
	})

	t.Run("output file", func(t *testing.T) {
		dir := fixtureDir(t)
		outPath := filepath.Join(dir, "audit.json")
		out, err := runRoot(t, "audit", dir, "--format", "json", "--output", outPath, "--deps=false")
		require.NoError(t, err)
		assert.Empty(t, out)

		data, rerr := os.ReadFile(outPath)
		require.NoError(t, rerr)
		assert.Contains(t, string(data), `"schema": "license-tool/report/v1"`)
	})

	t.Run("output file create error is internal error", func(t *testing.T) {
		dir := fixtureDir(t)
		outPath := filepath.Join(dir, "missing", "audit.json")
		stdout, stderr, code := executeRoot(t, "audit", dir, "--format", "json", "--output", outPath, "--deps=false")
		assert.Equal(t, 4, code)
		assert.Empty(t, stdout)
		assert.Contains(t, stderr, "create output")
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

	t.Run("no-deps alias skips dependency resolution", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, ".license-tool.yaml", configYAML)
		writeFile(t, dir, "package.json", `{"name":"x","dependencies":{"left-pad":"^1.0.0"}}`)
		out, err := runRoot(t, "audit", dir, "--format", "json", "--no-deps")
		require.NoError(t, err)
		assert.Contains(t, out, `"dependencies": []`)
		assert.NotContains(t, out, "left-pad")
	})

	t.Run("invalid resolver tier is usage error", func(t *testing.T) {
		dir := fixtureDir(t)
		stdout, stderr, code := executeRoot(t, "audit", dir, "--resolve-deps", "typo", "--deps=false")
		assert.Equal(t, 2, code)
		assert.Empty(t, stdout)
		assert.Contains(t, stderr, `unknown dependency resolver tier "typo"`)
	})

	t.Run("deps resolution discovers nested manifests", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, ".license-tool.yaml", configYAML)
		writeFile(t, dir, filepath.Join("services", "api", "package.json"), `{"name":"api","dependencies":{"left-pad":"1.3.0"}}`)
		writeFile(t, dir, filepath.Join("services", "api", "node_modules", "left-pad", "package.json"), `{"name":"left-pad","version":"1.3.0","license":"MIT"}`)

		out, err := runRoot(t, "audit", dir, "--format", "json")
		require.NoError(t, err)

		var got struct {
			Dependencies []struct {
				Ecosystem  string `json:"ecosystem"`
				Name       string `json:"name"`
				Version    string `json:"version"`
				SPDXID     string `json:"spdxId"`
				Resolution string `json:"resolution"`
			} `json:"dependencies"`
		}
		require.NoError(t, json.Unmarshal([]byte(out), &got))
		require.Len(t, got.Dependencies, 1)
		assert.Equal(t, "npm", got.Dependencies[0].Ecosystem)
		assert.Equal(t, "left-pad", got.Dependencies[0].Name)
		assert.Equal(t, "1.3.0", got.Dependencies[0].Version)
		assert.Equal(t, "MIT", got.Dependencies[0].SPDXID)
		assert.Equal(t, "resolved", got.Dependencies[0].Resolution)
	})

	t.Run("gradle tool tier reports unsupported resolver", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, ".license-tool.yaml", configYAML)
		writeFile(t, dir, "build.gradle", `dependencies { implementation 'com.google.guava:guava:31.1-jre' }`)

		out, err := runRoot(t, "audit", dir, "--format", "json", "--resolve-deps", "tool")
		require.NoError(t, err)

		var got struct {
			Dependencies []struct {
				Name   string `json:"name"`
				Reason string `json:"reason"`
			} `json:"dependencies"`
		}
		require.NoError(t, json.Unmarshal([]byte(out), &got))
		require.Len(t, got.Dependencies, 1)
		assert.Equal(t, "com.google.guava:guava", got.Dependencies[0].Name)
		assert.Contains(t, got.Dependencies[0].Reason, "Gradle tool-tier dependency-license resolution is not supported")
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

	t.Run("tool config does not trigger a missing-header check failure", func(t *testing.T) {
		// The config is headerless YAML; under the default fail_on (which includes
		// missing-header) it would fail check if counted as source. Excluding it lets a
		// repo whose only other file is properly headered pass. This guards the audit
		// pipeline's config exclusion against regression on the check exit code.
		dir := t.TempDir()
		writeFile(t, dir, ".license-tool.yaml", configYAML)
		writeFile(t, dir, "main.go", "/* SPDX-License-Identifier: AGPL-3.0-or-later */\n\npackage main\n")
		_, err := runRoot(t, "check", dir, "--deps=false")
		require.NoError(t, err, "the tool's own config must not cause a missing-header check failure")
	})

	t.Run("fail-on flag overrides check policy", func(t *testing.T) {
		dir := fixtureDir(t)
		out, err := runRoot(t, "check", dir, "--fail-on", "unresolved-dependency", "--deps=false")
		require.NoError(t, err)
		assert.Contains(t, out, "result: PASS")
	})

	t.Run("json output file", func(t *testing.T) {
		dir := fixtureDir(t)
		outPath := filepath.Join(dir, "check.json")
		out, err := runRoot(t, "check", dir, "--format", "json", "--output", outPath, "--fail-on", "unresolved-dependency", "--deps=false")
		require.NoError(t, err)
		assert.Empty(t, out)

		data, rerr := os.ReadFile(outPath)
		require.NoError(t, rerr)
		assert.Contains(t, string(data), `"schema": "license-tool/report/v1"`)
		assert.Contains(t, string(data), `"passed": true`)
	})

	t.Run("invalid fail-on flag is usage error", func(t *testing.T) {
		dir := fixtureDir(t)
		stdout, stderr, code := executeRoot(t, "check", dir, "--fail-on", "bogus", "--deps=false")
		assert.Equal(t, 2, code)
		assert.Empty(t, stdout)
		assert.Contains(t, stderr, `unknown fail-on condition "bogus"`)
	})

	t.Run("output file create error is internal error", func(t *testing.T) {
		dir := fixtureDir(t)
		outPath := filepath.Join(dir, "missing", "check.json")
		stdout, stderr, code := executeRoot(t, "check", dir, "--output", outPath, "--fail-on", "unresolved-dependency", "--deps=false")
		assert.Equal(t, 4, code)
		assert.Empty(t, stdout)
		assert.Contains(t, stderr, "create output")
	})

	t.Run("invalid resolver tier is usage error", func(t *testing.T) {
		dir := fixtureDir(t)
		stdout, stderr, code := executeRoot(t, "check", dir, "--resolve-deps", "typo", "--fail-on", "unresolved-dependency", "--deps=false")
		assert.Equal(t, 2, code)
		assert.Empty(t, stdout)
		assert.Contains(t, stderr, `unknown dependency resolver tier "typo"`)
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
		// so the top-level executor should exit 1 after rendering the report.
		dir := os.Getenv("LICENSE_TOOL_CHECK_EXIT_DIR")
		root := newRootCmd(testBuildInfo)
		root.SetArgs([]string{"check", dir, "--deps=false"})
		os.Exit(execute(root))
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
	assert.Contains(t, string(out), "license-tool audit report")
	assert.Contains(t, string(out), "result: FAIL")
}

func TestDependencyManifestClassifier(t *testing.T) {
	ft, ok := dependencyManifestClassifier(filepath.Join("services", "api", "package.json"))
	require.True(t, ok)
	assert.Equal(t, dependencyManifestFileType, ft)

	ft, ok = dependencyManifestClassifier("main.go")
	assert.False(t, ok)
	assert.Equal(t, model.FileType{}, ft)
}

func TestDependencyManifestDirs(t *testing.T) {
	root := filepath.Join(string(os.PathSeparator), "repo")
	entries := []enumerate.Entry{
		{Path: "package.json"},
		{Path: "services/api/package.json"},
		{Path: "services/api/build.gradle"},
		{Path: "ignored/package.json", Skip: true},
	}

	assert.Equal(t, []string{
		root,
		filepath.Join(root, "services", "api"),
	}, dependencyManifestDirs(root, entries))
}

func TestResolveDependencyManifestsHonorsIgnoreAndPrunesHeavyDirs(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".gitignore", "ignored/\n")
	writeFile(t, dir, filepath.Join("services", "api", "package.json"), `{"name":"api","dependencies":{"left-pad":"1.3.0"}}`)
	writeFile(t, dir, filepath.Join("services", "api", "node_modules", "left-pad", "package.json"), `{"name":"left-pad","version":"1.3.0","license":"MIT"}`)
	writeFile(t, dir, filepath.Join("ignored", "package.json"), `{"name":"ignored","dependencies":{"ignored-lib":"1.0.0"}}`)
	writeFile(t, dir, filepath.Join("node_modules", "scanned-if-not-pruned", "package.json"), `{"name":"scanned-if-not-pruned","dependencies":{"bad":"1.0.0"}}`)

	deps, err := resolveDependencyManifests(dir, nil, false, false)
	require.NoError(t, err)
	require.Len(t, deps, 1)
	assert.Equal(t, "left-pad", deps[0].Name)
	assert.Equal(t, "MIT", deps[0].SPDXID)
}

func TestResolveDependencyManifestsMissingRoot(t *testing.T) {
	deps, err := resolveDependencyManifests(filepath.Join(t.TempDir(), "missing"), nil, false, false)
	require.Error(t, err)
	assert.Nil(t, deps)
}

func TestApplyCommand(t *testing.T) {
	isolateEnv(t)

	t.Run("dry-run reports without writing", func(t *testing.T) {
		dir := fixtureDir(t)
		initGitRepo(t, dir)
		out, err := runRoot(t, "apply", dir)
		require.NoError(t, err)
		assert.Contains(t, out, "license-tool audit report")
		assert.Contains(t, out, "@@")
		assert.Contains(t, out, "+  SPDX-License-Identifier: AGPL-3.0-or-later")

		// Dry run leaves the source untouched.
		content, rerr := os.ReadFile(filepath.Join(dir, "main.go"))
		require.NoError(t, rerr)
		assert.NotContains(t, string(content), "GNU Affero")
	})

	t.Run("dry-run json includes unified diff", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, ".license-tool.yaml", configNoManagedYAML)
		writeFile(t, dir, "main.go", "package main\n")
		initGitRepo(t, dir)

		out, err := runRoot(t, "apply", dir, "--include", "main.go", "--format", "json")
		require.NoError(t, err)

		main := jsonFileEntry(t, out, "main.go")
		diff, ok := main["diff"].(string)
		require.True(t, ok)
		assert.Contains(t, diff, "+++ Go")
		assert.Contains(t, diff, "+  SPDX-License-Identifier: AGPL-3.0-or-later")
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

	t.Run("write json omits unified diff", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, ".license-tool.yaml", configNoManagedYAML)
		writeFile(t, dir, "main.go", "package main\n")
		initGitRepo(t, dir)

		out, err := runRoot(t, "apply", dir, "--write", "--include", "main.go", "--format", "json")
		require.NoError(t, err)

		main := jsonFileEntry(t, out, "main.go")
		assert.NotContains(t, main, "diff")
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

	t.Run("bad format", func(t *testing.T) {
		dir := fixtureDir(t)
		_, err := runRoot(t, "apply", dir, "--format", "xml")
		require.Error(t, err)
		assert.Contains(t, err.Error(), `unknown format "xml"`)
	})

	t.Run("valid but non-curated license rejected before apply", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "main.go", "package main\n")
		_, err := runRoot(t, "apply", dir, "--license", "Zlib", "--holder", "Acme")
		require.Error(t, err)
		assert.Contains(t, err.Error(), `"Zlib"`)
		assert.Contains(t, err.Error(), "cannot render")
		assert.NotContains(t, err.Error(), "unknown license")
	})

	t.Run("apply error: write in non-git dir without force", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, ".license-tool.yaml", configYAML)
		writeFile(t, dir, "main.go", "package main\n")
		_, err := runRoot(t, "apply", dir, "--write")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "non-git directory without --force")
	})

	t.Run("write honors include scope", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, ".license-tool.yaml", configNoManagedYAML)
		writeFile(t, dir, "included.go", "package included\n")
		writeFile(t, dir, "ignored.go", "package ignored\n")
		initGitRepo(t, dir)

		_, err := runRoot(t, "apply", dir, "--write", "--include", "included.go")
		require.NoError(t, err)

		included, rerr := os.ReadFile(filepath.Join(dir, "included.go"))
		require.NoError(t, rerr)
		assert.Contains(t, string(included), "SPDX-License-Identifier: AGPL-3.0-or-later")

		ignored, rerr := os.ReadFile(filepath.Join(dir, "ignored.go"))
		require.NoError(t, rerr)
		assert.NotContains(t, string(ignored), "SPDX-License-Identifier")
	})

	t.Run("allow-dirty commit leaves unrelated changes uncommitted", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, ".license-tool.yaml", configNoManagedYAML)
		writeFile(t, dir, "main.go", "package main\n")
		writeFile(t, dir, "unrelated.go", "package unrelated\n")
		initGitRepo(t, dir)
		writeFile(t, dir, "unrelated.go", "package unrelated\n\nvar Dirty = true\n")

		_, err := runRoot(t, "apply", dir,
			"--write",
			"--allow-dirty",
			"--commit",
			"--commit-message", "chore: scoped license update",
			"--include", "main.go",
		)
		require.NoError(t, err)

		assert.Equal(t, "chore: scoped license update", gitOutput(t, dir, "log", "-1", "--format=%s"))
		changedInCommit := gitOutput(t, dir, "show", "--name-only", "--format=", "HEAD")
		assert.Contains(t, changedInCommit, "main.go")
		assert.NotContains(t, changedInCommit, "unrelated.go")
		assert.Contains(t, gitOutput(t, dir, "status", "--porcelain"), "unrelated.go")
	})

	t.Run("valid but non-curated license maps to usage error", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "main.go", "package main\n")
		stdout, stderr, code := executeRoot(t, "apply", dir, "--license", "Zlib", "--holder", "Acme")
		assert.Equal(t, 2, code)
		assert.Empty(t, stdout)
		assert.Contains(t, stderr, `"Zlib"`)
		assert.Contains(t, stderr, "cannot render")
		assert.NotContains(t, stderr, "unknown license")
	})

	t.Run("render error is internal error", func(t *testing.T) {
		dir := fixtureDir(t)
		root := newRootCmd(testBuildInfo)
		root.SetOut(errorWriter{err: errors.New("write failed")})
		root.SetArgs([]string{"apply", dir})
		err := root.Execute()
		require.Error(t, err)
		assert.Equal(t, exitInternal, exitCode(err))
		assert.Contains(t, err.Error(), "write failed")
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
		assert.Contains(t, out, "+++ LICENSE")
		assert.Contains(t, out, "+GNU AFFERO GENERAL PUBLIC LICENSE")
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

	t.Run("write in non-git dir without force is refused", func(t *testing.T) {
		dir := fixtureDir(t)
		_, err := runRoot(t, "license", dir, "--write")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "non-git directory without --force")
		_, statErr := os.Stat(filepath.Join(dir, "LICENSE"))
		assert.True(t, os.IsNotExist(statErr))
	})

	t.Run("write in dirty git tree without allow-dirty is refused", func(t *testing.T) {
		dir := fixtureDir(t)
		initGitRepo(t, dir)
		writeFile(t, dir, "main.go", "package main\n\nvar Dirty = true\n")

		_, err := runRoot(t, "license", dir, "--write")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "dirty git tree")
		_, statErr := os.Stat(filepath.Join(dir, "LICENSE"))
		assert.True(t, os.IsNotExist(statErr))
	})

	t.Run("commit flags create scoped license-file commit", func(t *testing.T) {
		dir := fixtureDir(t)
		initGitRepo(t, dir)

		_, err := runRoot(t, "license", dir, "--write", "--commit", "--commit-message", "chore: add license files")
		require.NoError(t, err)

		assert.Equal(t, "chore: add license files", gitOutput(t, dir, "log", "-1", "--format=%s"))
		changedInCommit := gitOutput(t, dir, "show", "--name-only", "--format=", "HEAD")
		assert.Contains(t, changedInCommit, "LICENSE")
		assert.Contains(t, changedInCommit, filepath.ToSlash(filepath.Join("LICENSES", "AGPL-3.0-or-later.txt")))
		assert.Empty(t, gitOutput(t, dir, "status", "--porcelain"))
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

	t.Run("valid but non-curated license rejected before rendering", func(t *testing.T) {
		// Zlib is a real SPDX id, but this write path needs a license template the
		// tool can render. The command must fail during config resolution rather than
		// reaching ManageLicenseFiles and surfacing an unknown-license render error.
		dir := t.TempDir()
		writeFile(t, dir, "main.go", "package main\n")
		_, err := runRoot(t, "license", dir, "--license", "Zlib", "--holder", "Acme")
		require.Error(t, err)
		assert.Contains(t, err.Error(), `"Zlib"`)
		assert.Contains(t, err.Error(), "cannot render")
		assert.NotContains(t, err.Error(), "unknown license")
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

	t.Run("valid but non-curated license rejected before scaffold", func(t *testing.T) {
		dir := t.TempDir()
		_, err := runRoot(t, "init", dir, "--license", "Zlib", "--holder", "Acme")
		require.Error(t, err)
		assert.Contains(t, err.Error(), `"Zlib"`)
		assert.Contains(t, err.Error(), "cannot render")
		_, statErr := os.Stat(filepath.Join(dir, ".license-tool.yaml"))
		assert.True(t, os.IsNotExist(statErr), "unsupported render license must not be scaffolded")
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

func TestParseFailOnFlags(t *testing.T) {
	got, err := parseFailOnFlags([]string{"missing-header, unknown-license", "policy-violation", "unresolved-dependency"})
	require.NoError(t, err)
	assert.Equal(t, []model.FailCondition{
		model.FailOnMissingHeader,
		model.FailOnUnknownLicense,
		model.FailOnPolicyViolation,
		model.FailOnUnresolvedDependency,
	}, got)

	_, err = parseFailOnFlags([]string{"missing-header", "bogus"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown fail-on condition "bogus"`)
}

func TestRenderCommandReportErrors(t *testing.T) {
	root := newRootCmd(testBuildInfo)
	var out bytes.Buffer
	root.SetOut(&out)

	err := renderCommandReport(root, "", model.Report{}, report.Format(99), report.RenderOptions{})
	require.Error(t, err)
	assert.Equal(t, exitInternal, exitCode(err))
	assert.Empty(t, out.String())

	err = renderCommandReport(root, filepath.Join(t.TempDir(), "report.txt"), model.Report{}, report.Format(99), report.RenderOptions{})
	require.Error(t, err)
	assert.Equal(t, exitInternal, exitCode(err))

	orig := createReportFile
	createReportFile = func(string) (io.WriteCloser, error) {
		return closeErrorWriter{closeErr: errors.New("close failed")}, nil
	}
	t.Cleanup(func() { createReportFile = orig })

	err = renderCommandReport(root, filepath.Join(t.TempDir(), "report.txt"), model.Report{}, report.FormatText, report.RenderOptions{})
	require.Error(t, err)
	assert.Equal(t, exitInternal, exitCode(err))
	assert.Contains(t, err.Error(), "close failed")
}

func TestWriteOrInternalErrorClassifiesUnexpectedErrors(t *testing.T) {
	err := writeOrInternalError(errors.New("disk full"))
	require.Error(t, err)
	assert.Equal(t, exitInternal, exitCode(err))
}

func TestAuditSummaryOmitsFileList(t *testing.T) {
	isolateEnv(t)
	dir := fixtureDir(t)
	out, err := runRoot(t, "audit", dir, "--deps=false", "--summary")
	require.NoError(t, err)
	assert.Contains(t, out, "by SPDX id:")
	assert.NotContains(t, out, "\nsource files:")
}

func TestAuditGroupByLicense(t *testing.T) {
	isolateEnv(t)
	dir := fixtureDir(t)
	out, err := runRoot(t, "audit", dir, "--deps=false", "--group-by", "license")
	require.NoError(t, err)
	assert.Contains(t, out, "source files by license:")
}

func TestAuditGroupByUnknownIsUsageError(t *testing.T) {
	isolateEnv(t)
	dir := fixtureDir(t)
	stdout, stderr, code := executeRoot(t, "audit", dir, "--deps=false", "--group-by", "bogus")
	assert.Equal(t, 2, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "unknown group-by dimension")
}

func TestCheckGroupByUnknownIsUsageError(t *testing.T) {
	isolateEnv(t)
	dir := fixtureDir(t)
	stdout, stderr, code := executeRoot(t, "check", dir, "--deps=false", "--group-by", "bogus")
	assert.Equal(t, 2, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "unknown group-by dimension")
}

func TestParseSort(t *testing.T) {
	for _, tok := range []string{"", "key"} {
		got, err := parseSort(tok)
		require.NoError(t, err)
		assert.False(t, got)
	}
	got, err := parseSort("count")
	require.NoError(t, err)
	assert.True(t, got)

	_, err = parseSort("bogus")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown sort")
}

func TestAuditSortAndUnknownSort(t *testing.T) {
	isolateEnv(t)
	dir := fixtureDir(t)

	_, err := runRoot(t, "audit", dir, "--deps=false", "--sort", "count")
	require.NoError(t, err)

	_, stderr, code := executeRoot(t, "audit", dir, "--deps=false", "--sort", "bogus")
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr, "unknown sort")
}

func TestLicenseSelectOptions(t *testing.T) {
	opts := licenseSelectOptions()
	require.NotEmpty(t, opts, "license picker should offer renderable licenses")

	values := make([]string, 0, len(opts))
	for _, opt := range opts {
		values = append(values, opt.Value)
	}
	assert.Contains(t, values, "MIT")
	assert.NotContains(t, values, "Zlib", "picker must not offer a license the tool cannot render")
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

	// WHY /dev/null must NOT be a TTY: the previous os.ModeCharDevice check treated
	// every character device (including /dev/null) as a terminal, so 'init </dev/null'
	// and CI wrongly entered the interactive wizard. term.IsTerminal issues the real
	// terminal ioctl, so a char device that is not a terminal is correctly rejected.
	t.Run("character device is not a TTY", func(t *testing.T) {
		f, err := os.Open(os.DevNull)
		require.NoError(t, err)
		t.Cleanup(func() { _ = f.Close() })
		os.Stdin = f
		assert.False(t, isTTY())
	})

	t.Run("regular file is not a TTY", func(t *testing.T) {
		dir := t.TempDir()
		f, err := os.Create(filepath.Join(dir, "stdin.txt"))
		require.NoError(t, err)
		t.Cleanup(func() { _ = f.Close() })
		os.Stdin = f
		assert.False(t, isTTY())
	})

	t.Run("closed descriptor is not a TTY", func(t *testing.T) {
		// A closed descriptor cannot be a terminal; the ioctl fails and IsTerminal
		// reports false.
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

type errorWriter struct {
	err error
}

func (w errorWriter) Write([]byte) (int, error) {
	return 0, w.err
}

type closeErrorWriter struct {
	closeErr error
}

func (w closeErrorWriter) Write(p []byte) (int, error) {
	return len(p), nil
}

func (w closeErrorWriter) Close() error {
	return w.closeErr
}

func TestAuditOnlyFilter(t *testing.T) {
	isolateEnv(t)
	dir := fixtureDir(t) // headerless main.go
	out, err := runRoot(t, "audit", dir, "--deps=false", "--only", "missing")
	require.NoError(t, err)
	assert.Contains(t, out, "main.go")

	_, stderr, code := executeRoot(t, "audit", dir, "--deps=false", "--only", "bogus")
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr, "unknown --only filter")
}
