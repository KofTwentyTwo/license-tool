package resolve

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/KofTwentyTwo/license-tool/internal/model"
)

// writeFile is a shared test helper that writes content to path, creating parent
// directories as needed. It returns any error so callers can require.NoError.
func writeFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// TestFileExists covers the regular-file, missing, and directory cases of
// fileExists.
func TestFileExists(t *testing.T) {
	dir := t.TempDir()
	regular := filepath.Join(dir, "file.txt")
	require.NoError(t, os.WriteFile(regular, []byte("x"), 0o644))

	tests := []struct {
		name string
		path string
		want bool
	}{
		{"regular file", regular, true},
		{"missing file", filepath.Join(dir, "missing.txt"), false},
		{"directory", dir, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, fileExists(tc.path))
		})
	}
}

// TestHasUnresolved covers the empty, all-resolved, and mixed cases.
func TestHasUnresolved(t *testing.T) {
	tests := []struct {
		name string
		deps []model.DependencyLicense
		want bool
	}{
		{
			name: "empty",
			deps: nil,
			want: false,
		},
		{
			name: "all resolved",
			deps: []model.DependencyLicense{
				{Resolution: model.ResolutionResolved},
				{Resolution: model.ResolutionResolved},
			},
			want: false,
		},
		{
			name: "one unresolved",
			deps: []model.DependencyLicense{
				{Resolution: model.ResolutionResolved},
				{Resolution: model.ResolutionUnresolved},
			},
			want: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, hasUnresolved(tc.deps))
		})
	}
}

// TestAnnotateToolFailure verifies the note is appended only to unresolved deps,
// joined with a separator when a prior reason exists and set verbatim when not.
func TestAnnotateToolFailure(t *testing.T) {
	deps := []model.DependencyLicense{
		{Name: "resolved", Resolution: model.ResolutionResolved, Reason: ""},
		{Name: "empty-reason", Resolution: model.ResolutionUnresolved, Reason: ""},
		{Name: "with-reason", Resolution: model.ResolutionUnresolved, Reason: "prior"},
	}
	annotateToolFailure(deps, "note")

	// Resolved dependency is untouched.
	assert.Empty(t, deps[0].Reason)
	// Unresolved with no prior reason gets the note verbatim.
	assert.Equal(t, "note", deps[1].Reason)
	// Unresolved with a prior reason gets the note appended.
	assert.Equal(t, "prior; note", deps[2].Reason)
}
