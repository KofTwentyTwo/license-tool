package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/KofTwentyTwo/license-tool/internal/initwizard"
	"github.com/KofTwentyTwo/license-tool/internal/model"
	"github.com/KofTwentyTwo/license-tool/internal/spdx"
)

var (
	projectModelOptions = []initwizard.ProjectModel{
		initwizard.ProjectModelOpenSource,
		initwizard.ProjectModelPrivateInternal,
		initwizard.ProjectModelExistingProject,
		initwizard.ProjectModelAdvancedManual,
	}
	headerStyleOptions = []string{"reuse", "notice", "reuse+notice"}

	appTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("15")).
			Background(lipgloss.Color("62")).
			Padding(0, 1)
	paneStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240")).
			Padding(0, 1)
	activePaneStyle = paneStyle.
			BorderForeground(lipgloss.Color("62"))
	paneTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("15"))
	mutedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245"))
)

// licenseSelectOptions builds the license picker values: common renderable SPDX ids
// first, then the remaining renderable ids in sorted order without duplicates.
func licenseSelectOptions() []string {
	common := spdx.CommonIDs()
	seen := make(map[string]bool, len(common))
	opts := make([]string, 0, len(common))
	for _, id := range common {
		seen[id] = true
		opts = append(opts, id)
	}
	for _, id := range spdx.RenderableIDs() {
		if seen[id] {
			continue
		}
		opts = append(opts, id)
	}
	return opts
}

type initWizardModel struct {
	root           string
	answers        initwizard.Answers
	step           initwizard.Step
	input          textinput.Model
	licenseOptions []string
	licenseFilter  string
	previewPaths   []string
	projectIndex   int
	licenseIndex   int
	styleIndex     int
	width          int
	height         int
}

func newInitWizardModel(root string, answers initwizard.Answers) initWizardModel {
	m := initWizardModel{
		root:           root,
		answers:        answers,
		step:           initwizard.StepProjectModel,
		licenseOptions: licenseSelectOptions(),
		previewPaths:   previewCandidatePaths(root),
	}
	if m.answers.Project.Model == "" {
		m.answers.Project.Model = initwizard.ProjectModelOpenSource
	}
	if m.answers.License.SPDXID == "" && len(m.licenseOptions) > 0 {
		m.answers.License.SPDXID = m.licenseOptions[0]
	}
	if m.answers.Identity.Year == "" {
		m.answers.Identity.Year = "git"
	}
	if m.answers.HeaderStyle.Style == "" {
		m.answers.HeaderStyle.Style = "reuse+notice"
	}
	m.projectIndex = indexProjectModel(projectModelOptions, m.answers.Project.Model)
	m.licenseIndex = indexString(m.licenseOptions, m.answers.License.SPDXID)
	if len(m.licenseOptions) > 0 && m.answers.License.SPDXID != m.licenseOptions[m.licenseIndex] {
		m.answers.License.SPDXID = m.licenseOptions[m.licenseIndex]
	}
	m.styleIndex = indexString(headerStyleOptions, m.answers.HeaderStyle.Style)
	m.setInput()
	return m
}

func indexProjectModel(values []initwizard.ProjectModel, value initwizard.ProjectModel) int {
	for i, item := range values {
		if item == value {
			return i
		}
	}
	return 0
}

func indexString(values []string, value string) int {
	for i, item := range values {
		if item == value {
			return i
		}
	}
	return 0
}

func (m initWizardModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m initWizardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.Width = m.inputWidth()
		return m, nil
	case tea.KeyMsg:
		if m.step == initwizard.StepLicense {
			if m.handleLicenseKey(msg) {
				return m, nil
			}
		}
		switch msg.String() {
		case "ctrl+c", "esc":
			m.answers.Review.Confirmed = false
			return m, tea.Quit
		case "up", "k":
			m.moveSelection(-1)
			return m, nil
		case "down", "j":
			m.moveSelection(1)
			return m, nil
		case "pgup":
			m.moveSelection(-10)
			return m, nil
		case "pgdown":
			m.moveSelection(10)
			return m, nil
		case "home":
			m.moveSelectionToStart()
			return m, nil
		case "end":
			m.moveSelectionToEnd()
			return m, nil
		case " ", "y", "n":
			if m.step == initwizard.StepLicenseFiles {
				m.toggleBool(msg.String())
				return m, nil
			}
		case "backspace":
			if !m.usesTextInput() && m.step > initwizard.StepProjectModel {
				m.step--
				m.setInput()
				return m, nil
			}
		case "enter":
			if m.step == initwizard.StepLicense && len(m.filteredLicenseChoices()) == 0 {
				return m, nil
			}
			m.commitInput()
			if m.step == initwizard.StepReview {
				m.answers.Review.Confirmed = true
				return m, tea.Quit
			}
			m.step++
			m.setInput()
			return m, nil
		}
	}
	if m.usesTextInput() {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m initWizardModel) View() string {
	width := m.width
	if width <= 0 {
		width = 120
	}
	height := m.height
	if height <= 0 {
		height = 34
	}

	header := m.headerView(width)
	footer := m.footerView(width)
	bodyHeight := height - lipgloss.Height(header) - lipgloss.Height(footer) - 2
	if bodyHeight < 14 {
		bodyHeight = 14
	}

	return strings.TrimRight(lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		m.bodyView(width, bodyHeight),
		footer,
	), "\n")
}

func (m initWizardModel) stepTitle() string {
	return stepTitle(m.step)
}

func stepTitle(step initwizard.Step) string {
	switch step {
	case initwizard.StepProjectModel:
		return "Project model"
	case initwizard.StepLicense:
		return "License"
	case initwizard.StepIdentity:
		return "Identity"
	case initwizard.StepHeaderStyle:
		return "Header style"
	case initwizard.StepLicenseFiles:
		return "License files"
	case initwizard.StepCoverage:
		return "Coverage"
	default:
		return "Review"
	}
}

func (m initWizardModel) headerView(width int) string {
	title := "license-tool init"
	if m.root != "" {
		title += "  " + m.root
	}
	return appTitleStyle.Width(maxInt(1, width-2)).Render(title)
}

func (m initWizardModel) footerView(width int) string {
	keys := "enter next"
	if m.step == initwizard.StepReview {
		keys = "enter write"
	}
	keys += " | back back | esc cancel | arrows/pg move"
	if m.step == initwizard.StepLicense {
		keys += " | type filter | ctrl+u clear"
	}
	return mutedStyle.Width(width).Render(keys)
}

func (m initWizardModel) bodyView(width int, height int) string {
	if width < 100 {
		topHeight := height / 2
		if height >= 18 && topHeight < 11 {
			topHeight = 11
		}
		if height < 18 && topHeight < 7 {
			topHeight = 7
		}
		if topHeight > height-7 {
			topHeight = height - 7
		}
		if topHeight < 6 {
			topHeight = 6
		}
		previewHeight := height - topHeight - 1
		if previewHeight < 6 {
			previewHeight = 6
			topHeight = maxInt(6, height-previewHeight-1)
		}
		navWidth := 22
		if width < 70 {
			navWidth = 18
		}
		controlWidth := maxInt(24, width-navWidth-1)
		top := lipgloss.JoinHorizontal(
			lipgloss.Top,
			m.renderPane("Steps", m.navigationBody(), navWidth, topHeight, false),
			" ",
			m.renderPane(m.stepTitle(), m.stepBody(topHeight-3), controlWidth, topHeight, true),
		)
		return lipgloss.JoinVertical(
			lipgloss.Left,
			top,
			m.renderPane("Live preview", m.previewSummary(), width, previewHeight, false),
		)
	}

	gutter := 1
	navWidth := 24
	previewWidth := width / 2
	if previewWidth < 46 {
		previewWidth = 46
	}
	if previewWidth > 72 {
		previewWidth = 72
	}
	controlWidth := width - navWidth - previewWidth - gutter*2
	if controlWidth < 34 {
		controlWidth = 34
		previewWidth = width - navWidth - controlWidth - gutter*2
	}

	return lipgloss.JoinHorizontal(
		lipgloss.Top,
		m.renderPane("Steps", m.navigationBody(), navWidth, height, false),
		strings.Repeat(" ", gutter),
		m.renderPane(m.stepTitle(), m.stepBody(height-3), controlWidth, height, true),
		strings.Repeat(" ", gutter),
		m.renderPane("Live preview", m.previewSummary(), previewWidth, height, false),
	)
}

func (m initWizardModel) renderPane(title string, content string, width int, height int, active bool) string {
	if width < 12 {
		width = 12
	}
	if height < 6 {
		height = 6
	}
	innerWidth := maxInt(1, width-4)
	innerHeight := maxInt(1, height-2)
	body := paneTitleStyle.Render(title) + "\n" + fitText(content, innerWidth, innerHeight-1)
	style := paneStyle
	if active {
		style = activePaneStyle
	}
	return style.Width(innerWidth).Height(innerHeight).Render(body)
}

func (m initWizardModel) stepBody(rows int) string {
	switch m.step {
	case initwizard.StepProjectModel:
		return selectionList(projectModelsToStrings(projectModelOptions), m.projectIndex, rows)
	case initwizard.StepLicense:
		return m.licenseBody(rows)
	case initwizard.StepIdentity:
		return strings.Join([]string{
			"Copyright holder and year policy",
			"",
			m.input.View(),
			"",
			"Format: holder | year",
			"Year accepts current, git, YYYY, or YYYY-YYYY.",
		}, "\n")
	case initwizard.StepHeaderStyle:
		return selectionList(headerStyleOptions, m.styleIndex, rows)
	case initwizard.StepLicenseFiles:
		if m.answers.LicenseFiles.Manage {
			return "Manage LICENSE and LICENSES/<id>.txt\n\nCurrent value: yes\n\nPress space, y, or n to change."
		}
		return "Manage LICENSE and LICENSES/<id>.txt\n\nCurrent value: no\n\nPress space, y, or n to change."
	case initwizard.StepCoverage:
		return strings.Join([]string{
			"Default file scope",
			"",
			m.input.View(),
			"",
			"Format: include globs | exclude globs",
			"Examples: src/** | **/generated/**, vendor/**",
			"Leave include empty to scan all supported files.",
		}, "\n")
	default:
		return m.reviewBody()
	}
}

func (m initWizardModel) navigationBody() string {
	steps := []initwizard.Step{
		initwizard.StepProjectModel,
		initwizard.StepLicense,
		initwizard.StepIdentity,
		initwizard.StepHeaderStyle,
		initwizard.StepLicenseFiles,
		initwizard.StepCoverage,
		initwizard.StepReview,
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Step %d/%d\n", int(m.step)+1, len(steps))
	for _, step := range steps {
		mark := "  "
		if step < m.step {
			mark = "x "
		}
		if step == m.step {
			mark = "> "
		}
		fmt.Fprintf(&b, "%s%s\n", mark, stepTitle(step))
	}
	return strings.TrimRight(b.String(), "\n")
}

func projectModelsToStrings(values []initwizard.ProjectModel) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, string(value))
	}
	return out
}

func selectionList(values []string, selected int, rows int) string {
	if rows < 4 {
		rows = 4
	}
	visibleRows := rows - 2
	start := windowStart(selected, len(values), visibleRows)
	var b strings.Builder
	fmt.Fprintf(&b, "%-4s %s\n", "Sel", "Value")
	fmt.Fprintf(&b, "%-4s %s\n", "---", "-----")
	for i := start; i < len(values) && i < start+visibleRows; i++ {
		prefix := "  "
		if i == selected {
			prefix = "> "
		}
		fmt.Fprintf(&b, "%-4s %s\n", prefix, values[i])
	}
	if len(values) > visibleRows {
		fmt.Fprintf(&b, "\n%d/%d", selected+1, len(values))
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m initWizardModel) licenseBody(rows int) string {
	choices := m.filteredLicenseChoices()
	filter := m.licenseFilter
	if filter == "" {
		filter = "(none)"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Filter: %s    Selected: %s\n", filter, m.answers.License.SPDXID)
	if len(choices) == 0 {
		b.WriteString("No matching renderable licenses.\n\nBackspace or ctrl+u to change the filter.")
		return b.String()
	}

	selectedPos := m.licenseChoicePosition(choices)
	visibleRows := rows - 4
	if visibleRows < 4 {
		visibleRows = 4
	}
	start := windowStart(selectedPos, len(choices), visibleRows)
	fmt.Fprintf(&b, "%-4s %-24s %s\n", "Sel", "SPDX id", "Group")
	fmt.Fprintf(&b, "%-4s %-24s %s\n", "---", "-------", "-----")
	for pos := start; pos < len(choices) && pos < start+visibleRows; pos++ {
		choice := choices[pos]
		mark := "  "
		if choice.index == m.licenseIndex {
			mark = "> "
		}
		group := "other"
		if isCommonLicense(choice.id) {
			group = "common"
		}
		fmt.Fprintf(&b, "%-4s %-24s %s\n", mark, choice.id, group)
	}
	fmt.Fprintf(&b, "\n%d/%d matches, %d renderable total", selectedPos+1, len(choices), len(m.licenseOptions))
	return strings.TrimRight(b.String(), "\n")
}

func (m initWizardModel) reviewBody() string {
	rows := [][2]string{
		{"Project model", string(m.answers.Project.Model)},
		{"License", m.answers.License.SPDXID},
		{"Holder", valueOrPlaceholder(m.answers.Identity.Holder, "(required)")},
		{"Year", valueOrPlaceholder(m.answers.Identity.Year, "git")},
		{"Header style", m.answers.HeaderStyle.Style},
		{"Manage license files", yesNo(m.answers.LicenseFiles.Manage)},
		{"Include", listOrAll(m.answers.Coverage.Include)},
		{"Exclude", listOrNone(m.answers.Coverage.Exclude)},
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%-22s %s\n", "Field", "Value")
	fmt.Fprintf(&b, "%-22s %s\n", "-----", "-----")
	for _, row := range rows {
		fmt.Fprintf(&b, "%-22s %s\n", row[0], row[1])
	}
	b.WriteString("\nPress enter to write .license-tool.yaml.")
	return strings.TrimRight(b.String(), "\n")
}

func (m initWizardModel) previewSummary() string {
	cfg, _ := initwizard.Translate(m.answers, initwizard.TranslateOptions{AllowPlaceholders: true})
	sample := initwizard.SelectSample(m.previewPaths)
	sourcePreview, sourceErr := initwizard.BuildSourcePreview(initwizard.SourcePreviewInput{
		Config:       cfg,
		Sample:       sample,
		ResolvedYear: previewResolvedYear(cfg.Year),
	})
	yamlPreview, yamlErr := initwizard.BuildYAMLPreview(cfg)

	var b strings.Builder
	if sourceErr == nil {
		fmt.Fprintf(&b, "%s  %s\n\n", sourcePreview.Language, sourcePreview.Path)
		b.WriteString(sourcePreview.Content)
	} else {
		fmt.Fprintf(&b, "Source preview unavailable: %s\n", sourceErr)
	}
	if yamlErr == nil {
		fmt.Fprintf(&b, "\n\n.license-tool.yaml\n\n%s", strings.TrimRight(string(yamlPreview), "\n"))
	} else {
		fmt.Fprintf(&b, "\n\nYAML preview unavailable: %s", yamlErr)
	}
	fmt.Fprintf(&b, "\n\nLicense files: %s", yesNo(cfg.ManageLicenseFile))
	fmt.Fprintf(&b, "\nCoverage include: %s", listOrAll(cfg.Includes))
	fmt.Fprintf(&b, "\nCoverage exclude: %s", listOrNone(cfg.Excludes))
	return strings.TrimRight(b.String(), "\n")
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func (m *initWizardModel) handleLicenseKey(msg tea.KeyMsg) bool {
	switch msg.Type {
	case tea.KeyRunes:
		m.licenseFilter += string(msg.Runes)
		m.alignLicenseSelection()
		return true
	case tea.KeyBackspace:
		if m.licenseFilter == "" {
			return false
		}
		runes := []rune(m.licenseFilter)
		m.licenseFilter = string(runes[:len(runes)-1])
		m.alignLicenseSelection()
		return true
	}
	switch msg.String() {
	case "ctrl+u":
		m.licenseFilter = ""
		m.alignLicenseSelection()
		return true
	case "up", "k":
		m.moveSelection(-1)
		return true
	case "down", "j":
		m.moveSelection(1)
		return true
	case "pgup":
		m.moveSelection(-10)
		return true
	case "pgdown":
		m.moveSelection(10)
		return true
	case "home":
		m.moveSelectionToStart()
		return true
	case "end":
		m.moveSelectionToEnd()
		return true
	}
	return false
}

func (m *initWizardModel) moveSelection(delta int) {
	switch m.step {
	case initwizard.StepProjectModel:
		m.projectIndex = wrapIndex(m.projectIndex+delta, len(projectModelOptions))
		m.answers.Project.Model = projectModelOptions[m.projectIndex]
	case initwizard.StepLicense:
		choices := m.filteredLicenseChoices()
		if len(choices) == 0 {
			return
		}
		pos := clampInt(m.licenseChoicePosition(choices)+delta, 0, len(choices)-1)
		m.licenseIndex = choices[pos].index
		m.answers.License.SPDXID = choices[pos].id
	case initwizard.StepHeaderStyle:
		m.styleIndex = wrapIndex(m.styleIndex+delta, len(headerStyleOptions))
		m.answers.HeaderStyle.Style = headerStyleOptions[m.styleIndex]
	}
}

func (m *initWizardModel) moveSelectionToStart() {
	switch m.step {
	case initwizard.StepProjectModel:
		m.projectIndex = 0
		m.answers.Project.Model = projectModelOptions[m.projectIndex]
	case initwizard.StepLicense:
		choices := m.filteredLicenseChoices()
		if len(choices) == 0 {
			return
		}
		m.licenseIndex = choices[0].index
		m.answers.License.SPDXID = choices[0].id
	case initwizard.StepHeaderStyle:
		m.styleIndex = 0
		m.answers.HeaderStyle.Style = headerStyleOptions[m.styleIndex]
	}
}

func (m *initWizardModel) moveSelectionToEnd() {
	switch m.step {
	case initwizard.StepProjectModel:
		m.projectIndex = len(projectModelOptions) - 1
		m.answers.Project.Model = projectModelOptions[m.projectIndex]
	case initwizard.StepLicense:
		choices := m.filteredLicenseChoices()
		if len(choices) == 0 {
			return
		}
		last := choices[len(choices)-1]
		m.licenseIndex = last.index
		m.answers.License.SPDXID = last.id
	case initwizard.StepHeaderStyle:
		m.styleIndex = len(headerStyleOptions) - 1
		m.answers.HeaderStyle.Style = headerStyleOptions[m.styleIndex]
	}
}

type licenseChoice struct {
	index int
	id    string
}

func (m initWizardModel) filteredLicenseChoices() []licenseChoice {
	filter := strings.ToLower(strings.TrimSpace(m.licenseFilter))
	choices := make([]licenseChoice, 0, len(m.licenseOptions))
	for i, id := range m.licenseOptions {
		if filter != "" && !strings.Contains(strings.ToLower(id), filter) {
			continue
		}
		choices = append(choices, licenseChoice{index: i, id: id})
	}
	return choices
}

func (m initWizardModel) licenseChoicePosition(choices []licenseChoice) int {
	for i, choice := range choices {
		if choice.index == m.licenseIndex {
			return i
		}
	}
	return 0
}

func (m *initWizardModel) alignLicenseSelection() {
	choices := m.filteredLicenseChoices()
	if len(choices) == 0 {
		return
	}
	for _, choice := range choices {
		if choice.index == m.licenseIndex {
			m.answers.License.SPDXID = choice.id
			return
		}
	}
	m.licenseIndex = choices[0].index
	m.answers.License.SPDXID = choices[0].id
}

func isCommonLicense(id string) bool {
	for _, common := range spdx.CommonIDs() {
		if common == id {
			return true
		}
	}
	return false
}

func wrapIndex(value, size int) int {
	if size == 0 {
		return 0
	}
	for value < 0 {
		value += size
	}
	return value % size
}

func (m *initWizardModel) toggleBool(key string) {
	if m.step != initwizard.StepLicenseFiles {
		return
	}
	switch key {
	case "y":
		m.answers.LicenseFiles.Manage = true
	case "n":
		m.answers.LicenseFiles.Manage = false
	default:
		m.answers.LicenseFiles.Manage = !m.answers.LicenseFiles.Manage
	}
}

func (m initWizardModel) usesTextInput() bool {
	return m.step == initwizard.StepIdentity || m.step == initwizard.StepCoverage
}

func (m *initWizardModel) setInput() {
	ti := textinput.New()
	ti.Width = m.inputWidth()
	switch m.step {
	case initwizard.StepIdentity:
		ti.Placeholder = "holder | year, for example Acme, LLC | git"
		ti.SetValue(strings.TrimSpace(m.answers.Identity.Holder + " | " + m.answers.Identity.Year))
		ti.Focus()
	case initwizard.StepCoverage:
		ti.Placeholder = "include globs | exclude globs"
		ti.SetValue(coverageInputValue(m.answers.Coverage))
		ti.Focus()
	}
	m.input = ti
}

func (m *initWizardModel) commitInput() {
	switch m.step {
	case initwizard.StepIdentity:
		holder, year, ok := strings.Cut(m.input.Value(), "|")
		if ok {
			m.answers.Identity.Holder = strings.TrimSpace(holder)
			m.answers.Identity.Year = strings.TrimSpace(year)
			return
		}
		m.answers.Identity.Holder = strings.TrimSpace(m.input.Value())
	case initwizard.StepCoverage:
		include, exclude, hasSeparator := strings.Cut(m.input.Value(), "|")
		if hasSeparator {
			m.answers.Coverage.Include = splitCSV(include)
			m.answers.Coverage.Exclude = splitCSV(exclude)
			return
		}
		m.answers.Coverage.Include = splitCSV(m.input.Value())
		m.answers.Coverage.Exclude = nil
	}
}

func coverageInputValue(a initwizard.CoverageAnswer) string {
	include := strings.Join(a.Include, ", ")
	exclude := strings.Join(a.Exclude, ", ")
	if include == "" && exclude == "" {
		return ""
	}
	return include + " | " + exclude
}

func splitCSV(raw string) []string {
	var out []string
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func previewResolvedYear(y model.YearSpec) string {
	current := time.Now().Year()
	switch y.Kind {
	case model.YearCurrent:
		return fmt.Sprintf("%d", current)
	case model.YearExplicit:
		if y.Start > 0 {
			return fmt.Sprintf("%d", y.Start)
		}
	case model.YearRange:
		if y.Start > 0 && y.End > 0 {
			return fmt.Sprintf("%d-%d", y.Start, y.End)
		}
	}
	return fmt.Sprintf("2021-%d", current)
}

func previewCandidatePaths(root string) []string {
	if root == "" {
		return nil
	}
	var paths []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != root && ignoredPreviewDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)
		if _, ok := initwizard.SampleForPath(rel); ok {
			paths = append(paths, rel)
		}
		return nil
	})
	sort.Strings(paths)
	return paths
}

func ignoredPreviewDir(name string) bool {
	switch name {
	case ".git", ".hg", ".svn", ".cache", ".next", "build", "dist", "node_modules", "target", "vendor":
		return true
	default:
		return false
	}
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

func valueOrPlaceholder(value string, placeholder string) string {
	if strings.TrimSpace(value) == "" {
		return placeholder
	}
	return value
}

func (m initWizardModel) inputWidth() int {
	width := m.width
	if width <= 0 {
		width = 120
	}
	if width < 100 {
		return maxInt(20, width-8)
	}
	previewWidth := width / 2
	if previewWidth < 46 {
		previewWidth = 46
	}
	if previewWidth > 72 {
		previewWidth = 72
	}
	controlWidth := width - 24 - previewWidth - 2
	return maxInt(20, controlWidth-8)
}

func fitText(text string, width int, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	raw := strings.Split(text, "\n")
	lines := make([]string, 0, minInt(len(raw), height))
	for _, line := range raw {
		lines = append(lines, ellipsize(line, width))
		if len(lines) == height {
			break
		}
	}
	if len(raw) > height && height > 0 {
		lines[height-1] = ellipsize("...", width)
	}
	return strings.Join(lines, "\n")
}

func ellipsize(line string, width int) string {
	runes := []rune(line)
	if len(runes) <= width {
		return line
	}
	if width <= 3 {
		return string(runes[:width])
	}
	return string(runes[:width-3]) + "..."
}

func windowStart(selected int, total int, rows int) int {
	if total <= rows || rows <= 0 {
		return 0
	}
	selected = clampInt(selected, 0, total-1)
	start := selected - rows/2
	if start < 0 {
		return 0
	}
	if start+rows > total {
		return total - rows
	}
	return start
}

func clampInt(value int, minValue int, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func runInitWizard(path string, a initwizard.Answers) (initwizard.Answers, error) {
	program := tea.NewProgram(
		newInitWizardModel(path, a),
		tea.WithInput(os.Stdin),
		tea.WithOutput(os.Stderr),
		tea.WithAltScreen(),
	)
	final, err := program.Run()
	if err != nil {
		return a, err
	}
	m, ok := final.(initWizardModel)
	if !ok {
		return a, fmt.Errorf("init wizard returned unexpected model")
	}
	if !m.answers.Review.Confirmed {
		return a, initwizard.ErrAborted
	}
	return m.answers, nil
}

// collectInteractive runs the wizard when interactive is true, otherwise returning
// the flag-derived answers unchanged for deterministic non-TTY behavior.
func collectInteractive(path string, a initwizard.Answers, interactive bool) (initwizard.Answers, error) {
	if !interactive {
		return a, nil
	}
	return runInitWizard(path, a)
}
