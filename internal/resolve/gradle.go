package resolve

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/KofTwentyTwo/license-tool/internal/model"
)

// GradleResolver detects a Gradle project and enumerates its declared
// dependencies, but does not resolve their licenses: Gradle has no on-disk
// resolved-license metadata equivalent to a maven .pom (its caches store jars and
// module descriptors, not POM <licenses>), and v1 does no network fetch. So every
// detected dependency is reported unresolved with a reason explaining the gap.
//
// WHY emit unresolved entries rather than nothing: the audit must surface that a
// Gradle project's dependency licenses are unknown, not silently treat the project
// as license-free. Listing each declared dependency makes the gap visible and
// actionable (e.g. enable a Gradle license plugin and re-run with the tool tier
// once v2 wires it in).
type GradleResolver struct{}

// Ecosystem implements model.Resolver.
func (r *GradleResolver) Ecosystem() string { return "gradle" }

// gradleManifests are the build scripts whose presence marks a Gradle project.
var gradleManifests = []string{"build.gradle", "build.gradle.kts"}

// Detect implements model.Resolver: true when a build.gradle or build.gradle.kts
// is present at path.
func (r *GradleResolver) Detect(path string) bool {
	for _, m := range gradleManifests {
		if fileExists(filepath.Join(path, m)) {
			return true
		}
	}
	return false
}

// gradleDepRE matches the common Gradle dependency-declaration shapes in both the
// Groovy and Kotlin DSLs, capturing the "group:artifact:version" coordinate:
//
//	implementation 'com.google.guava:guava:31.1-jre'
//	api("org.springframework:spring-core:6.1.0")
//	testImplementation "junit:junit:4.13.2"
//
// It is deliberately a surface scan, not a full Gradle model build: detect-only.
var gradleDepRE = regexp.MustCompile(`["']([\w.\-]+:[\w.\-]+:[\w.\-${}]+)["']`)

const (
	gradleOnDiskReason = "gradle on-disk dependency-license resolution is not supported (detect-only; no on-disk license metadata, no network fetch)"
	gradleToolReason   = "Gradle tool-tier dependency-license resolution is not supported; this resolver remains detect-only and does not shell out"
)

// Resolve implements model.Resolver. It surface-scans the build script(s) for
// dependency coordinates and returns each as an unresolved DependencyLicense with
// a reason. It never resolves a license: Gradle resolution is detect-only.
func (r *GradleResolver) Resolve(path string, opts model.ResolveOptions) ([]model.DependencyLicense, error) {
	coords := map[string]string{} // coordinate "group:artifact" -> version
	for _, m := range gradleManifests {
		scanGradleManifest(filepath.Join(path, m), coords)
	}

	reason := gradleOnDiskReason
	if opts.AllowToolShellOut {
		reason = gradleToolReason
	}

	names := make([]string, 0, len(coords))
	for n := range coords {
		names = append(names, n)
	}
	sort.Strings(names)

	out := make([]model.DependencyLicense, 0, len(names))
	for _, name := range names {
		out = append(out, model.DependencyLicense{
			Ecosystem:  r.Ecosystem(),
			Name:       name,
			Version:    coords[name],
			Resolution: model.ResolutionUnresolved,
			Reason:     reason,
		})
	}
	return out, nil
}

// scanGradleManifest reads a build script (if present) and records each matched
// "group:artifact" -> version coordinate into coords. A "${...}" version is
// normalized to empty, since we cannot evaluate Gradle property interpolation.
func scanGradleManifest(path string, coords map[string]string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, m := range gradleDepRE.FindAllStringSubmatch(string(b), -1) {
		// gradleDepRE captures the form "group:artifact:version", so splitting on ":"
		// always yields at least three parts; no short-coordinate guard is needed.
		parts := strings.Split(m[1], ":")
		key := parts[0] + ":" + parts[1]
		version := parts[2]
		if strings.Contains(version, "${") || strings.Contains(version, "}") {
			version = ""
		}
		coords[key] = version
	}
}
