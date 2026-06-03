package resolve

import (
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/KofTwentyTwo/license-tool/internal/model"
)

// MavenResolver resolves Maven dependency licenses from the project pom.xml plus
// the on-disk local repository (~/.m2/repository) resolved POMs, with an optional
// shell-out to the maven-license-plugin behind ResolveOptions.AllowToolShellOut.
type MavenResolver struct {
	// localRepo overrides the default ~/.m2/repository location; empty uses the
	// default. WHY a field: tests point it at a fixture repository without touching
	// the developer's real ~/.m2.
	localRepo string

	// mvnRunner runs the maven CLI for the tool tier; nil uses the real exec path.
	// WHY injectable: the on-disk tier is unit-testable without maven installed,
	// and the tool tier is testable without a network or a real reactor build.
	mvnRunner func(projectDir string) ([]byte, error)
}

// Ecosystem implements model.Resolver.
func (r *MavenResolver) Ecosystem() string { return "maven" }

// Detect implements model.Resolver: true when a pom.xml is present at path.
func (r *MavenResolver) Detect(path string) bool {
	return fileExists(filepath.Join(path, "pom.xml"))
}

// pomProject mirrors the slice of POM XML we read: the project's own coordinate,
// its declared licenses, and its declared dependencies.
type pomProject struct {
	XMLName      xml.Name        `xml:"project"`
	GroupID      string          `xml:"groupId"`
	ArtifactID   string          `xml:"artifactId"`
	Version      string          `xml:"version"`
	Parent       pomParent       `xml:"parent"`
	Licenses     []pomLicense    `xml:"licenses>license"`
	Dependencies []pomDependency `xml:"dependencies>dependency"`
}

type pomParent struct {
	GroupID string `xml:"groupId"`
	Version string `xml:"version"`
}

type pomLicense struct {
	Name string `xml:"name"`
	URL  string `xml:"url"`
}

type pomDependency struct {
	GroupID    string `xml:"groupId"`
	ArtifactID string `xml:"artifactId"`
	Version    string `xml:"version"`
	Scope      string `xml:"scope"`
}

// Resolve implements model.Resolver. It reads the project pom.xml, enumerates its
// dependencies, and resolves each one's license from the on-disk local repository.
// When AllowToolShellOut is set and the on-disk tier leaves gaps, it augments the
// results with a maven CLI run. Dependencies that resolve to neither metadata nor
// tool output are returned unresolved with a reason.
func (r *MavenResolver) Resolve(path string, opts model.ResolveOptions) ([]model.DependencyLicense, error) {
	pomPath := filepath.Join(path, "pom.xml")
	proj, err := parsePOM(pomPath)
	if err != nil {
		return nil, fmt.Errorf("maven: read %s: %w", pomPath, err)
	}

	repo := r.repoDir()

	out := make([]model.DependencyLicense, 0, len(proj.Dependencies))
	for _, d := range proj.Dependencies {
		ver := effectiveVersion(d, proj)
		dep := model.DependencyLicense{
			Ecosystem: r.Ecosystem(),
			Name:      d.GroupID + ":" + d.ArtifactID,
			Version:   ver,
		}

		id, reason := r.resolveOnDisk(repo, d.GroupID, d.ArtifactID, ver)
		if id != "" {
			dep.SPDXID = id
			dep.Resolution = model.ResolutionResolved
		} else {
			dep.Resolution = model.ResolutionUnresolved
			dep.Reason = reason
		}
		out = append(out, dep)
	}

	// Tool tier: only run when explicitly permitted AND there is at least one
	// dependency the on-disk tier could not resolve, so we never shell out
	// gratuitously.
	if opts.AllowToolShellOut && hasUnresolved(out) {
		r.augmentFromTool(path, out)
	}

	return out, nil
}

// repoDir returns the local repository directory: the injected override when set,
// else ~/.m2/repository. WHY default-to-home: that is maven's documented default
// and the only location we can read without running maven.
func (r *MavenResolver) repoDir() string {
	if r.localRepo != "" {
		return r.localRepo
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".m2", "repository")
}

// resolveOnDisk reads the dependency's resolved POM from the local repository and
// maps its declared license to an SPDX id. It returns ("", reason) when the POM is
// missing, declares no license, or declares one we cannot positively identify.
func (r *MavenResolver) resolveOnDisk(repo, group, artifact, version string) (string, string) {
	if repo == "" {
		return "", "no local repository available (~/.m2 not found)"
	}
	if version == "" {
		return "", "dependency version unresolved in pom (no <version>, dependencyManagement not evaluated)"
	}

	depPOM := filepath.Join(repo, filepath.Join(strings.Split(group, ".")...), artifact, version, artifact+"-"+version+".pom")
	if !fileExists(depPOM) {
		return "", "no resolved pom on disk at " + depPOM
	}
	proj, err := parsePOM(depPOM)
	if err != nil {
		return "", "resolved pom unreadable: " + err.Error()
	}
	if len(proj.Licenses) == 0 {
		return "", "resolved pom declares no <licenses>"
	}
	return spdxFromPOMLicenses(proj.Licenses)
}

// augmentFromTool runs the maven CLI (or the injected runner) and fills in any
// still-unresolved dependencies whose license the tool output reveals. It is
// best-effort: a tool failure leaves the on-disk results untouched (the deps stay
// unresolved with their on-disk reason) rather than erroring the whole audit.
func (r *MavenResolver) augmentFromTool(projectDir string, deps []model.DependencyLicense) {
	runner := r.mvnRunner
	if runner == nil {
		runner = defaultMvnRunner
	}
	raw, err := runner(projectDir)
	if err != nil {
		annotateToolFailure(deps, "maven tool run failed: "+err.Error())
		return
	}
	found := parseMvnLicenseOutput(raw)
	for i := range deps {
		if deps[i].Resolution == model.ResolutionResolved {
			continue
		}
		if id, ok := found[deps[i].Name]; ok {
			deps[i].SPDXID = id
			deps[i].Resolution = model.ResolutionResolved
			deps[i].Reason = ""
		}
	}
}

// defaultMvnRunner invokes the maven license plugin to dump dependency licenses.
// WHY this goal: `license:aggregate-add-third-party` / `license:download-licenses`
// vary by setup; `dependency:list` plus per-artifact POMs is the most portable,
// but here we use the license plugin's third-party report which prints
// "(<license>) <name> (<group>:<artifact>:<version>...)" lines we can parse.
func defaultMvnRunner(projectDir string) ([]byte, error) {
	cmd := exec.Command("mvn", "-q", "-B",
		"org.codehaus.mojo:license-maven-plugin:add-third-party",
		"-Dlicense.outputDirectory=target/generated-sources/license",
	)
	cmd.Dir = projectDir
	return cmd.CombinedOutput()
}

// parseMvnLicenseOutput parses THIRD-PARTY.txt-style lines emitted by the maven
// license plugin into a map of "group:artifact" -> SPDX id, keeping only entries
// whose license string we can positively normalize.
//
// The line shape is: "    (The Apache Software License, Version 2.0) Guava (com.google.guava:guava:31.1-jre - https://...)".
func parseMvnLicenseOutput(raw []byte) map[string]string {
	out := map[string]string{}
	for _, ln := range strings.Split(string(raw), "\n") {
		ln = strings.TrimSpace(ln)
		open := strings.Index(ln, "(")
		close := strings.Index(ln, ")")
		if open != 0 || close <= 1 {
			continue
		}
		licStr := ln[open+1 : close]
		coord := coordFromThirdPartyLine(ln[close+1:])
		if coord == "" {
			continue
		}
		if id, ok := normalizeSPDX(licStr); ok {
			out[coord] = id
		}
	}
	return out
}

// coordFromThirdPartyLine extracts "group:artifact" from the trailing
// "(group:artifact:version - url)" segment of a THIRD-PARTY line.
func coordFromThirdPartyLine(rest string) string {
	lp := strings.LastIndex(rest, "(")
	if lp < 0 {
		return ""
	}
	inner := rest[lp+1:]
	if rp := strings.IndexAny(inner, " )"); rp >= 0 {
		inner = inner[:rp]
	}
	parts := strings.Split(inner, ":")
	if len(parts) < 2 {
		return ""
	}
	return parts[0] + ":" + parts[1]
}

// parsePOM reads and unmarshals a POM file into pomProject.
func parsePOM(path string) (pomProject, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return pomProject{}, err
	}
	var proj pomProject
	if err := xml.Unmarshal(b, &proj); err != nil {
		return pomProject{}, err
	}
	return proj, nil
}

// effectiveVersion returns a dependency's version, falling back to the project (or
// its parent) version when the dependency omits one and shares the project group
// (a common intra-reactor pattern). It deliberately does NOT evaluate
// dependencyManagement or properties: that requires a full maven model build,
// which is the tool tier's job. An undetermined version stays empty so the on-disk
// tier reports it unresolved rather than guessing.
func effectiveVersion(d pomDependency, proj pomProject) string {
	if d.Version != "" && !strings.HasPrefix(d.Version, "${") {
		return d.Version
	}
	if d.GroupID != "" && d.GroupID == proj.GroupID {
		if proj.Version != "" {
			return proj.Version
		}
		return proj.Parent.Version
	}
	return ""
}

// spdxFromPOMLicenses picks the first POM-declared license that normalizes to a
// known SPDX id. When none normalize, it returns ("", reason) listing what was
// declared, so the audit explains why it stayed unresolved without guessing.
func spdxFromPOMLicenses(lics []pomLicense) (string, string) {
	var names []string
	for _, l := range lics {
		name := strings.TrimSpace(l.Name)
		if name == "" {
			continue
		}
		names = append(names, name)
		if id, ok := normalizeSPDX(name); ok {
			return id, ""
		}
	}
	if len(names) == 0 {
		return "", "resolved pom <licenses> had no usable <name>"
	}
	return "", "declared license(s) not recognized as SPDX: " + strings.Join(names, "; ")
}
