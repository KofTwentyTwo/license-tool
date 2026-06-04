package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/KofTwentyTwo/license-tool/internal/initwizard"
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
		return m, nil
	case tea.KeyMsg:
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
	var b strings.Builder
	fmt.Fprintf(&b, "license-tool init\n")
	if m.root != "" {
		fmt.Fprintf(&b, "path: %s\n", m.root)
	}
	fmt.Fprintf(&b, "\n%s\n\n", m.stepTitle())
	b.WriteString(m.stepBody())
	fmt.Fprintf(&b, "\n\n%s\n", m.previewSummary())
	fmt.Fprintf(&b, "\nenter next, backspace back, esc cancel")
	if m.step == initwizard.StepReview {
		fmt.Fprintf(&b, "\nenter confirms and writes .license-tool.yaml")
	}
	return b.String()
}

func (m initWizardModel) stepTitle() string {
	switch m.step {
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

func (m initWizardModel) stepBody() string {
	switch m.step {
	case initwizard.StepProjectModel:
		return optionList(projectModelsToStrings(projectModelOptions), m.projectIndex)
	case initwizard.StepLicense:
		return optionList(m.licenseOptions, m.licenseIndex)
	case initwizard.StepIdentity:
		return m.input.View()
	case initwizard.StepHeaderStyle:
		return optionList(headerStyleOptions, m.styleIndex)
	case initwizard.StepLicenseFiles:
		if m.answers.LicenseFiles.Manage {
			return "Manage LICENSE and LICENSES/<id>.txt: yes"
		}
		return "Manage LICENSE and LICENSES/<id>.txt: no"
	case initwizard.StepCoverage:
		return m.input.View()
	default:
		return m.reviewBody()
	}
}

func projectModelsToStrings(values []initwizard.ProjectModel) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, string(value))
	}
	return out
}

func optionList(values []string, selected int) string {
	var b strings.Builder
	for i, value := range values {
		prefix := "  "
		if i == selected {
			prefix = "> "
		}
		fmt.Fprintf(&b, "%s%s\n", prefix, value)
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m initWizardModel) reviewBody() string {
	return strings.Join([]string{
		"Project model: " + string(m.answers.Project.Model),
		"License: " + m.answers.License.SPDXID,
		"Holder: " + m.answers.Identity.Holder,
		"Year: " + m.answers.Identity.Year,
		"Header style: " + m.answers.HeaderStyle.Style,
		"Manage license files: " + yesNo(m.answers.LicenseFiles.Manage),
		"Include: " + strings.Join(m.answers.Coverage.Include, ", "),
		"Exclude: " + strings.Join(m.answers.Coverage.Exclude, ", "),
	}, "\n")
}

func (m initWizardModel) previewSummary() string {
	return "Preview engine: pending integration with the sample renderer. Current review values are shown above."
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func (m *initWizardModel) moveSelection(delta int) {
	switch m.step {
	case initwizard.StepProjectModel:
		m.projectIndex = wrapIndex(m.projectIndex+delta, len(projectModelOptions))
		m.answers.Project.Model = projectModelOptions[m.projectIndex]
	case initwizard.StepLicense:
		m.licenseIndex = wrapIndex(m.licenseIndex+delta, len(m.licenseOptions))
		if len(m.licenseOptions) > 0 {
			m.answers.License.SPDXID = m.licenseOptions[m.licenseIndex]
		}
	case initwizard.StepHeaderStyle:
		m.styleIndex = wrapIndex(m.styleIndex+delta, len(headerStyleOptions))
		m.answers.HeaderStyle.Style = headerStyleOptions[m.styleIndex]
	}
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
	switch m.step {
	case initwizard.StepIdentity:
		ti.Placeholder = "holder | year, for example Acme, LLC | git"
		ti.SetValue(strings.TrimSpace(m.answers.Identity.Holder + " | " + m.answers.Identity.Year))
		ti.Focus()
	case initwizard.StepCoverage:
		ti.Placeholder = "exclude globs, comma-separated"
		ti.SetValue(strings.Join(m.answers.Coverage.Exclude, ", "))
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
		m.answers.Coverage.Exclude = splitCSV(m.input.Value())
	}
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
