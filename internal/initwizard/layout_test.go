package initwizard

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func panelKinds(plan PanelPlan) []PanelKind {
	kinds := make([]PanelKind, 0, len(plan.Panels))
	for _, p := range plan.Panels {
		kinds = append(kinds, p.Kind)
	}
	return kinds
}

func TestLayoutTooSmall(t *testing.T) {
	for _, dim := range [][2]int{{50, 30}, {120, 10}, {59, 19}} {
		plan := Layout(dim[0], dim[1])
		assert.True(t, plan.TooSmall, "%dx%d should be too small", dim[0], dim[1])
		assert.Empty(t, plan.Panels)
	}
}

func TestLayoutWideHasThreePanels(t *testing.T) {
	plan := Layout(120, 40)
	require.False(t, plan.TooSmall)
	assert.Equal(t, LayoutHorizontal, plan.Orientation)
	assert.Equal(t, []PanelKind{PanelForm, PanelSourcePreview, PanelYAMLPreview}, panelKinds(plan))

	total := 2 * layoutGutter
	for _, p := range plan.Panels {
		total += p.Width
		assert.Equal(t, 40, p.Height)
	}
	assert.Equal(t, 120, total)
	assert.Equal(t, layoutFormWidth, plan.Panels[0].Width)
}

func TestLayoutMediumHasTwoPanels(t *testing.T) {
	plan := Layout(90, 30)
	require.False(t, plan.TooSmall)
	assert.Equal(t, LayoutHorizontal, plan.Orientation)
	assert.Equal(t, []PanelKind{PanelForm, PanelSourcePreview}, panelKinds(plan))
	assert.Equal(t, 90, plan.Panels[0].Width+plan.Panels[1].Width+layoutGutter)
}

func TestLayoutNarrowStacks(t *testing.T) {
	plan := Layout(70, 24)
	require.False(t, plan.TooSmall)
	assert.Equal(t, LayoutStacked, plan.Orientation)
	assert.Equal(t, []PanelKind{PanelForm, PanelSourcePreview}, panelKinds(plan))
	for _, p := range plan.Panels {
		assert.Equal(t, 70, p.Width)
	}
	assert.Equal(t, 24, plan.Panels[0].Height+plan.Panels[1].Height+layoutGutter)
}

func TestLayoutBreakpointBoundaries(t *testing.T) {
	assert.Len(t, Layout(layoutWideBreakpoint, 40).Panels, 3)
	assert.Len(t, Layout(layoutMediumBreakpoint, 40).Panels, 2)
	assert.Equal(t, LayoutStacked, Layout(layoutMinWidth, layoutMinHeight).Orientation)
}
