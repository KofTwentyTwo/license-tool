// Package policy classifies detected licenses and enforces the config-defined
// policy: allow/deny SPDX lists, a required license, a curated set of well-known
// hard incompatibilities, and heterogeneity flagging. It produces violation tokens
// that drive check's exit code per the configured fail_on conditions.
//
// This package only enforces policy; it makes no authoritative legal compatibility
// determination, and callers must always print the Disclaimer ("not legal advice").
//
// WHY classification routes through internal/spdx: the vendored snapshot is the
// single source of license category metadata, so policy never hard-codes a second,
// drifting opinion about what a license id means. Ids outside the curated rendering
// set classify as unknown, which is the honest answer rather than a guess.
package policy

import (
	"fmt"
	"sort"

	"github.com/github/go-spdx/v2/spdxexp"

	"github.com/KofTwentyTwo/license-tool/internal/model"
	"github.com/KofTwentyTwo/license-tool/internal/spdx"
)

// Disclaimer is the mandatory non-legal-advice note this package's findings carry.
// It matches the report package's banner verbatim so the two layers never present
// the user with two different disclaimers for the same run.
const Disclaimer = "This tool reports and enforces license metadata; it is not legal advice."

// Violation is a single policy finding, with a stable token used for fail_on
// matching and a human-readable message for reports.
type Violation struct {
	// Condition is the fail_on category this violation belongs to.
	Condition model.FailCondition
	// SPDXID is the offending license id, if applicable.
	SPDXID string
	// Path is the offending file path, if file-scoped; empty for repo-level findings.
	Path string
	// Message is the human-readable explanation.
	Message string
}

// Token returns the stable token for the violation's condition (e.g.
// "policy-violation"), used to match against the configured fail_on set.
func (v Violation) Token() string {
	return v.Condition.String()
}

// Classify maps an SPDX id to its model.Category via the vendored snapshot. An empty
// id or one outside the curated set classifies as CategoryUnknown. WHY exported:
// policy owns license classification for the tool, and audit/report consumers ask it
// rather than reaching into the SPDX store's curated/uncurated split themselves.
func Classify(id string) model.Category {
	if id == "" {
		return model.CategoryUnknown
	}
	if lic, ok := spdx.Lookup(id); ok {
		return lic.Category
	}
	return model.CategoryUnknown
}

// EvaluateFile applies the policy to a single file's detection result and returns
// any violations (missing header, unknown license, allow/deny/required breach).
//
// Skipped files (uncommentable, binary, unknown type) carry no managed header and
// are never faulted for missing one. A present-but-id-less header (matched by our
// sentinel with no SPDX tag) counts as managed: it is not "missing", and with no id
// there is nothing to classify or check against allow/deny/required.
func EvaluateFile(p model.Policy, fr model.FileResult) []Violation {
	if fr.Skipped {
		return nil
	}

	var out []Violation

	if !fr.Detected.Present {
		out = append(out, Violation{
			Condition: model.FailOnMissingHeader,
			Path:      fr.Path,
			Message:   fmt.Sprintf("%s: no managed license header", fr.Path),
		})
		return out
	}

	id := fr.Detected.SPDXID
	if id == "" {
		// A managed header with no SPDX id (sentinel-only). Nothing further to judge.
		return out
	}

	if Classify(id) == model.CategoryUnknown {
		out = append(out, Violation{
			Condition: model.FailOnUnknownLicense,
			SPDXID:    id,
			Path:      fr.Path,
			Message:   fmt.Sprintf("%s: license %q is not classifiable from vendored metadata", fr.Path, id),
		})
	}

	out = append(out, checkAllowDenyRequired(p, id, fr.Path)...)
	return out
}

// EvaluateDependency applies the policy to a resolved dependency license and
// returns any violations. An unresolved dependency is an unresolved-dependency
// finding; a resolved one is checked against deny and allow (the required license
// governs the project's own source, not its third-party dependencies).
func EvaluateDependency(p model.Policy, dep model.DependencyLicense) []Violation {
	label := dependencyLabel(dep)

	if dep.Resolution != model.ResolutionResolved {
		reason := dep.Reason
		if reason == "" {
			reason = "license could not be determined and was not guessed"
		}
		return []Violation{{
			Condition: model.FailOnUnresolvedDependency,
			Path:      label,
			Message:   fmt.Sprintf("%s: %s", label, reason),
		}}
	}

	id := dep.SPDXID
	if id == "" {
		// Marked resolved but with no id: treat as unresolved rather than silently pass.
		return []Violation{{
			Condition: model.FailOnUnresolvedDependency,
			Path:      label,
			Message:   fmt.Sprintf("%s: marked resolved but carries no SPDX id", label),
		}}
	}

	var out []Violation
	if inList(p.Deny, id) {
		out = append(out, Violation{
			Condition: model.FailOnPolicyViolation,
			SPDXID:    id,
			Path:      label,
			Message:   fmt.Sprintf("%s: license %q is on the deny list", label, id),
		})
	}
	if len(p.Allow) > 0 && !inList(p.Allow, id) {
		out = append(out, Violation{
			Condition: model.FailOnPolicyViolation,
			SPDXID:    id,
			Path:      label,
			Message:   fmt.Sprintf("%s: license %q is not on the allow list", label, id),
		})
	}
	return out
}

// EvaluateRepo applies repo-level checks across all detected licenses: allow/deny
// satisfaction, the required license, heterogeneity, and the curated incompatibility
// table. ids is the set of distinct SPDX ids found in the repo (it need not be
// sorted or unique; this function normalizes it).
//
// Heterogeneity and incompatibility are not their own fail_on conditions in the
// model, so they surface under policy-violation: they are policy concerns the run
// can gate on through the policy-violation token.
func EvaluateRepo(p model.Policy, ids []string) []Violation {
	distinct := uniqueSorted(ids)

	var out []Violation

	// Per-id allow/deny across the repo's licenses.
	for _, id := range distinct {
		if id == "" {
			continue
		}
		if inList(p.Deny, id) {
			out = append(out, Violation{
				Condition: model.FailOnPolicyViolation,
				SPDXID:    id,
				Message:   fmt.Sprintf("repo: license %q is on the deny list", id),
			})
		}
		if len(p.Allow) > 0 && !inList(p.Allow, id) {
			out = append(out, Violation{
				Condition: model.FailOnPolicyViolation,
				SPDXID:    id,
				Message:   fmt.Sprintf("repo: license %q is not on the allow list", id),
			})
		}
	}

	// Required license must appear somewhere in the repo's detected licenses. WHY only
	// when at least one license was detected: an empty repo (no managed headers yet)
	// surfaces as missing-header per file, not as a repo-wide required-license breach.
	if p.Required != "" && len(distinct) > 0 && !inList(distinct, p.Required) {
		out = append(out, Violation{
			Condition: model.FailOnPolicyViolation,
			SPDXID:    p.Required,
			Message:   fmt.Sprintf("repo: required license %q is not present among detected licenses", p.Required),
		})
	}

	// Heterogeneity: more than one distinct, classifiable license category in use is a
	// policy signal worth flagging. Unknown ids are excluded from the category set so a
	// single unknown id does not masquerade as a second category.
	if cats := distinctCategories(distinct); len(cats) > 1 {
		out = append(out, Violation{
			Condition: model.FailOnPolicyViolation,
			Message:   fmt.Sprintf("repo: heterogeneous license categories in use: %s", joinCategories(cats)),
		})
	}

	// Curated hard incompatibilities: any unordered pair of detected ids that the
	// curated table marks incompatible is flagged once, in id order.
	for i := 0; i < len(distinct); i++ {
		for j := i + 1; j < len(distinct); j++ {
			if Incompatible(distinct[i], distinct[j]) {
				out = append(out, Violation{
					Condition: model.FailOnPolicyViolation,
					Message:   fmt.Sprintf("repo: licenses %q and %q are a known hard incompatibility", distinct[i], distinct[j]),
				})
			}
		}
	}

	return out
}

// checkAllowDenyRequired evaluates the per-file allow/deny/required policy for a
// single detected id at path, returning policy-violation findings.
func checkAllowDenyRequired(p model.Policy, id, path string) []Violation {
	var out []Violation
	if inList(p.Deny, id) {
		out = append(out, Violation{
			Condition: model.FailOnPolicyViolation,
			SPDXID:    id,
			Path:      path,
			Message:   fmt.Sprintf("%s: license %q is on the deny list", path, id),
		})
	}
	if len(p.Allow) > 0 && !inList(p.Allow, id) {
		out = append(out, Violation{
			Condition: model.FailOnPolicyViolation,
			SPDXID:    id,
			Path:      path,
			Message:   fmt.Sprintf("%s: license %q is not on the allow list", path, id),
		})
	}
	if p.Required != "" && id != p.Required {
		out = append(out, Violation{
			Condition: model.FailOnPolicyViolation,
			SPDXID:    id,
			Path:      path,
			Message:   fmt.Sprintf("%s: license %q does not match required license %q", path, id, p.Required),
		})
	}
	return out
}

// hardIncompatibilities is the curated table of well-known one-way and mutual
// license conflicts. Each pair is stored once and matched in both directions by
// Incompatible. WHY hand-curated and small: this is a heuristic aid, not a legal
// compatibility engine (see Disclaimer). The entries are the conflicts that recur in
// real audits and are uncontroversial in the FOSS community:
//
//   - GPL-2.0-only is incompatible with Apache-2.0 (the patent-termination clause)
//     and with the GPLv3 family (no "or later" upgrade path from a v2-only grant).
//   - Strong/network copyleft (GPL/AGPL) cannot be combined into a single permissive
//     redistribution; pairing AGPL or GPL with a permissive license in one work is
//     flagged so the auditor reviews the combination.
var hardIncompatibilities = []struct{ a, b string }{
	{"GPL-2.0-only", "Apache-2.0"},
	{"GPL-2.0-only", "GPL-3.0-or-later"},
	{"GPL-2.0-only", "AGPL-3.0-or-later"},
	{"GPL-2.0-only", "AGPL-3.0-only"},
	{"GPL-2.0-only", "LGPL-3.0-or-later"},
	{"AGPL-3.0-or-later", "Apache-2.0"},
	{"AGPL-3.0-only", "Apache-2.0"},
	{"GPL-3.0-or-later", "Apache-2.0"},
}

// Incompatible reports whether two SPDX ids are a well-known hard incompatibility
// from the curated table (e.g. GPL-2.0-only with Apache-2.0). It is symmetric and a
// license is never incompatible with itself. This is a heuristic aid, not legal
// advice (see Disclaimer).
func Incompatible(a, b string) bool {
	if a == "" || b == "" || a == b {
		return false
	}
	for _, pair := range hardIncompatibilities {
		if (pair.a == a && pair.b == b) || (pair.a == b && pair.b == a) {
			return true
		}
	}
	return false
}

// ValidExpression reports whether expr is a syntactically valid SPDX license
// expression (e.g. "MIT", "Apache-2.0 OR MIT"). It backs allow/deny/required
// parsing in the policy feature work and pins the go-spdx expression validator in
// go.mod for downstream use (feature agents must not edit go.mod).
func ValidExpression(expr string) bool {
	ok, _ := spdxexp.ValidateLicenses([]string{expr})
	return ok
}

// Passed reports whether the given violations contain none whose condition is in
// the configured fail_on set; this drives check's zero/non-zero exit.
func Passed(violations []Violation, failOn []model.FailCondition) bool {
	failSet := make(map[model.FailCondition]bool, len(failOn))
	for _, f := range failOn {
		failSet[f] = true
	}
	for _, v := range violations {
		if failSet[v.Condition] {
			return false
		}
	}
	return true
}

// --- small helpers (package-local; kept allocation-light and easy to audit) ---

// inList reports whether id is present in list (exact match).
func inList(list []string, id string) bool {
	for _, v := range list {
		if v == id {
			return true
		}
	}
	return false
}

// uniqueSorted returns the sorted, de-duplicated, non-empty subset of ids.
func uniqueSorted(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// distinctCategories returns the sorted set of classifiable categories among ids.
// Unknown is excluded so an unclassifiable id never counts toward heterogeneity.
func distinctCategories(ids []string) []model.Category {
	seen := make(map[model.Category]bool, len(ids))
	for _, id := range ids {
		cat := Classify(id)
		if cat == model.CategoryUnknown {
			continue
		}
		seen[cat] = true
	}
	out := make([]model.Category, 0, len(seen))
	for c := range seen {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// joinCategories renders categories as their comma-separated tokens for messages.
func joinCategories(cats []model.Category) string {
	tokens := make([]string, 0, len(cats))
	for _, c := range cats {
		tokens = append(tokens, c.String())
	}
	out := ""
	for i, t := range tokens {
		if i > 0 {
			out += ", "
		}
		out += t
	}
	return out
}

// dependencyLabel renders a dependency's identity for violation paths/messages,
// e.g. "maven:org.example:lib@1.2.3" or "npm:left-pad" when no version is known.
func dependencyLabel(dep model.DependencyLicense) string {
	name := dep.Name
	if name == "" {
		name = "(unnamed)"
	}
	coord := name
	if dep.Version != "" {
		coord = name + "@" + dep.Version
	}
	if dep.Ecosystem == "" {
		return coord
	}
	return dep.Ecosystem + ":" + coord
}
