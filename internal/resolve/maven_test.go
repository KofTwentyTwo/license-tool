package resolve

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/KofTwentyTwo/license-tool/internal/model"
)

// writeDepPOM writes a resolved dependency POM into a fixture local repository at
// the maven layout path repo/<group-as-dirs>/<artifact>/<version>/<artifact>-<version>.pom.
func writeDepPOM(t *testing.T, repo, group, artifact, version, body string) {
	t.Helper()
	dir := filepath.Join(repo, filepath.Join(strings.Split(group, ".")...), artifact, version)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	pom := filepath.Join(dir, artifact+"-"+version+".pom")
	require.NoError(t, os.WriteFile(pom, []byte(body), 0o644))
}

// pomWithLicense builds a dependency POM declaring the given license name.
func pomWithLicense(name string) string {
	return `<project><licenses><license><name>` + name + `</name></license></licenses></project>`
}

func TestMavenEcosystemAndDetect(t *testing.T) {
	r := &MavenResolver{}
	assert.Equal(t, "maven", r.Ecosystem())

	dir := t.TempDir()
	assert.False(t, r.Detect(dir))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pom.xml"), []byte("<project/>"), 0o644))
	assert.True(t, r.Detect(dir))
}

// TestMavenResolveParseError verifies a missing/unreadable project pom.xml is a
// hard error (the project's own manifest must parse).
func TestMavenResolveParseError(t *testing.T) {
	dir := t.TempDir()
	// No pom.xml present -> os.ReadFile error wrapped by parsePOM.
	r := &MavenResolver{}
	_, err := r.Resolve(dir, model.ResolveOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "maven: read")

	// Malformed XML -> xml.Unmarshal error.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pom.xml"), []byte("<project><dependencies>"), 0o644))
	_, err = r.Resolve(dir, model.ResolveOptions{})
	require.Error(t, err)
}

// TestMavenResolveOnDisk drives Resolve through every on-disk outcome: a resolved
// license, and the several unresolved reasons, using a fixture local repository.
func TestMavenResolveOnDisk(t *testing.T) {
	repo := t.TempDir()

	// Dependency with a recognized license on disk.
	writeDepPOM(t, repo, "com.example", "good", "1.0.0", pomWithLicense("Apache-2.0"))
	// Dependency whose POM declares a license we cannot normalize.
	writeDepPOM(t, repo, "com.example", "weird", "1.0.0", pomWithLicense("Some Weird License"))
	// Dependency whose POM declares an empty <name>.
	writeDepPOM(t, repo, "com.example", "emptyname", "1.0.0",
		`<project><licenses><license><name>  </name></license></licenses></project>`)
	// Dependency whose POM declares no <licenses>.
	writeDepPOM(t, repo, "com.example", "nolicense", "1.0.0", `<project></project>`)
	// Dependency whose POM on disk is malformed.
	writeDepPOM(t, repo, "com.example", "broken", "1.0.0", `<project><licenses>`)

	projectPOM := `<project>
  <groupId>com.example</groupId>
  <artifactId>root</artifactId>
  <version>9.9.9</version>
  <dependencies>
    <dependency><groupId>com.example</groupId><artifactId>good</artifactId><version>1.0.0</version></dependency>
    <dependency><groupId>com.example</groupId><artifactId>weird</artifactId><version>1.0.0</version></dependency>
    <dependency><groupId>com.example</groupId><artifactId>emptyname</artifactId><version>1.0.0</version></dependency>
    <dependency><groupId>com.example</groupId><artifactId>nolicense</artifactId><version>1.0.0</version></dependency>
    <dependency><groupId>com.example</groupId><artifactId>broken</artifactId><version>1.0.0</version></dependency>
    <dependency><groupId>com.example</groupId><artifactId>missing</artifactId><version>2.0.0</version></dependency>
    <dependency><groupId>com.other</groupId><artifactId>noversion</artifactId></dependency>
  </dependencies>
</project>`
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pom.xml"), []byte(projectPOM), 0o644))

	r := &MavenResolver{localRepo: repo}
	out, err := r.Resolve(dir, model.ResolveOptions{})
	require.NoError(t, err)

	byName := map[string]model.DependencyLicense{}
	for _, d := range out {
		byName[d.Name] = d
	}

	good := byName["com.example:good"]
	assert.Equal(t, model.ResolutionResolved, good.Resolution)
	assert.Equal(t, "Apache-2.0", good.SPDXID)
	assert.Equal(t, "1.0.0", good.Version)

	weird := byName["com.example:weird"]
	assert.Equal(t, model.ResolutionUnresolved, weird.Resolution)
	assert.Contains(t, weird.Reason, "not recognized as SPDX")

	emptyName := byName["com.example:emptyname"]
	assert.Equal(t, model.ResolutionUnresolved, emptyName.Resolution)
	assert.Contains(t, emptyName.Reason, "no usable <name>")

	noLicense := byName["com.example:nolicense"]
	assert.Equal(t, model.ResolutionUnresolved, noLicense.Resolution)
	assert.Contains(t, noLicense.Reason, "no <licenses>")

	broken := byName["com.example:broken"]
	assert.Equal(t, model.ResolutionUnresolved, broken.Resolution)
	assert.Contains(t, broken.Reason, "unreadable")

	missing := byName["com.example:missing"]
	assert.Equal(t, model.ResolutionUnresolved, missing.Resolution)
	assert.Contains(t, missing.Reason, "no resolved pom on disk")

	// A dependency with no version that does not share the project group stays
	// version-empty and is reported unresolved with the version reason.
	noVersion := byName["com.other:noversion"]
	assert.Equal(t, model.ResolutionUnresolved, noVersion.Resolution)
	assert.Contains(t, noVersion.Reason, "version unresolved")
}

// TestMavenResolveNoRepo verifies that when no local repository is available the
// dependency is reported unresolved with the no-repository reason. We force this
// by clearing HOME so os.UserHomeDir / repoDir yields no usable repo, but the
// simplest deterministic route is pointing localRepo at a path and relying on
// repoDir; here we exercise the repo=="" branch by using a resolver whose repoDir
// returns empty.
func TestMavenResolveNoRepo(t *testing.T) {
	dir := t.TempDir()
	projectPOM := `<project>
  <groupId>g</groupId><artifactId>a</artifactId><version>1</version>
  <dependencies>
    <dependency><groupId>com.x</groupId><artifactId>y</artifactId><version>1.0</version></dependency>
  </dependencies>
</project>`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pom.xml"), []byte(projectPOM), 0o644))

	// Force os.UserHomeDir to fail by unsetting HOME (and the OS-specific vars)
	// so repoDir() returns "".
	t.Setenv("HOME", "")
	if os.Getenv("USERPROFILE") != "" {
		t.Setenv("USERPROFILE", "")
	}
	r := &MavenResolver{} // no localRepo override -> repoDir consults home
	out, err := r.Resolve(dir, model.ResolveOptions{})
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, model.ResolutionUnresolved, out[0].Resolution)
	assert.Contains(t, out[0].Reason, "no local repository available")
}

// TestMavenRepoDir covers the override branch and the home-default branch.
func TestMavenRepoDir(t *testing.T) {
	r := &MavenResolver{localRepo: "/custom/repo"}
	assert.Equal(t, "/custom/repo", r.repoDir())

	home, err := os.UserHomeDir()
	require.NoError(t, err)
	def := (&MavenResolver{}).repoDir()
	assert.Equal(t, filepath.Join(home, ".m2", "repository"), def)
}

// TestMavenResolveOnDiskDirect drives resolveOnDisk directly for the repo=="" and
// version=="" guards that the higher-level Resolve test cannot easily reach in one
// table.
func TestMavenResolveOnDiskDirect(t *testing.T) {
	r := &MavenResolver{}

	id, reason := r.resolveOnDisk("", "g", "a", "1.0")
	assert.Empty(t, id)
	assert.Contains(t, reason, "no local repository available")

	id, reason = r.resolveOnDisk("/some/repo", "g", "a", "")
	assert.Empty(t, id)
	assert.Contains(t, reason, "version unresolved")
}

// TestMavenToolTier covers augmentFromTool via Resolve with an injected runner:
// a successful run that fills an unresolved dep, and a failing run that annotates.
func TestMavenToolTier(t *testing.T) {
	repo := t.TempDir() // empty: every dep is unresolved on disk
	dir := t.TempDir()
	projectPOM := `<project>
  <groupId>g</groupId><artifactId>a</artifactId><version>1</version>
  <dependencies>
    <dependency><groupId>com.google.guava</groupId><artifactId>guava</artifactId><version>31.1-jre</version></dependency>
  </dependencies>
</project>`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pom.xml"), []byte(projectPOM), 0o644))

	t.Run("runner fills unresolved", func(t *testing.T) {
		thirdParty := "    (The Apache Software License, Version 2.0) Guava (com.google.guava:guava:31.1-jre - https://github.com/google/guava)\n"
		r := &MavenResolver{
			localRepo: repo,
			mvnRunner: func(projectDir string) ([]byte, error) {
				assert.Equal(t, dir, projectDir)
				return []byte(thirdParty), nil
			},
		}
		out, err := r.Resolve(dir, model.ResolveOptions{AllowToolShellOut: true})
		require.NoError(t, err)
		require.Len(t, out, 1)
		assert.Equal(t, model.ResolutionResolved, out[0].Resolution)
		assert.Equal(t, "Apache-2.0", out[0].SPDXID)
		assert.Empty(t, out[0].Reason)
	})

	t.Run("runner failure annotates", func(t *testing.T) {
		r := &MavenResolver{
			localRepo: repo,
			mvnRunner: func(projectDir string) ([]byte, error) {
				return nil, errors.New("boom")
			},
		}
		out, err := r.Resolve(dir, model.ResolveOptions{AllowToolShellOut: true})
		require.NoError(t, err)
		require.Len(t, out, 1)
		assert.Equal(t, model.ResolutionUnresolved, out[0].Resolution)
		assert.Contains(t, out[0].Reason, "maven tool run failed: boom")
	})

	t.Run("tool not run when all resolved", func(t *testing.T) {
		repo2 := t.TempDir()
		writeDepPOM(t, repo2, "com.google.guava", "guava", "31.1-jre", pomWithLicense("Apache-2.0"))
		called := false
		r := &MavenResolver{
			localRepo: repo2,
			mvnRunner: func(projectDir string) ([]byte, error) {
				called = true
				return nil, nil
			},
		}
		out, err := r.Resolve(dir, model.ResolveOptions{AllowToolShellOut: true})
		require.NoError(t, err)
		require.Len(t, out, 1)
		assert.Equal(t, model.ResolutionResolved, out[0].Resolution)
		assert.False(t, called, "tool must not run when nothing is unresolved")
	})

	t.Run("runner skips already-resolved deps", func(t *testing.T) {
		// Mixed project: one dep resolves on disk, one does not. With the tool tier
		// enabled the runner runs (an unresolved dep exists); the resolved dep must
		// be skipped in the result-merge loop (covers the continue branch).
		repoMixed := t.TempDir()
		writeDepPOM(t, repoMixed, "com.resolved", "ok", "1.0", pomWithLicense("MIT"))
		dirMixed := t.TempDir()
		mixedPOM := `<project>
  <groupId>g</groupId><artifactId>a</artifactId><version>1</version>
  <dependencies>
    <dependency><groupId>com.resolved</groupId><artifactId>ok</artifactId><version>1.0</version></dependency>
    <dependency><groupId>com.gap</groupId><artifactId>missing</artifactId><version>2.0</version></dependency>
  </dependencies>
</project>`
		require.NoError(t, os.WriteFile(filepath.Join(dirMixed, "pom.xml"), []byte(mixedPOM), 0o644))

		// Tool output reports a license for BOTH deps; the resolved one must keep its
		// on-disk MIT (not be overwritten) because the loop continues past it.
		thirdParty := "" +
			"    (Apache-2.0) Ok (com.resolved:ok:1.0 - http://x)\n" +
			"    (BSD-3-Clause) Missing (com.gap:missing:2.0 - http://y)\n"
		r := &MavenResolver{
			localRepo: repoMixed,
			mvnRunner: func(projectDir string) ([]byte, error) {
				return []byte(thirdParty), nil
			},
		}
		out, err := r.Resolve(dirMixed, model.ResolveOptions{AllowToolShellOut: true})
		require.NoError(t, err)

		byName := map[string]model.DependencyLicense{}
		for _, d := range out {
			byName[d.Name] = d
		}
		// On-disk resolved dep is untouched by the tool merge.
		assert.Equal(t, model.ResolutionResolved, byName["com.resolved:ok"].Resolution)
		assert.Equal(t, "MIT", byName["com.resolved:ok"].SPDXID)
		// The gap dep is filled by the tool.
		assert.Equal(t, model.ResolutionResolved, byName["com.gap:missing"].Resolution)
		assert.Equal(t, "BSD-3-Clause", byName["com.gap:missing"].SPDXID)
	})

	t.Run("tool not run when not permitted", func(t *testing.T) {
		called := false
		r := &MavenResolver{
			localRepo: repo,
			mvnRunner: func(projectDir string) ([]byte, error) {
				called = true
				return nil, nil
			},
		}
		out, err := r.Resolve(dir, model.ResolveOptions{AllowToolShellOut: false})
		require.NoError(t, err)
		require.Len(t, out, 1)
		assert.Equal(t, model.ResolutionUnresolved, out[0].Resolution)
		assert.False(t, called)
	})
}

// TestAugmentFromToolNilRunner covers the runner==nil branch of augmentFromTool,
// which falls back to defaultMvnRunner. We do not require mvn to be installed: the
// fallback runner returns an error (or output), and augmentFromTool handles both;
// either way the nil-runner branch executes. We point PATH at a stub mvn so the
// run is deterministic.
func TestAugmentFromToolNilRunner(t *testing.T) {
	stubDir := t.TempDir()
	stub := filepath.Join(stubDir, "mvn")
	script := "#!/bin/sh\n" +
		`echo "    (MIT License) Foo (com.x:y:1.0 - http://example.com)"` + "\n"
	require.NoError(t, os.WriteFile(stub, []byte(script), 0o755))
	t.Setenv("PATH", stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	dir := t.TempDir()
	deps := []model.DependencyLicense{
		{Ecosystem: "maven", Name: "com.x:y", Version: "1.0", Resolution: model.ResolutionUnresolved, Reason: "on-disk gap"},
	}
	r := &MavenResolver{} // mvnRunner nil -> defaultMvnRunner via stub on PATH
	r.augmentFromTool(dir, deps)
	assert.Equal(t, model.ResolutionResolved, deps[0].Resolution)
	assert.Equal(t, "MIT", deps[0].SPDXID)
}

// TestDefaultMvnRunner invokes defaultMvnRunner directly. mvn is almost certainly
// absent in CI, so we only require that the function returns (any error/output is
// acceptable); this executes its body for coverage.
func TestDefaultMvnRunner(t *testing.T) {
	dir := t.TempDir()
	_, err := defaultMvnRunner(dir)
	// Either mvn ran (nil or build error) or was not found; both are fine. We just
	// assert the call completed without panicking.
	_ = err
}

// TestParseMvnLicenseOutput covers the parser's accept and skip branches.
func TestParseMvnLicenseOutput(t *testing.T) {
	raw := strings.Join([]string{
		"Some preamble line that is ignored",
		"    (The Apache Software License, Version 2.0) Guava (com.google.guava:guava:31.1-jre - https://x)",
		"    (MIT License) Left Pad (org.left:pad:1.0)",
		"    (Some Unrecognized License) Thing (org.thing:thing:2.0)", // license not normalizable -> skipped
		"    () Empty License (org.empty:empty:1.0)",                  // close<=1 -> skipped
		"    (MIT) NoCoord without trailing paren",                    // no coord paren -> skipped
		"text (not at start) so open!=0",                              // open != 0 -> skipped
		"",
	}, "\n")

	got := parseMvnLicenseOutput([]byte(raw))
	assert.Equal(t, "Apache-2.0", got["com.google.guava:guava"])
	assert.Equal(t, "MIT", got["org.left:pad"])
	_, hasThing := got["org.thing:thing"]
	assert.False(t, hasThing)
	_, hasEmpty := got["org.empty:empty"]
	assert.False(t, hasEmpty)
	assert.Len(t, got, 2)
}

// TestCoordFromThirdPartyLine covers each branch of the coordinate extractor.
func TestCoordFromThirdPartyLine(t *testing.T) {
	tests := []struct {
		name string
		rest string
		want string
	}{
		{"full coord with url", " Guava (com.google.guava:guava:31.1-jre - https://x)", "com.google.guava:guava"},
		{"coord closed by paren", " Foo (org.a:b:1.0)", "org.a:b"},
		{"no opening paren", " Guava with no coordinate", ""},
		{"too few colon parts", " Foo (justone)", ""},
		{"two parts no version", " Foo (group:artifact)", "group:artifact"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, coordFromThirdPartyLine(tc.rest))
		})
	}
}

// TestParsePOM covers the read-error and unmarshal-error branches plus success.
func TestParsePOM(t *testing.T) {
	dir := t.TempDir()

	_, err := parsePOM(filepath.Join(dir, "missing.pom"))
	require.Error(t, err)

	bad := filepath.Join(dir, "bad.pom")
	require.NoError(t, os.WriteFile(bad, []byte("<project><bad"), 0o644))
	_, err = parsePOM(bad)
	require.Error(t, err)

	good := filepath.Join(dir, "good.pom")
	require.NoError(t, os.WriteFile(good, []byte(`<project><groupId>g</groupId><artifactId>a</artifactId><version>1</version></project>`), 0o644))
	proj, err := parsePOM(good)
	require.NoError(t, err)
	assert.Equal(t, "g", proj.GroupID)
	assert.Equal(t, "a", proj.ArtifactID)
}

// TestEffectiveVersion covers each branch of version resolution.
func TestEffectiveVersion(t *testing.T) {
	proj := pomProject{
		GroupID: "com.example",
		Version: "9.9.9",
		Parent:  pomParent{Version: "8.8.8"},
	}
	projNoVersion := pomProject{
		GroupID: "com.example",
		Parent:  pomParent{Version: "8.8.8"},
	}

	tests := []struct {
		name string
		dep  pomDependency
		proj pomProject
		want string
	}{
		{
			name: "explicit version",
			dep:  pomDependency{GroupID: "x", ArtifactID: "y", Version: "1.2.3"},
			proj: proj,
			want: "1.2.3",
		},
		{
			name: "property placeholder ignored, not same group",
			dep:  pomDependency{GroupID: "x", ArtifactID: "y", Version: "${foo.version}"},
			proj: proj,
			want: "",
		},
		{
			name: "no version, same group, project version",
			dep:  pomDependency{GroupID: "com.example", ArtifactID: "y"},
			proj: proj,
			want: "9.9.9",
		},
		{
			name: "no version, same group, falls back to parent version",
			dep:  pomDependency{GroupID: "com.example", ArtifactID: "y"},
			proj: projNoVersion,
			want: "8.8.8",
		},
		{
			name: "property placeholder, same group, uses project version",
			dep:  pomDependency{GroupID: "com.example", ArtifactID: "y", Version: "${proj.ver}"},
			proj: proj,
			want: "9.9.9",
		},
		{
			name: "no version, different group",
			dep:  pomDependency{GroupID: "com.other", ArtifactID: "y"},
			proj: proj,
			want: "",
		},
		{
			name: "empty group dependency",
			dep:  pomDependency{ArtifactID: "y"},
			proj: proj,
			want: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, effectiveVersion(tc.dep, tc.proj))
		})
	}
}

// TestSPDXFromPOMLicenses covers: first recognized wins, none recognized, and no
// usable names.
func TestSPDXFromPOMLicenses(t *testing.T) {
	t.Run("first recognized wins", func(t *testing.T) {
		id, reason := spdxFromPOMLicenses([]pomLicense{
			{Name: "Unknown One"},
			{Name: "Apache-2.0"},
		})
		assert.Equal(t, "Apache-2.0", id)
		assert.Empty(t, reason)
	})

	t.Run("none recognized lists declared", func(t *testing.T) {
		id, reason := spdxFromPOMLicenses([]pomLicense{
			{Name: "Weird One"},
			{Name: "Weird Two"},
		})
		assert.Empty(t, id)
		assert.Contains(t, reason, "not recognized as SPDX")
		assert.Contains(t, reason, "Weird One")
		assert.Contains(t, reason, "Weird Two")
	})

	t.Run("no usable names", func(t *testing.T) {
		id, reason := spdxFromPOMLicenses([]pomLicense{
			{Name: ""},
			{Name: "   "},
		})
		assert.Empty(t, id)
		assert.Contains(t, reason, "no usable <name>")
	})
}
