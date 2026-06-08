package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCategoryString(t *testing.T) {
	tests := []struct {
		name     string
		category Category
		want     string
	}{
		{name: "permissive", category: CategoryPermissive, want: "permissive"},
		{name: "weak copyleft", category: CategoryWeakCopyleft, want: "weak-copyleft"},
		{name: "strong copyleft", category: CategoryStrongCopyleft, want: "strong-copyleft"},
		{name: "network copyleft", category: CategoryNetworkCopyleft, want: "network-copyleft"},
		{name: "proprietary", category: CategoryProprietary, want: "proprietary"},
		{name: "unknown zero value", category: CategoryUnknown, want: "unknown"},
		{name: "out of range default", category: Category(99), want: "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.category.String())
		})
	}
}

func TestCategoryRisk(t *testing.T) {
	tests := []struct {
		category Category
		want     string
	}{
		{CategoryStrongCopyleft, "high"},
		{CategoryNetworkCopyleft, "high"},
		{CategoryProprietary, "high"},
		{CategoryWeakCopyleft, "medium"},
		{CategoryPermissive, "low"},
		{CategoryUnknown, "unknown"},
		{Category(99), "unknown"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, tt.category.Risk())
	}
}

func TestHeaderStyleString(t *testing.T) {
	tests := []struct {
		name  string
		style HeaderStyle
		want  string
	}{
		{name: "reuse", style: StyleReuse, want: "reuse"},
		{name: "notice", style: StyleNotice, want: "notice"},
		{name: "reuse plus notice", style: StyleReusePlusNotice, want: "reuse+notice"},
		{name: "out of range defaults to reuse+notice", style: HeaderStyle(99), want: "reuse+notice"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.style.String())
		})
	}
}

func TestFailConditionString(t *testing.T) {
	tests := []struct {
		name      string
		condition FailCondition
		want      string
	}{
		{name: "missing header", condition: FailOnMissingHeader, want: "missing-header"},
		{name: "unknown license", condition: FailOnUnknownLicense, want: "unknown-license"},
		{name: "policy violation", condition: FailOnPolicyViolation, want: "policy-violation"},
		{name: "unresolved dependency", condition: FailOnUnresolvedDependency, want: "unresolved-dependency"},
		{name: "out of range default", condition: FailCondition(99), want: "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.condition.String())
		})
	}
}

func TestDependencyResolutionString(t *testing.T) {
	tests := []struct {
		name       string
		resolution DependencyResolution
		want       string
	}{
		{name: "resolved", resolution: ResolutionResolved, want: "resolved"},
		{name: "unresolved zero value", resolution: ResolutionUnresolved, want: "unresolved"},
		{name: "out of range defaults to unresolved", resolution: DependencyResolution(99), want: "unresolved"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.resolution.String())
		})
	}
}
