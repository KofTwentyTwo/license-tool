package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KofTwentyTwo/license-tool/internal/initwizard"
)

// initWizardModel is a thin Bubble Tea adapter. WHY it carries no logic: every
// decision (field state, validation, layout, rendering, the single Answers->Config
// translation) lives in the pure, fully-tested internal/initwizard package; this
// shell only forwards window sizes and keystrokes to FormState transitions and
// renders via initwizard.Render. It is excluded from the coverage gate precisely
// because it is this trivial.
type initWizardModel struct {
	form      initwizard.FormState
	sample    initwizard.Sample
	nowYear   int
	width     int
	height    int
	confirmed bool
}

func newInitWizardModel(root string, a initwizard.Answers) initWizardModel {
	seeded, detected := initwizard.Seed(root, a, initwizard.SeedDeps{})
	return initWizardModel{
		form:    initwizard.NewForm(seeded, detected),
		sample:  initwizard.SelectSample(previewCandidatePaths(root)),
		nowYear: time.Now().Year(),
	}
}

func (m initWizardModel) Init() tea.Cmd { return nil }

func (m initWizardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m initWizardModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyCtrlS:
		if m.form.CanWrite() {
			m.confirmed = true
			return m, tea.Quit
		}
		return m, nil
	}

	if m.form.Editing() {
		return m.handleEditingKey(msg)
	}
	return m.handleNavKey(msg)
}

func (m initWizardModel) handleEditingKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.form.Cancel()
	case tea.KeyEnter:
		m.form.Commit()
	case tea.KeyUp:
		m.form.MoveCursor(-1)
	case tea.KeyDown:
		m.form.MoveCursor(1)
	case tea.KeyDelete:
		m.form.Remove()
	case tea.KeyBackspace:
		m.form.Backspace()
	case tea.KeyRunes, tea.KeySpace:
		for _, r := range msg.Runes {
			m.form.Input(r)
		}
	}
	return m, nil
}

func (m initWizardModel) handleNavKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		if m.form.Cancel() == initwizard.ActionQuit {
			return m, tea.Quit
		}
	case tea.KeyEnter, tea.KeySpace:
		if m.form.Activate() == initwizard.ActionWrite {
			m.confirmed = true
			return m, tea.Quit
		}
	case tea.KeyUp:
		m.form.MoveFocus(-1)
	case tea.KeyDown, tea.KeyTab:
		m.form.MoveFocus(1)
	case tea.KeyShiftTab:
		m.form.MoveFocus(-1)
	}
	return m, nil
}

func (m initWizardModel) View() string {
	width, height := m.width, m.height
	if width <= 0 {
		width = 120
	}
	if height <= 0 {
		height = 34
	}
	// Reserve two rows for the footer that Render appends below the panels.
	plan := initwizard.Layout(width, height-2)
	return initwizard.Render(m.form, plan, m.sample, m.nowYear)
}

// previewCandidatePaths walks root for files whose extension maps to a preview
// sample, so SelectSample can pick a language family present in the repo.
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
	if !m.confirmed {
		return a, initwizard.ErrAborted
	}
	return m.form.Answers(), nil
}

// collectInteractive runs the wizard when interactive is true, otherwise returning
// the flag-derived answers unchanged for deterministic non-TTY behavior.
func collectInteractive(path string, a initwizard.Answers, interactive bool) (initwizard.Answers, error) {
	if !interactive {
		return a, nil
	}
	return runInitWizard(path, a)
}
