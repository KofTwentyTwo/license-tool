// Package resolve determines third-party dependency licenses per ecosystem using
// the tiered strategy from the requirements: read already-resolved on-disk
// metadata by default; optionally shell out to the ecosystem's native tool behind
// a flag; deps that resolve to neither are reported unresolved with a reason,
// never guessed.
//
// Each ecosystem implements model.Resolver. Detected returns the resolvers whose
// Detect reports the ecosystem present at a path, so audit iterates uniformly.
//
// WHY "never guess": the audit's value is that an unresolved dependency is an
// honest gap, not a fabricated answer. Every code path that cannot positively
// identify a license from metadata or a tool emits a DependencyLicense with
// Resolution == ResolutionUnresolved and a human-readable Reason; no heuristic
// ever invents an SPDXID.
package resolve

import (
	"github.com/KofTwentyTwo/license-tool/internal/model"
)

// All returns every known ecosystem resolver, in priority order (Maven, npm/pnpm,
// Gradle first per the requirements non-goals, then any lower-priority ecosystems).
func All() []model.Resolver {
	return []model.Resolver{
		&MavenResolver{},
		&NPMResolver{},
		&GradleResolver{},
	}
}

// Detected returns the subset of All() whose Detect reports presence at path.
// Detection is ecosystem-by-manifest-presence, so a polyglot repo (a Maven module
// that also ships a package.json) yields multiple resolvers.
func Detected(path string) []model.Resolver {
	var out []model.Resolver
	for _, r := range All() {
		if r.Detect(path) {
			out = append(out, r)
		}
	}
	return out
}
