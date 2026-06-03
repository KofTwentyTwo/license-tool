package resolve

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/KofTwentyTwo/license-tool/internal/model"
)

// NPMResolver resolves npm/pnpm dependency licenses from the already-installed
// node_modules tree: every package.json under node_modules carries a license
// field that npm/pnpm wrote when it materialized the dependency. WHY node_modules
// rather than the lockfile: the lockfile records versions, not license strings;
// the installed package.json files are the on-disk metadata the requirements call
// for, and they cover npm, pnpm, and yarn layouts identically.
type NPMResolver struct{}

// Ecosystem implements model.Resolver.
func (r *NPMResolver) Ecosystem() string { return "npm" }

// Detect implements model.Resolver: true when a package.json is present at path.
func (r *NPMResolver) Detect(path string) bool {
	return fileExists(filepath.Join(path, "package.json"))
}

// packageJSON mirrors the slice of package.json we read. license may be a bare
// SPDX string ("MIT") or, in old packages, an object {type,url}; licenses is the
// deprecated array form. dependency maps capture declared (not installed) deps so
// a dependency present in package.json but absent from node_modules is reported
// unresolved rather than silently dropped.
type packageJSON struct {
	Name            string            `json:"name"`
	Version         string            `json:"version"`
	License         json.RawMessage   `json:"license"`
	Licenses        []licenseObject   `json:"licenses"`
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
}

type licenseObject struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

// Resolve implements model.Resolver. It collects the set of declared dependency
// names from the root package.json, then resolves each one's license from its
// installed node_modules/<name>/package.json. Declared dependencies with no
// installed package.json, or whose license string cannot be normalized to SPDX,
// are returned unresolved with a reason. opts.AllowToolShellOut is unused for npm:
// the installed metadata is authoritative and complete, so there is no native tool
// tier to add value (recorded in opts handling for interface symmetry).
func (r *NPMResolver) Resolve(path string, opts model.ResolveOptions) ([]model.DependencyLicense, error) {
	rootPath := filepath.Join(path, "package.json")
	root, err := parsePackageJSON(rootPath)
	if err != nil {
		return nil, fmt.Errorf("npm: read %s: %w", rootPath, err)
	}

	names := declaredDependencyNames(root)
	nodeModules := filepath.Join(path, "node_modules")

	out := make([]model.DependencyLicense, 0, len(names))
	for _, name := range names {
		dep := model.DependencyLicense{
			Ecosystem: r.Ecosystem(),
			Name:      name,
		}
		id, version, reason := resolveInstalledNPM(nodeModules, name)
		dep.Version = version
		if id != "" {
			dep.SPDXID = id
			dep.Resolution = model.ResolutionResolved
		} else {
			dep.Resolution = model.ResolutionUnresolved
			dep.Reason = reason
		}
		out = append(out, dep)
	}
	return out, nil
}

// declaredDependencyNames returns the sorted union of dependencies and
// devDependencies from the root package.json. Sorting makes Resolve output
// deterministic regardless of map iteration order.
func declaredDependencyNames(p packageJSON) []string {
	set := map[string]struct{}{}
	for n := range p.Dependencies {
		set[n] = struct{}{}
	}
	for n := range p.DevDependencies {
		set[n] = struct{}{}
	}
	names := make([]string, 0, len(set))
	for n := range set {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// resolveInstalledNPM reads node_modules/<name>/package.json (scoped names like
// "@scope/pkg" map to node_modules/@scope/pkg/package.json) and normalizes its
// license. Returns (id, version, "") on success or ("", version, reason) when the
// package is not installed or its license is unrecognized.
func resolveInstalledNPM(nodeModules, name string) (string, string, string) {
	pkgPath := filepath.Join(nodeModules, filepath.FromSlash(name), "package.json")
	if !fileExists(pkgPath) {
		return "", "", "not installed under node_modules/" + name
	}
	pkg, err := parsePackageJSON(pkgPath)
	if err != nil {
		return "", "", "installed package.json unreadable: " + err.Error()
	}
	raw, reason := licenseStringFromPackage(pkg)
	if raw == "" {
		return "", pkg.Version, reason
	}
	if id, ok := normalizeSPDX(raw); ok {
		return id, pkg.Version, ""
	}
	return "", pkg.Version, "declared license not recognized as SPDX: " + raw
}

// licenseStringFromPackage extracts the raw license string from a package.json,
// handling the three historical shapes: a bare string, the {type,url} object, and
// the deprecated licenses[] array. Returns ("", reason) when none is present so
// the dependency is reported unresolved rather than guessed.
func licenseStringFromPackage(p packageJSON) (string, string) {
	if len(p.License) > 0 {
		// String form: "MIT".
		var s string
		if err := json.Unmarshal(p.License, &s); err == nil && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s), ""
		}
		// Object form: {"type":"MIT","url":...}.
		var obj licenseObject
		if err := json.Unmarshal(p.License, &obj); err == nil && strings.TrimSpace(obj.Type) != "" {
			return strings.TrimSpace(obj.Type), ""
		}
	}
	// Deprecated array form: [{"type":"MIT"}, ...]; first usable type wins.
	for _, l := range p.Licenses {
		if strings.TrimSpace(l.Type) != "" {
			return strings.TrimSpace(l.Type), ""
		}
	}
	return "", "package.json declares no license field"
}

// parsePackageJSON reads and unmarshals a package.json into packageJSON.
func parsePackageJSON(path string) (packageJSON, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return packageJSON{}, err
	}
	var p packageJSON
	if err := json.Unmarshal(b, &p); err != nil {
		return packageJSON{}, err
	}
	return p, nil
}
