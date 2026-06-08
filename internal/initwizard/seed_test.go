package initwizard

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/KofTwentyTwo/license-tool/internal/config"
	"github.com/KofTwentyTwo/license-tool/internal/model"
	"github.com/KofTwentyTwo/license-tool/internal/render"
	"github.com/KofTwentyTwo/license-tool/internal/spdx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedWriteFile(t *testing.T, root, rel, content string) {
	t.Helper()
	abs := filepath.Join(root, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
	require.NoError(t, os.WriteFile(abs, []byte(content), 0o644))
}

// seedWriteHeadered writes a Go file carrying a real managed header for licID/holder,
// built through the same render path apply uses so detect is guaranteed to recognize
// it. WHY round-trip through render: it ties the test to the tool's actual header
// shape rather than a hand-rolled approximation that could drift from detect.
func seedWriteHeadered(t *testing.T, root, rel, pkg, licID, holder string) {
	t.Helper()
	ft, ok := config.LookupFunc(config.Defaults())(rel)
	require.True(t, ok)
	lic, ok := spdx.Lookup(licID)
	require.True(t, ok)
	hdr, err := render.Header(render.HeaderInput{
		License:  lic,
		Holder:   holder,
		Year:     "2024",
		Style:    model.StyleReusePlusNotice,
		FileType: ft,
	})
	require.NoError(t, err)
	content, _ := render.Insert([]byte(pkg), ft, hdr)
	seedWriteFile(t, root, rel, string(content))
}

func noAuthor(string) string  { return "" }
func gitPerson(string) string { return "Git Person" }

func TestSeedFromHeaders(t *testing.T) {
	root := t.TempDir()
	seedWriteHeadered(t, root, "a.go", "package a\n", "MIT", "Acme, LLC")
	seedWriteHeadered(t, root, "b.go", "package b\n", "MIT", "Acme, LLC")

	got, detected := Seed(root, Answers{}, SeedDeps{GitAuthor: gitPerson})

	assert.Equal(t, "MIT", got.License.SPDXID)
	assert.Equal(t, "Acme, LLC", got.Identity.Holder)
	assert.True(t, detected.License)
	assert.True(t, detected.Holder)
}

func TestSeedTieBreaksLexically(t *testing.T) {
	root := t.TempDir()
	seedWriteHeadered(t, root, "a.go", "package a\n", "MIT", "Bravo Co")
	seedWriteHeadered(t, root, "b.go", "package b\n", "Apache-2.0", "Alpha Co")

	got, detected := Seed(root, Answers{}, SeedDeps{GitAuthor: noAuthor})

	// 1-1 ties resolve to the lexically-first key for determinism.
	assert.Equal(t, "Apache-2.0", got.License.SPDXID)
	assert.Equal(t, "Alpha Co", got.Identity.Holder)
	assert.True(t, detected.License)
	assert.True(t, detected.Holder)
}

func TestSeedGitAuthorFallback(t *testing.T) {
	root := t.TempDir()
	// A plain source file with no header exercises detect's not-present path.
	seedWriteFile(t, root, "main.go", "package main\n")

	got, detected := Seed(root, Answers{}, SeedDeps{GitAuthor: gitPerson})

	assert.Equal(t, "Git Person", got.Identity.Holder)
	assert.True(t, detected.Holder)
	assert.Empty(t, got.License.SPDXID)
	assert.False(t, detected.License)
}

func TestSeedNoDetection(t *testing.T) {
	root := t.TempDir()
	seedWriteFile(t, root, "main.go", "package main\n")

	got, detected := Seed(root, Answers{}, SeedDeps{GitAuthor: noAuthor})

	assert.Empty(t, got.License.SPDXID)
	assert.Empty(t, got.Identity.Holder)
	assert.Equal(t, Detected{}, detected)
}

func TestSeedDoesNotOverridePreset(t *testing.T) {
	root := t.TempDir()
	seedWriteHeadered(t, root, "a.go", "package a\n", "MIT", "Acme, LLC")

	base := Answers{
		License:  LicenseAnswer{SPDXID: "GPL-3.0-or-later"},
		Identity: IdentityAnswer{Holder: "Preset Holder"},
	}
	got, detected := Seed(root, base, SeedDeps{GitAuthor: gitPerson})

	assert.Equal(t, "GPL-3.0-or-later", got.License.SPDXID)
	assert.Equal(t, "Preset Holder", got.Identity.Holder)
	assert.False(t, detected.License)
	assert.False(t, detected.Holder)
}

func TestSeedUnrenderableLicenseNotSeeded(t *testing.T) {
	root := t.TempDir()
	// A real SPDX id the tool does not ship rendering for; detect reads the tag, but
	// Seed must not adopt a license it cannot render.
	seedWriteFile(t, root, "x.go", "// SPDX-License-Identifier: Glide\n\npackage x\n")

	got, detected := Seed(root, Answers{}, SeedDeps{GitAuthor: noAuthor})

	assert.Empty(t, got.License.SPDXID)
	assert.False(t, detected.License)
}

func TestSeedDetectsLicenseFile(t *testing.T) {
	t.Run("LICENSE file present", func(t *testing.T) {
		root := t.TempDir()
		seedWriteFile(t, root, "LICENSE", "MIT License\n")
		_, detected := Seed(root, Answers{}, SeedDeps{GitAuthor: noAuthor})
		assert.True(t, detected.Manage)
	})

	t.Run("LICENSES directory present", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, "LICENSES"), 0o755))
		_, detected := Seed(root, Answers{}, SeedDeps{GitAuthor: noAuthor})
		assert.True(t, detected.Manage)
	})
}

func TestSeedReadErrorSkipsFile(t *testing.T) {
	root := t.TempDir()
	seedWriteHeadered(t, root, "a.go", "package a\n", "MIT", "Acme, LLC")

	orig := seedReadFile
	seedReadFile = func(string) ([]byte, error) { return nil, errors.New("boom") }
	defer func() { seedReadFile = orig }()

	got, detected := Seed(root, Answers{}, SeedDeps{GitAuthor: noAuthor})

	assert.Empty(t, got.License.SPDXID)
	assert.False(t, detected.License)
}

func TestSeedEnumerateErrorIsSafe(t *testing.T) {
	got, detected := Seed(filepath.Join(t.TempDir(), "does-not-exist"), Answers{}, SeedDeps{GitAuthor: noAuthor})
	assert.Empty(t, got.License.SPDXID)
	assert.Empty(t, got.Identity.Holder)
	assert.Equal(t, Detected{}, detected)
}

// TestGitAuthorErrorIsEmpty covers gitAuthor's failure path: git cannot chdir into a
// nonexistent directory, so it exits non-zero and the helper yields "".
func TestGitAuthorErrorIsEmpty(t *testing.T) {
	assert.Empty(t, gitAuthor(filepath.Join(t.TempDir(), "missing", "deeper")))
}

// TestSeedDefaultGitAuthor exercises the default seam (deps.GitAuthor nil) against a
// real repo whose author is configured, covering the nil-default branch and the real
// gitAuthor helper.
func TestSeedDefaultGitAuthor(t *testing.T) {
	root := t.TempDir()
	for _, args := range [][]string{
		{"init"},
		{"config", "user.name", "Repo Author"},
		{"config", "user.email", "repo@example.com"},
	} {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		require.NoError(t, cmd.Run())
	}
	seedWriteFile(t, root, "main.go", "package main\n")

	got, detected := Seed(root, Answers{}, SeedDeps{})
	assert.Equal(t, "Repo Author", got.Identity.Holder)
	assert.True(t, detected.Holder)
}
