package resolve

import (
	"os"

	"github.com/KofTwentyTwo/license-tool/internal/model"
)

// fileExists reports whether path names an existing regular file (not a
// directory). Resolvers use it for manifest-presence detection and on-disk
// metadata lookups.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// hasUnresolved reports whether any dependency in deps is still unresolved, used
// to decide whether the optional tool tier is worth running.
func hasUnresolved(deps []model.DependencyLicense) bool {
	for _, d := range deps {
		if d.Resolution == model.ResolutionUnresolved {
			return true
		}
	}
	return false
}

// annotateToolFailure appends a tool-failure note to every still-unresolved
// dependency's Reason so the audit explains that the tool tier ran and failed,
// rather than silently leaving the on-disk reason alone.
func annotateToolFailure(deps []model.DependencyLicense, note string) {
	for i := range deps {
		if deps[i].Resolution != model.ResolutionUnresolved {
			continue
		}
		if deps[i].Reason == "" {
			deps[i].Reason = note
		} else {
			deps[i].Reason = deps[i].Reason + "; " + note
		}
	}
}
