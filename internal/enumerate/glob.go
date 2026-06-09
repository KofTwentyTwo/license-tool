package enumerate

import (
	ignore "github.com/sabhiram/go-gitignore"
)

// Matcher matches repo-relative, forward-slash paths against a set of
// gitignore-style glob patterns. WHY gitignore semantics rather than filepath.Match:
// gitignore supports ** (cross-directory) and treats a slash-less pattern as
// matching at any depth, which is what include/exclude globs are documented to do.
// filepath.Match has neither behavior, so "src/**" and "**/generated/**" silently
// matched nothing on the include path (issue #32). Includes and excludes now share
// this one matcher so their semantics cannot drift.
type Matcher struct {
	ig *ignore.GitIgnore
}

// CompileMatcher builds a Matcher from glob patterns. An empty set yields an Empty
// matcher; callers decide whether empty means "match all" (includes) or "match
// nothing" (excludes).
func CompileMatcher(patterns []string) Matcher {
	if len(patterns) == 0 {
		return Matcher{}
	}
	return Matcher{ig: ignore.CompileIgnoreLines(patterns...)}
}

// Empty reports whether the matcher was built from no patterns.
func (m Matcher) Empty() bool {
	return m.ig == nil
}

// Match reports whether rel (forward-slash, relative to the scan root) matches any
// pattern. An Empty matcher matches nothing.
func (m Matcher) Match(rel string) bool {
	if m.ig == nil {
		return false
	}
	return m.ig.MatchesPath(rel)
}
