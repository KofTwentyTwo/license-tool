package enumerate

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestCompileMatcher verifies the shared gitignore-style glob matcher used by both
// includes and excludes. The load-bearing cases are the ** ones: the previous
// filepath.Match include path silently failed them (issue #32).
func TestCompileMatcher(t *testing.T) {
	cases := []struct {
		name     string
		patterns []string
		rel      string
		want     bool
	}{
		{"basename glob matches at depth", []string{"*.go"}, "a/b/c/x.go", true},
		{"single-segment glob does not span dirs", []string{"src/*.go"}, "src/sub/x.go", false},
		{"single-segment glob matches direct child", []string{"src/*.go"}, "src/x.go", true},
		{"doublestar matches direct child", []string{"src/**"}, "src/x.go", true},
		{"doublestar matches nested", []string{"src/**"}, "src/sub/deep/x.go", true},
		{"leading doublestar matches any depth", []string{"**/generated/**"}, "a/b/generated/c.go", true},
		{"leading doublestar non-match", []string{"**/generated/**"}, "a/b/src/c.go", false},
		{"no pattern matches", []string{"*.go", "*.py"}, "x.ts", false},
		{"one of several matches", []string{"*.go", "*.py"}, "x.py", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := CompileMatcher(tc.patterns)
			assert.False(t, m.Empty())
			assert.Equal(t, tc.want, m.Match(tc.rel))
		})
	}
}

// TestCompileMatcherEmpty verifies an empty pattern set is Empty and matches nothing
// (callers translate "empty includes" into "match all" themselves).
func TestCompileMatcherEmpty(t *testing.T) {
	m := CompileMatcher(nil)
	assert.True(t, m.Empty())
	assert.False(t, m.Match("anything.go"))
}
