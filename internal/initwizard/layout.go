package initwizard

// PanelKind identifies a renderable panel in the layout plan.
type PanelKind int

const (
	// PanelForm is the editable field column.
	PanelForm PanelKind = iota
	// PanelSourcePreview is the rendered example source file.
	PanelSourcePreview
	// PanelYAMLPreview is the .license-tool.yaml preview.
	PanelYAMLPreview
)

// Orientation is how the panels are arranged.
type Orientation int

const (
	// LayoutHorizontal places panels side by side.
	LayoutHorizontal Orientation = iota
	// LayoutStacked places panels top to bottom.
	LayoutStacked
)

// Panel is one panel's kind and its allotted interior dimensions.
type Panel struct {
	Kind   PanelKind
	Width  int
	Height int
}

// PanelPlan is the adaptive layout decision for a given terminal body size. When
// TooSmall is set the caller should refuse the TUI and fall back to flag-only init.
type PanelPlan struct {
	TooSmall    bool
	Orientation Orientation
	Panels      []Panel
}

// Layout breakpoints. WHY adaptive panel count: a teaching tool wants the source AND
// yaml previews beside the form when there is room, two panels at medium width, and a
// stacked single column when narrow; below the floor the previews cannot render
// usefully so the TUI is refused.
const (
	layoutMinWidth         = 60
	layoutMinHeight        = 20
	layoutFormWidth        = 46
	layoutWideBreakpoint   = 110
	layoutMediumBreakpoint = 80
	layoutGutter           = 1
)

// Layout chooses the panel arrangement for the given body width and height.
func Layout(width, height int) PanelPlan {
	if width < layoutMinWidth || height < layoutMinHeight {
		return PanelPlan{TooSmall: true}
	}

	switch {
	case width >= layoutWideBreakpoint:
		rest := width - layoutFormWidth - 2*layoutGutter
		src := rest / 2
		yaml := rest - src
		return PanelPlan{
			Orientation: LayoutHorizontal,
			Panels: []Panel{
				{PanelForm, layoutFormWidth, height},
				{PanelSourcePreview, src, height},
				{PanelYAMLPreview, yaml, height},
			},
		}
	case width >= layoutMediumBreakpoint:
		src := width - layoutFormWidth - layoutGutter
		return PanelPlan{
			Orientation: LayoutHorizontal,
			Panels: []Panel{
				{PanelForm, layoutFormWidth, height},
				{PanelSourcePreview, src, height},
			},
		}
	default:
		formHeight := height / 2
		srcHeight := height - formHeight - layoutGutter
		return PanelPlan{
			Orientation: LayoutStacked,
			Panels: []Panel{
				{PanelForm, width, formHeight},
				{PanelSourcePreview, width, srcHeight},
			},
		}
	}
}
