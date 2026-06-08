package initwizard

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/KofTwentyTwo/license-tool/internal/model"
)

var (
	paneStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240"))
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	mutedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
)

const footerHints = "tab move | enter edit/commit | esc cancel/back | ctrl+s write | ctrl+c quit"

// Render produces the full init view for the form, layout plan, chosen preview
// sample, and a now-year for preview year resolution. WHY a pure function: the
// Bubble Tea adapter only forwards window size and keys; everything visible is built
// here and unit-tested, so the adapter shell stays trivial.
func Render(s FormState, plan PanelPlan, sample Sample, nowYear int) string {
	if plan.TooSmall {
		return tooSmallView()
	}

	cfg, _ := Translate(s.answers, TranslateOptions{AllowPlaceholders: true})

	boxes := make([]string, 0, len(plan.Panels))
	for _, p := range plan.Panels {
		var title, content string
		switch p.Kind {
		case PanelForm:
			title, content = "license-tool init", renderForm(s)
		case PanelSourcePreview:
			title, content = "Source preview", renderSource(cfg, sample, nowYear)
		case PanelYAMLPreview:
			title, content = ".license-tool.yaml", renderYAML(cfg)
		}
		boxes = append(boxes, panelBox(p, title, content))
	}

	return joinPanels(plan.Orientation, boxes) + "\n" + footer(s)
}

func tooSmallView() string {
	return "Terminal too small for the init wizard.\n" +
		"Widen to at least 60x20, or run non-interactively:\n" +
		"  license-tool init --license <SPDX> --holder \"<name>\""
}

func renderForm(s FormState) string {
	invalid := map[Field]bool{}
	for _, e := range s.Validate() {
		invalid[e.Field] = true
	}

	var b strings.Builder
	for f := FieldLicense; f <= FieldWrite; f++ {
		b.WriteString(renderRow(s, f, invalid[f]))
		b.WriteByte('\n')
		if s.editing && s.focus == f {
			b.WriteString(indentBlock(renderEditor(s)))
			b.WriteByte('\n')
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderRow(s FormState, f Field, invalid bool) string {
	marker := "  "
	if f == s.focus {
		marker = "> "
	}
	if f == FieldWrite {
		label := f.Label()
		if !s.CanWrite() {
			label += "  (blocked)"
		}
		return marker + label
	}

	row := marker + f.Label() + ": " + rowValue(s, f)
	if detectedBadge(s, f) {
		row += "  (detected)"
	}
	if invalid {
		row += "  !"
	}
	return row
}

func rowValue(s FormState, f Field) string {
	switch f {
	case FieldLicense:
		return s.answers.License.SPDXID
	case FieldHolder:
		return orPlaceholder(s.answers.Identity.Holder, "(required)")
	case FieldYear:
		return s.answers.Identity.Year
	case FieldStyle:
		return s.answers.HeaderStyle.Style
	case FieldManageFiles:
		return yesNo(s.answers.LicenseFiles.Manage)
	case FieldInclude:
		return listOrAll(s.answers.Coverage.Include)
	default:
		return listOrNone(s.answers.Coverage.Exclude)
	}
}

func detectedBadge(s FormState, f Field) bool {
	switch f {
	case FieldLicense:
		return s.detected.License
	case FieldHolder:
		return s.detected.Holder
	case FieldManageFiles:
		return s.detected.Manage
	default:
		return false
	}
}

func renderEditor(s FormState) string {
	switch s.focus {
	case FieldLicense:
		return renderLicenseEditor(s)
	case FieldStyle:
		return renderChoiceEditor(styleOptions, s.styleCursor)
	case FieldHolder, FieldYear:
		return "> " + s.textBuffer + "_"
	default: // FieldInclude, FieldExclude
		return renderGlobEditor(s)
	}
}

func renderLicenseEditor(s FormState) string {
	choices := s.filteredLicenses()
	var b strings.Builder
	fmt.Fprintf(&b, "filter: %s\n", orPlaceholder(s.licenseFilter, "(type to filter)"))
	if len(choices) == 0 {
		b.WriteString("(no matching licenses)")
		return b.String()
	}
	start, end := window(s.licenseCursor, len(choices), 6)
	for i := start; i < end; i++ {
		b.WriteString(cursorMark(i == s.licenseCursor) + choices[i] + "\n")
	}
	fmt.Fprintf(&b, "%d/%d", s.licenseCursor+1, len(choices))
	return b.String()
}

func renderChoiceEditor(options []string, cursor int) string {
	var b strings.Builder
	for i, opt := range options {
		b.WriteString(cursorMark(i == cursor) + opt + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderGlobEditor(s FormState) string {
	entries := s.globEntries()
	var b strings.Builder
	if len(entries) == 0 {
		b.WriteString("(no entries yet)\n")
	}
	for i, e := range entries {
		b.WriteString(cursorMark(i == s.globCursor) + e + "\n")
	}
	fmt.Fprintf(&b, "add: %s_  (enter to add, d to remove)", s.globBuffer)
	return b.String()
}

func renderSource(cfg model.Config, sample Sample, nowYear int) string {
	sp, err := BuildSourcePreview(SourcePreviewInput{
		Config:       cfg,
		Sample:       sample,
		ResolvedYear: previewYear(cfg.Year, nowYear),
	})
	if err != nil {
		return "source preview unavailable: " + err.Error()
	}
	return fmt.Sprintf("%s  %s\n\n%s", sp.Language, sp.Path, sp.Content)
}

func renderYAML(cfg model.Config) string {
	// BuildYAMLPreview wraps config.RenderFile, which is documented total (yaml.Marshal
	// of the fixed schema cannot fail for a real Config), so the error is unreachable
	// for any cfg this view can produce and is intentionally not branched on.
	y, _ := BuildYAMLPreview(cfg)
	return strings.TrimRight(string(y), "\n")
}

func previewYear(y model.YearSpec, nowYear int) string {
	switch y.Kind {
	case model.YearCurrent:
		return fmt.Sprintf("%d", nowYear)
	case model.YearExplicit:
		if y.Start > 0 {
			return fmt.Sprintf("%d", y.Start)
		}
	case model.YearRange:
		if y.Start > 0 && y.End > 0 {
			return fmt.Sprintf("%d-%d", y.Start, y.End)
		}
	}
	return fmt.Sprintf("2021-%d", nowYear)
}

func footer(s FormState) string {
	status := "ready to write"
	if r := s.BlockingReason(); r != "" {
		status = "cannot write: " + r
	}
	return mutedStyle.Render(footerHints) + "\n" + mutedStyle.Render(status)
}

func panelBox(p Panel, title, content string) string {
	// The rounded border adds one column/row on each side, so the text area is
	// p.Width-2 by p.Height-2. Content is pre-fit to exactly that width so lipgloss
	// never re-wraps an already-truncated line.
	w := maxInt(1, p.Width-2)
	h := maxInt(1, p.Height-2)
	body := titleStyle.Render(title) + "\n" + fitBlock(content, w, maxInt(1, h-1))
	return paneStyle.Width(w).Height(h).Render(body)
}

func joinPanels(orientation Orientation, boxes []string) string {
	if orientation == LayoutStacked {
		return lipgloss.JoinVertical(lipgloss.Left, boxes...)
	}
	spaced := make([]string, 0, len(boxes)*2)
	for i, b := range boxes {
		if i > 0 {
			spaced = append(spaced, " ")
		}
		spaced = append(spaced, b)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, spaced...)
}

func cursorMark(selected bool) string {
	if selected {
		return "> "
	}
	return "  "
}

func orPlaceholder(value, placeholder string) string {
	if strings.TrimSpace(value) == "" {
		return placeholder
	}
	return value
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func listOrAll(values []string) string {
	if len(values) == 0 {
		return "all supported files"
	}
	return strings.Join(values, ", ")
}

func listOrNone(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	return strings.Join(values, ", ")
}

func indentBlock(text string) string {
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		lines[i] = "    " + l
	}
	return strings.Join(lines, "\n")
}

func window(cursor, total, rows int) (start, end int) {
	if total <= rows {
		return 0, total
	}
	start = cursor - rows/2
	if start < 0 {
		start = 0
	}
	if start+rows > total {
		start = total - rows
	}
	return start, start + rows
}

func fitBlock(text string, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	raw := strings.Split(text, "\n")
	lines := make([]string, 0, height)
	for _, line := range raw {
		lines = append(lines, truncateLine(line, width))
		if len(lines) == height {
			break
		}
	}
	if len(raw) > height {
		lines[height-1] = truncateLine("...", width)
	}
	return strings.Join(lines, "\n")
}

func truncateLine(line string, width int) string {
	r := []rune(line)
	if len(r) <= width {
		return line
	}
	if width <= 3 {
		return string(r[:width])
	}
	return string(r[:width-3]) + "..."
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
