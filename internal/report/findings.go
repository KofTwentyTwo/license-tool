package report

import (
	"fmt"
	"sort"
	"strings"

	"github.com/KofTwentyTwo/license-tool/internal/model"
	"github.com/KofTwentyTwo/license-tool/internal/spdx"
)

// Findings is the at-a-glance summary printed near the top of the text report. It
// is derived purely from a model.Report so it can be unit-tested without rendering,
// and so the text renderer stays a thin formatter over already-computed numbers.
//
// WHY a dedicated struct rather than formatting inline in renderText: the summary
// answers "what did we find" by category (source coverage, license mix, unknowns,
// copyleft exposure, dependency resolution, policy), each of which is a small bit
// of aggregation over the report's existing fields. Pulling that aggregation into
// one pure function makes the arithmetic testable in isolation and keeps the
// renderer deterministic.
type Findings struct {
	// SourceTotal is the count of coverable (non-skipped) source files.
	SourceTotal int
	// SourceHeadered is the count of coverable source files carrying a managed header.
	SourceHeadered int
	// SourceMissing is the count of coverable source files with no managed header.
	SourceMissing int

	// LicenseCounts maps each detected SPDX id to its source-file count (managed,
	// headered files only). It excludes the synthetic (none) bucket; missing files
	// are reported via SourceMissing.
	LicenseCounts map[string]int

	// UnknownCount is the number of headered source files whose detected id is not a
	// recognized SPDX identifier (or classifies as the unknown category).
	UnknownCount int

	// Copyleft lists the distinct copyleft SPDX ids present in source (weak, strong,
	// or network copyleft categories), sorted.
	Copyleft []string

	// DepsScanned is true when dependency resolution ran (Dependencies is non-empty);
	// the dependencies line is omitted from the summary when false.
	DepsScanned bool
	// DepsTotal/Resolved/Unresolved summarize dependency resolution.
	DepsTotal      int
	DepsResolved   int
	DepsUnresolved int

	// Passed mirrors Report.Passed (the policy verdict).
	Passed bool
	// Violations are the repo-level violation tokens, sorted, surfaced when failing.
	Violations []string

	// RiskLevel is the worst obligation severity among the headered source licenses
	// ("high"|"medium"|"low"|"none"), so a reader or model can branch on one value.
	RiskLevel string
	// WorstCategory is the license category contributing RiskLevel (empty when none).
	WorstCategory string
}

// buildFindings folds a finished model.Report into a Findings summary. It is pure
// (no I/O, no globals beyond the read-only vendored SPDX index) and order-stable, so
// identical reports yield identical Findings.
func buildFindings(r model.Report) Findings {
	f := Findings{
		LicenseCounts: map[string]int{},
		Passed:        r.Passed,
	}

	copyleftSeen := map[string]bool{}
	worst := worstRisk{}
	for _, fr := range r.Files {
		// Coverable means a file that participates in license management. Skipped
		// files (binary, uncommentable, unknown type) carry no managed header and are
		// excluded from the source coverage tally, matching Build's by-license logic.
		if fr.Skipped {
			continue
		}
		f.SourceTotal++

		id := fr.Detected.SPDXID
		if fr.Detected.Present && id != "" {
			f.SourceHeadered++
			f.LicenseCounts[id]++

			// Unknown = a detected id we cannot vouch for: not a real SPDX id, or one
			// we have no curated category for. Either way it is unrecognized exposure.
			if !spdx.Validate(id) || categoryToken(id) == model.CategoryUnknown.String() {
				f.UnknownCount++
			}

			cat := classifyCategory(id)
			worst.observe(cat)
			switch cat {
			case model.CategoryWeakCopyleft,
				model.CategoryStrongCopyleft,
				model.CategoryNetworkCopyleft:
				copyleftSeen[id] = true
			}
		} else {
			f.SourceMissing++
		}
	}

	f.Copyleft = sortedKeys(copyleftSeen)
	f.RiskLevel, f.WorstCategory = worst.result()

	if len(r.Dependencies) > 0 {
		f.DepsScanned = true
		f.DepsTotal = len(r.Dependencies)
		for _, dep := range r.Dependencies {
			if dep.Resolution == model.ResolutionResolved {
				f.DepsResolved++
			} else {
				f.DepsUnresolved++
			}
		}
	}

	// Violations are already sorted+deduped on the Report, but copy defensively so a
	// caller mutating the slice cannot perturb the source report.
	f.Violations = append([]string(nil), r.Violations...)

	return f
}

// licenseSummary renders the per-id license mix as "<id> <count>" tokens joined by
// commas, appending "none <M>" when any coverable source file is missing a header,
// and "(none)" when there are no coverable source files at all. Ids are sorted so
// the line is deterministic.
func (f Findings) licenseSummary() string {
	if f.SourceTotal == 0 {
		return "(none)"
	}
	parts := make([]string, 0, len(f.LicenseCounts)+1)
	for _, id := range sortedKeys(f.LicenseCounts) {
		parts = append(parts, fmt.Sprintf("%s %d", id, f.LicenseCounts[id]))
	}
	if f.SourceMissing > 0 {
		parts = append(parts, fmt.Sprintf("%s %d", noLicenseKey, f.SourceMissing))
	}
	if len(parts) == 0 {
		return "(none)"
	}
	return strings.Join(parts, ", ")
}

// copyleftSummary renders the distinct copyleft ids present, or "none".
func (f Findings) copyleftSummary() string {
	if len(f.Copyleft) == 0 {
		return "none"
	}
	return strings.Join(f.Copyleft, ", ")
}

// policySummary renders "PASS" or, on failure, "FAIL" followed by the violation
// tokens and a count, so the reader sees the verdict and its cause in one line.
func (f Findings) policySummary() string {
	if f.Passed {
		return "PASS"
	}
	if len(f.Violations) == 0 {
		return "FAIL"
	}
	return fmt.Sprintf("FAIL (%d: %s)", len(f.Violations), strings.Join(f.Violations, ", "))
}

// renderFindings writes the Findings block. It is the only formatter that knows the
// block's layout; buildFindings owns the arithmetic. The dependencies line is
// omitted entirely when dependency resolution did not run.
func renderFindings(bw *errWriter, f Findings) {
	bw.printf("findings:\n")
	bw.printf("  source files: %d (headered %d, missing %d)\n", f.SourceTotal, f.SourceHeadered, f.SourceMissing)
	bw.printf("  license types: %s\n", f.licenseSummary())
	bw.printf("  unknown/unrecognized: %d\n", f.UnknownCount)
	bw.printf("  copyleft: %s\n", f.copyleftSummary())
	bw.printf("  risk: %s\n", f.riskSummary())
	if f.DepsScanned {
		bw.printf("  dependencies: %d (resolved %d, unresolved %d)\n", f.DepsTotal, f.DepsResolved, f.DepsUnresolved)
	}
	bw.printf("  policy: %s\n", f.policySummary())
	bw.printf("\n")
}

// worstRisk tracks the highest-severity license category observed across headered
// source files, for the findings RiskLevel. Ties on risk rank break toward the higher
// Category enum so the result is deterministic.
type worstRisk struct {
	cat  model.Category
	have bool
}

func (w *worstRisk) observe(c model.Category) {
	if !w.have || riskRank(c) > riskRank(w.cat) || (riskRank(c) == riskRank(w.cat) && c > w.cat) {
		w.cat = c
		w.have = true
	}
}

func (w worstRisk) result() (level, category string) {
	if !w.have {
		return "none", ""
	}
	return w.cat.Risk(), w.cat.String()
}

// riskRank ranks a category's obligation severity for "worst" comparison.
func riskRank(c model.Category) int {
	switch c.Risk() {
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}

// riskSummary renders the risk level and the worst category, or "none".
func (f Findings) riskSummary() string {
	if f.WorstCategory == "" {
		return "none"
	}
	return fmt.Sprintf("%s (%s)", f.RiskLevel, f.WorstCategory)
}

// sortedKeys returns the keys of a string-keyed map in ascending order, the canonical
// ordering used throughout the renderers for determinism.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
