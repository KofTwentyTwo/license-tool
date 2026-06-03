// Package policy tests cover classification and policy enforcement end to end:
// Classify, EvaluateFile/Dependency/Repo, Incompatible, ValidExpression, and Passed,
// plus the package-local helpers reached only through those exported entry points.
package policy

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/KofTwentyTwo/license-tool/internal/model"
)

func TestClassify(t *testing.T) {
	tests := []struct {
		name string
		id   string
		want model.Category
	}{
		{"empty id is unknown", "", model.CategoryUnknown},
		{"permissive MIT", "MIT", model.CategoryPermissive},
		{"permissive Apache", "Apache-2.0", model.CategoryPermissive},
		{"weak copyleft MPL", "MPL-2.0", model.CategoryWeakCopyleft},
		{"weak copyleft LGPL", "LGPL-3.0-or-later", model.CategoryWeakCopyleft},
		{"strong copyleft GPL2", "GPL-2.0-only", model.CategoryStrongCopyleft},
		{"strong copyleft GPL3", "GPL-3.0-or-later", model.CategoryStrongCopyleft},
		{"network copyleft AGPL", "AGPL-3.0-or-later", model.CategoryNetworkCopyleft},
		{"uncurated but real SPDX id is unknown", "Zlib", model.CategoryUnknown},
		{"nonsense id is unknown", "Not-A-License", model.CategoryUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Classify(tt.id))
		})
	}
}

func TestViolationToken(t *testing.T) {
	tests := []struct {
		name string
		cond model.FailCondition
		want string
	}{
		{"missing header", model.FailOnMissingHeader, "missing-header"},
		{"unknown license", model.FailOnUnknownLicense, "unknown-license"},
		{"policy violation", model.FailOnPolicyViolation, "policy-violation"},
		{"unresolved dependency", model.FailOnUnresolvedDependency, "unresolved-dependency"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := Violation{Condition: tt.cond}
			assert.Equal(t, tt.want, v.Token())
		})
	}
}

func TestEvaluateFile(t *testing.T) {
	present := func(id string) model.DetectedHeader {
		return model.DetectedHeader{Present: true, SPDXID: id}
	}

	tests := []struct {
		name       string
		policy     model.Policy
		fr         model.FileResult
		wantConds  []model.FailCondition
		wantSPDX   []string // SPDXID per returned violation, aligned with wantConds
		wantNoneAt bool     // expect a nil/empty slice
	}{
		{
			name:       "skipped file yields nothing",
			fr:         model.FileResult{Path: "a.bin", Skipped: true, Detected: present("GPL-2.0-only")},
			wantNoneAt: true,
		},
		{
			name:      "missing header",
			fr:        model.FileResult{Path: "a.go", Detected: model.DetectedHeader{Present: false}},
			wantConds: []model.FailCondition{model.FailOnMissingHeader},
			wantSPDX:  []string{""},
		},
		{
			name:       "sentinel-only header (present, no id) yields nothing",
			fr:         model.FileResult{Path: "a.go", Detected: present("")},
			wantNoneAt: true,
		},
		{
			name:      "unknown license is flagged",
			fr:        model.FileResult{Path: "a.go", Detected: present("Not-A-License")},
			wantConds: []model.FailCondition{model.FailOnUnknownLicense},
			wantSPDX:  []string{"Not-A-License"},
		},
		{
			name:       "clean classifiable license with empty policy passes",
			fr:         model.FileResult{Path: "a.go", Detected: present("MIT")},
			wantNoneAt: true,
		},
		{
			name:      "deny-listed license",
			policy:    model.Policy{Deny: []string{"GPL-2.0-only"}},
			fr:        model.FileResult{Path: "a.go", Detected: present("GPL-2.0-only")},
			wantConds: []model.FailCondition{model.FailOnPolicyViolation},
			wantSPDX:  []string{"GPL-2.0-only"},
		},
		{
			name:      "not on non-empty allow list",
			policy:    model.Policy{Allow: []string{"MIT"}},
			fr:        model.FileResult{Path: "a.go", Detected: present("Apache-2.0")},
			wantConds: []model.FailCondition{model.FailOnPolicyViolation},
			wantSPDX:  []string{"Apache-2.0"},
		},
		{
			name:       "on allow list passes",
			policy:     model.Policy{Allow: []string{"MIT", "Apache-2.0"}},
			fr:         model.FileResult{Path: "a.go", Detected: present("MIT")},
			wantNoneAt: true,
		},
		{
			name:      "does not match required",
			policy:    model.Policy{Required: "AGPL-3.0-or-later"},
			fr:        model.FileResult{Path: "a.go", Detected: present("MIT")},
			wantConds: []model.FailCondition{model.FailOnPolicyViolation},
			wantSPDX:  []string{"MIT"},
		},
		{
			name:       "matches required passes",
			policy:     model.Policy{Required: "MIT"},
			fr:         model.FileResult{Path: "a.go", Detected: present("MIT")},
			wantNoneAt: true,
		},
		{
			name: "unknown plus deny plus allow-miss plus required-miss stack",
			policy: model.Policy{
				Deny:     []string{"Not-A-License"},
				Allow:    []string{"MIT"},
				Required: "MIT",
			},
			fr: model.FileResult{Path: "a.go", Detected: present("Not-A-License")},
			wantConds: []model.FailCondition{
				model.FailOnUnknownLicense,
				model.FailOnPolicyViolation,
				model.FailOnPolicyViolation,
				model.FailOnPolicyViolation,
			},
			wantSPDX: []string{"Not-A-License", "Not-A-License", "Not-A-License", "Not-A-License"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EvaluateFile(tt.policy, tt.fr)
			if tt.wantNoneAt {
				assert.Empty(t, got)
				return
			}
			require.Len(t, got, len(tt.wantConds))
			for i := range tt.wantConds {
				assert.Equal(t, tt.wantConds[i], got[i].Condition, "condition[%d]", i)
				assert.Equal(t, tt.wantSPDX[i], got[i].SPDXID, "spdxid[%d]", i)
				assert.Equal(t, tt.fr.Path, got[i].Path, "path[%d]", i)
				assert.NotEmpty(t, got[i].Message, "message[%d]", i)
			}
		})
	}
}

func TestEvaluateDependency(t *testing.T) {
	tests := []struct {
		name      string
		policy    model.Policy
		dep       model.DependencyLicense
		wantConds []model.FailCondition
		wantLabel string
		wantEmpty bool
	}{
		{
			name:      "unresolved with explicit reason",
			dep:       model.DependencyLicense{Ecosystem: "maven", Name: "org.example:lib", Version: "1.2.3", Resolution: model.ResolutionUnresolved, Reason: "no metadata on disk"},
			wantConds: []model.FailCondition{model.FailOnUnresolvedDependency},
			wantLabel: "maven:org.example:lib@1.2.3",
		},
		{
			name:      "unresolved with default reason and no ecosystem/version",
			dep:       model.DependencyLicense{Name: "left-pad", Resolution: model.ResolutionUnresolved},
			wantConds: []model.FailCondition{model.FailOnUnresolvedDependency},
			wantLabel: "left-pad",
		},
		{
			name:      "unnamed unresolved dependency",
			dep:       model.DependencyLicense{Resolution: model.ResolutionUnresolved},
			wantConds: []model.FailCondition{model.FailOnUnresolvedDependency},
			wantLabel: "(unnamed)",
		},
		{
			name:      "resolved but no id is treated as unresolved",
			dep:       model.DependencyLicense{Ecosystem: "npm", Name: "left-pad", Resolution: model.ResolutionResolved},
			wantConds: []model.FailCondition{model.FailOnUnresolvedDependency},
			wantLabel: "npm:left-pad",
		},
		{
			name:      "resolved clean dependency with empty policy passes",
			dep:       model.DependencyLicense{Ecosystem: "npm", Name: "lodash", Version: "4.0.0", SPDXID: "MIT", Resolution: model.ResolutionResolved},
			wantEmpty: true,
		},
		{
			name:      "resolved deny-listed dependency",
			policy:    model.Policy{Deny: []string{"GPL-2.0-only"}},
			dep:       model.DependencyLicense{Ecosystem: "maven", Name: "lib", Version: "1.0", SPDXID: "GPL-2.0-only", Resolution: model.ResolutionResolved},
			wantConds: []model.FailCondition{model.FailOnPolicyViolation},
			wantLabel: "maven:lib@1.0",
		},
		{
			name:      "resolved not on allow list",
			policy:    model.Policy{Allow: []string{"MIT"}},
			dep:       model.DependencyLicense{Ecosystem: "maven", Name: "lib", SPDXID: "Apache-2.0", Resolution: model.ResolutionResolved},
			wantConds: []model.FailCondition{model.FailOnPolicyViolation},
			wantLabel: "maven:lib",
		},
		{
			name:      "resolved on allow list passes",
			policy:    model.Policy{Allow: []string{"MIT", "Apache-2.0"}},
			dep:       model.DependencyLicense{Ecosystem: "maven", Name: "lib", SPDXID: "MIT", Resolution: model.ResolutionResolved},
			wantEmpty: true,
		},
		{
			name:      "resolved deny and allow-miss stack",
			policy:    model.Policy{Deny: []string{"GPL-2.0-only"}, Allow: []string{"MIT"}},
			dep:       model.DependencyLicense{Ecosystem: "maven", Name: "lib", SPDXID: "GPL-2.0-only", Resolution: model.ResolutionResolved},
			wantConds: []model.FailCondition{model.FailOnPolicyViolation, model.FailOnPolicyViolation},
			wantLabel: "maven:lib",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EvaluateDependency(tt.policy, tt.dep)
			if tt.wantEmpty {
				assert.Empty(t, got)
				return
			}
			require.Len(t, got, len(tt.wantConds))
			for i := range tt.wantConds {
				assert.Equal(t, tt.wantConds[i], got[i].Condition, "condition[%d]", i)
				assert.Equal(t, tt.wantLabel, got[i].Path, "label[%d]", i)
				assert.NotEmpty(t, got[i].Message, "message[%d]", i)
			}
		})
	}
}

func TestEvaluateDependencyUnresolvedDefaultReason(t *testing.T) {
	// The default-reason branch (empty Reason) must produce a message carrying the
	// fallback text, not an empty trailing segment.
	got := EvaluateDependency(model.Policy{}, model.DependencyLicense{
		Name:       "left-pad",
		Resolution: model.ResolutionUnresolved,
	})
	require.Len(t, got, 1)
	assert.Contains(t, got[0].Message, "license could not be determined and was not guessed")
}

func TestEvaluateRepo(t *testing.T) {
	tests := []struct {
		name      string
		policy    model.Policy
		ids       []string
		wantMsgs  []string // substring expected in each returned violation message, in order
		wantEmpty bool
	}{
		{
			name:      "empty id set yields nothing",
			ids:       nil,
			wantEmpty: true,
		},
		{
			name:      "only empty-string ids are skipped",
			ids:       []string{"", ""},
			wantEmpty: true,
		},
		{
			name:      "single permissive license, no policy, passes",
			ids:       []string{"MIT"},
			wantEmpty: true,
		},
		{
			name:     "deny-listed id flagged",
			policy:   model.Policy{Deny: []string{"GPL-2.0-only"}},
			ids:      []string{"GPL-2.0-only"},
			wantMsgs: []string{`"GPL-2.0-only" is on the deny list`},
		},
		{
			name:     "not on allow list flagged",
			policy:   model.Policy{Allow: []string{"MIT"}},
			ids:      []string{"Apache-2.0"},
			wantMsgs: []string{`"Apache-2.0" is not on the allow list`},
		},
		{
			name:     "required license absent among detected",
			policy:   model.Policy{Required: "AGPL-3.0-or-later"},
			ids:      []string{"MIT"},
			wantMsgs: []string{`required license "AGPL-3.0-or-later" is not present`},
		},
		{
			name:      "required license present passes",
			policy:    model.Policy{Required: "MIT"},
			ids:       []string{"MIT"},
			wantEmpty: true,
		},
		{
			name:      "required set but no licenses detected does not breach",
			policy:    model.Policy{Required: "MIT"},
			ids:       []string{""},
			wantEmpty: true,
		},
		{
			name:     "heterogeneous categories flagged",
			ids:      []string{"MIT", "GPL-2.0-only"},
			wantMsgs: []string{"heterogeneous license categories in use"},
		},
		{
			name:      "two ids in same category are not heterogeneous",
			ids:       []string{"MIT", "Apache-2.0"},
			wantEmpty: true,
		},
		{
			name:      "one classifiable plus one unknown is not heterogeneous but unknown is excluded",
			ids:       []string{"MIT", "Not-A-License"},
			wantEmpty: true,
		},
		{
			name:     "hard incompatibility flagged in id order",
			ids:      []string{"GPL-2.0-only", "Apache-2.0"},
			wantMsgs: []string{"heterogeneous license categories in use", `licenses "Apache-2.0" and "GPL-2.0-only" are a known hard incompatibility`},
		},
		{
			name: "everything stacks: deny, allow-miss, required-miss, heterogeneity, incompat",
			policy: model.Policy{
				Deny:     []string{"GPL-2.0-only"},
				Allow:    []string{"Apache-2.0"},
				Required: "MIT",
			},
			ids: []string{"GPL-2.0-only", "Apache-2.0"},
			wantMsgs: []string{
				`"GPL-2.0-only" is on the deny list`,
				`"GPL-2.0-only" is not on the allow list`,
				`required license "MIT" is not present`,
				"heterogeneous license categories in use",
				"known hard incompatibility",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EvaluateRepo(tt.policy, tt.ids)
			if tt.wantEmpty {
				assert.Empty(t, got)
				return
			}
			require.Len(t, got, len(tt.wantMsgs))
			for i := range tt.wantMsgs {
				assert.Equal(t, model.FailOnPolicyViolation, got[i].Condition, "condition[%d]", i)
				assert.Contains(t, got[i].Message, tt.wantMsgs[i], "message[%d]", i)
			}
		})
	}
}

func TestEvaluateRepoDeduplicatesAndSorts(t *testing.T) {
	// uniqueSorted must collapse duplicates and order ids; with a duplicated deny id
	// the violation appears exactly once.
	got := EvaluateRepo(model.Policy{Deny: []string{"GPL-2.0-only"}}, []string{"GPL-2.0-only", "GPL-2.0-only"})
	require.Len(t, got, 1)
	assert.Contains(t, got[0].Message, "deny list")
}

func TestEvaluateRepoHeterogeneityListsCategoriesSorted(t *testing.T) {
	// Three distinct categories should render in ascending category order
	// (permissive < weak-copyleft < strong-copyleft).
	got := EvaluateRepo(model.Policy{}, []string{"MPL-2.0", "MIT", "GPL-2.0-only"})
	require.NotEmpty(t, got)
	// The first (and only) violation here is the heterogeneity one; no incompatible
	// pair among MPL-2.0/MIT/GPL-2.0-only.
	var hetero *Violation
	for i := range got {
		if got[i].Message != "" && got[i].SPDXID == "" {
			hetero = &got[i]
			break
		}
	}
	require.NotNil(t, hetero)
	assert.Contains(t, hetero.Message, "permissive, weak-copyleft, strong-copyleft")
}

// TestEvaluateLicenseID covers evaluateLicenseID directly, including the empty-id
// guard. EvaluateRepo only calls it with the output of uniqueSorted, which strips
// empties, so the empty branch is unreachable from there; testing the helper
// exercises it alongside the deny, not-allowed, and clean paths.
func TestEvaluateLicenseID(t *testing.T) {
	t.Run("empty id yields no violations", func(t *testing.T) {
		assert.Nil(t, evaluateLicenseID(model.Policy{Deny: []string{"MIT"}}, ""))
	})

	t.Run("deny-listed id is flagged", func(t *testing.T) {
		got := evaluateLicenseID(model.Policy{Deny: []string{"GPL-2.0-only"}}, "GPL-2.0-only")
		require.Len(t, got, 1)
		assert.Equal(t, "GPL-2.0-only", got[0].SPDXID)
		assert.Contains(t, got[0].Message, "deny list")
	})

	t.Run("id absent from non-empty allow list is flagged", func(t *testing.T) {
		got := evaluateLicenseID(model.Policy{Allow: []string{"MIT"}}, "Apache-2.0")
		require.Len(t, got, 1)
		assert.Contains(t, got[0].Message, "not on the allow list")
	})

	t.Run("allowed and not denied yields no violations", func(t *testing.T) {
		assert.Empty(t, evaluateLicenseID(model.Policy{Allow: []string{"MIT"}}, "MIT"))
	})
}

func TestIncompatible(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want bool
	}{
		{"empty a", "", "MIT", false},
		{"empty b", "MIT", "", false},
		{"identical ids are never incompatible", "GPL-2.0-only", "GPL-2.0-only", false},
		{"curated forward direction", "GPL-2.0-only", "Apache-2.0", true},
		{"curated reverse direction", "Apache-2.0", "GPL-2.0-only", true},
		{"AGPL-or-later with Apache", "AGPL-3.0-or-later", "Apache-2.0", true},
		{"GPL2 with GPL3", "GPL-2.0-only", "GPL-3.0-or-later", true},
		{"unrelated permissive pair is compatible", "MIT", "Apache-2.0", false},
		{"both unknown ids are compatible", "Foo", "Bar", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Incompatible(tt.a, tt.b))
		})
	}
}

func TestValidExpression(t *testing.T) {
	tests := []struct {
		name string
		expr string
		want bool
	}{
		{"simple id", "MIT", true},
		{"OR expression", "Apache-2.0 OR MIT", true},
		{"AND expression", "MIT AND Apache-2.0", true},
		{"nonsense id", "Not-A-License", false},
		{"empty expression", "", false},
		{"dangling operator", "MIT AND", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ValidExpression(tt.expr))
		})
	}
}

func TestPassed(t *testing.T) {
	tests := []struct {
		name       string
		violations []Violation
		failOn     []model.FailCondition
		want       bool
	}{
		{
			name:   "no violations passes regardless of failOn",
			failOn: []model.FailCondition{model.FailOnPolicyViolation},
			want:   true,
		},
		{
			name:       "violation in failOn set fails",
			violations: []Violation{{Condition: model.FailOnPolicyViolation}},
			failOn:     []model.FailCondition{model.FailOnPolicyViolation},
			want:       false,
		},
		{
			name:       "violation not in failOn set passes",
			violations: []Violation{{Condition: model.FailOnMissingHeader}},
			failOn:     []model.FailCondition{model.FailOnPolicyViolation},
			want:       true,
		},
		{
			name: "one of several violations matching fails",
			violations: []Violation{
				{Condition: model.FailOnMissingHeader},
				{Condition: model.FailOnUnresolvedDependency},
			},
			failOn: []model.FailCondition{model.FailOnUnresolvedDependency},
			want:   false,
		},
		{
			name:       "empty failOn set never fails",
			violations: []Violation{{Condition: model.FailOnPolicyViolation}},
			failOn:     nil,
			want:       true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Passed(tt.violations, tt.failOn))
		})
	}
}

func TestDisclaimerIsStable(t *testing.T) {
	assert.Equal(t, "This tool reports and enforces license metadata; it is not legal advice.", Disclaimer)
}
