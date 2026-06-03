package resolve

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAll verifies the resolver registry returns the three known ecosystems in
// the documented priority order.
func TestAll(t *testing.T) {
	rs := All()
	require.Len(t, rs, 3)
	got := []string{rs[0].Ecosystem(), rs[1].Ecosystem(), rs[2].Ecosystem()}
	assert.Equal(t, []string{"maven", "npm", "gradle"}, got)
}

// TestDetected verifies that Detected returns exactly the resolvers whose manifest
// is present at the path, including the polyglot (multiple manifests) case.
func TestDetected(t *testing.T) {
	tests := []struct {
		name      string
		manifests []string
		want      []string
	}{
		{
			name:      "none present",
			manifests: nil,
			want:      []string{},
		},
		{
			name:      "maven only",
			manifests: []string{"pom.xml"},
			want:      []string{"maven"},
		},
		{
			name:      "npm only",
			manifests: []string{"package.json"},
			want:      []string{"npm"},
		},
		{
			name:      "gradle groovy only",
			manifests: []string{"build.gradle"},
			want:      []string{"gradle"},
		},
		{
			name:      "gradle kotlin only",
			manifests: []string{"build.gradle.kts"},
			want:      []string{"gradle"},
		},
		{
			name:      "polyglot maven plus npm",
			manifests: []string{"pom.xml", "package.json"},
			want:      []string{"maven", "npm"},
		},
		{
			name:      "all three",
			manifests: []string{"pom.xml", "package.json", "build.gradle"},
			want:      []string{"maven", "npm", "gradle"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, m := range tc.manifests {
				require.NoError(t, writeFile(filepath.Join(dir, m), "x"))
			}
			out := Detected(dir)
			got := make([]string, 0, len(out))
			for _, r := range out {
				got = append(got, r.Ecosystem())
			}
			assert.Equal(t, tc.want, got)
		})
	}
}
