package resolve

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/KofTwentyTwo/license-tool/internal/model"
)

func TestGradleEcosystemAndDetect(t *testing.T) {
	r := &GradleResolver{}
	assert.Equal(t, "gradle", r.Ecosystem())

	tests := []struct {
		name     string
		manifest string
		want     bool
	}{
		{"none", "", false},
		{"groovy", "build.gradle", true},
		{"kotlin", "build.gradle.kts", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.manifest != "" {
				require.NoError(t, os.WriteFile(filepath.Join(dir, tc.manifest), []byte("x"), 0o644))
			}
			assert.Equal(t, tc.want, r.Detect(dir))
		})
	}
}

// TestGradleResolve scans a build script and asserts every coordinate is reported
// unresolved with the v1 reason, deduplicated and sorted, with property-version
// normalization to empty.
func TestGradleResolve(t *testing.T) {
	dir := t.TempDir()
	groovy := `
dependencies {
    implementation 'com.google.guava:guava:31.1-jre'
    api("org.springframework:spring-core:6.1.0")
    testImplementation "junit:junit:4.13.2"
    implementation "org.example:propver:${someVersion}"
    // duplicate of guava, should dedupe
    implementation 'com.google.guava:guava:31.1-jre'
    // a non-coordinate string that should not match the three-part shape
    implementation 'plainstring'
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "build.gradle"), []byte(groovy), 0o644))

	kotlin := `
dependencies {
    implementation("io.ktor:ktor-server-core:2.3.0")
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "build.gradle.kts"), []byte(kotlin), 0o644))

	r := &GradleResolver{}
	out, err := r.Resolve(dir, model.ResolveOptions{})
	require.NoError(t, err)

	byName := map[string]model.DependencyLicense{}
	names := make([]string, 0, len(out))
	for _, d := range out {
		byName[d.Name] = d
		names = append(names, d.Name)
		assert.Equal(t, model.ResolutionUnresolved, d.Resolution)
		assert.Equal(t, "gradle", d.Ecosystem)
		assert.Contains(t, d.Reason, "not implemented in v1")
	}

	// Sorted and deduplicated.
	assert.Equal(t, []string{
		"com.google.guava:guava",
		"io.ktor:ktor-server-core",
		"junit:junit",
		"org.example:propver",
		"org.springframework:spring-core",
	}, names)

	// Versions captured, property version normalized to empty.
	assert.Equal(t, "31.1-jre", byName["com.google.guava:guava"].Version)
	assert.Equal(t, "6.1.0", byName["org.springframework:spring-core"].Version)
	assert.Empty(t, byName["org.example:propver"].Version)
}

// TestGradleResolveNoManifest verifies Resolve on a directory with no build script
// returns an empty (non-nil-handled) result: scanGradleManifest's read-error path.
func TestGradleResolveNoManifest(t *testing.T) {
	dir := t.TempDir()
	r := &GradleResolver{}
	out, err := r.Resolve(dir, model.ResolveOptions{})
	require.NoError(t, err)
	assert.Empty(t, out)
}

// TestScanGradleManifest exercises the helper directly, including the read-error
// (missing file) path, the <3 parts skip, and property-version normalization for
// both the "${" and "}" markers.
func TestScanGradleManifest(t *testing.T) {
	dir := t.TempDir()

	// Missing file: read error, coords untouched.
	coords := map[string]string{}
	scanGradleManifest(filepath.Join(dir, "nope.gradle"), coords)
	assert.Empty(t, coords)

	content := `
    implementation 'a.b:c:1.0'
    implementation 'two:parts'
    implementation 'g:a:${v}'
    implementation 'h:i:1.0}'
`
	path := filepath.Join(dir, "build.gradle")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	scanGradleManifest(path, coords)
	// "two:parts" only has 2 colon-parts after splitting "two:parts" -> but the
	// regex requires group:artifact:version, so it never matches; absent here.
	_, hasTwo := coords["two:parts"]
	assert.False(t, hasTwo)
	assert.Equal(t, "1.0", coords["a.b:c"])
	assert.Empty(t, coords["g:a"])
	assert.Empty(t, coords["h:i"])
}
