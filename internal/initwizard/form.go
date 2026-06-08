package initwizard

import (
	"strings"

	"github.com/KofTwentyTwo/license-tool/internal/config"
	"github.com/KofTwentyTwo/license-tool/internal/model"
	"github.com/KofTwentyTwo/license-tool/internal/spdx"
)

// Field identifies one editable row of the single-screen init form, in display
// order. FieldWrite is the trailing "write" affordance, not a value.
type Field int

const (
	FieldLicense Field = iota
	FieldHolder
	FieldYear
	FieldStyle
	FieldManageFiles
	FieldInclude
	FieldExclude
	FieldWrite
)

// fieldCount is the number of focusable rows (values + the write action).
const fieldCount = int(FieldWrite) + 1

// Label is the human label for a field row.
func (f Field) Label() string {
	switch f {
	case FieldLicense:
		return "License"
	case FieldHolder:
		return "Holder"
	case FieldYear:
		return "Year"
	case FieldStyle:
		return "Header style"
	case FieldManageFiles:
		return "Manage license files"
	case FieldInclude:
		return "Include globs"
	case FieldExclude:
		return "Exclude globs"
	default:
		return "Write .license-tool.yaml"
	}
}

// Action is the outcome the adapter must act on after a transition: write the config
// or quit. Most transitions return ActionNone.
type Action int

const (
	// ActionNone means the transition only changed in-form state.
	ActionNone Action = iota
	// ActionWrite means the user confirmed a valid write.
	ActionWrite
	// ActionQuit means the user asked to leave without writing.
	ActionQuit
)

// FieldError is a single failed validation, addressed to the field that owns it.
type FieldError struct {
	Field Field
	Msg   string
}

// styleOptions are the selectable header styles, in display order.
var styleOptions = []string{"reuse", "notice", "reuse+notice"}

// FormState is the pure, PTY-free state of the init form: the answers being edited,
// which fields were repo-detected, the focused row, and any open editor's transient
// cursor/buffer. The Bubble Tea adapter maps keystrokes to the transition methods
// and renders via Render; all behavior is unit-tested here so the adapter stays a
// trivial, coverage-excluded shell.
type FormState struct {
	answers  Answers
	detected Detected
	focus    Field
	editing  bool

	licenseOptions []string
	licenseFilter  string
	licenseCursor  int

	styleCursor int

	textBuffer string

	globBuffer string
	globCursor int
}

// NewForm builds a form from seeded answers, applying the documented defaults for any
// field the caller left unset (license = first option, year = git, style = reuse+notice).
func NewForm(a Answers, detected Detected) FormState {
	s := FormState{
		answers:        a,
		detected:       detected,
		focus:          FieldLicense,
		licenseOptions: licenseSelectOptions(),
	}
	if s.answers.License.SPDXID == "" && len(s.licenseOptions) > 0 {
		s.answers.License.SPDXID = s.licenseOptions[0]
	}
	if s.answers.Identity.Year == "" {
		s.answers.Identity.Year = "git"
	}
	if s.answers.HeaderStyle.Style == "" {
		s.answers.HeaderStyle.Style = "reuse+notice"
	}
	return s
}

// licenseSelectOptions lists renderable SPDX ids with the common ones first, then the
// remaining renderable ids in sorted order, without duplicates.
func licenseSelectOptions() []string {
	return selectOptions(spdx.CommonIDs(), spdx.RenderableIDs())
}

// selectOptions merges the editorial common list (kept first, in order) with the full
// renderable list (appended, deduped). Split out from licenseSelectOptions so the
// dedup/append behavior is unit-testable independent of the vendored snapshot, where
// the common set currently happens to cover every renderable id.
func selectOptions(common, renderable []string) []string {
	seen := make(map[string]bool, len(common))
	opts := make([]string, 0, len(common)+len(renderable))
	for _, id := range common {
		seen[id] = true
		opts = append(opts, id)
	}
	for _, id := range renderable {
		if !seen[id] {
			opts = append(opts, id)
		}
	}
	return opts
}

// Editing reports whether the focused field's inline editor is open.
func (s FormState) Editing() bool { return s.editing }

// Focused returns the focused field.
func (s FormState) Focused() Field { return s.focus }

// Answers returns the current answers (a snapshot for the adapter to translate).
func (s FormState) Answers() Answers { return s.answers }

// WriteConfig translates the current answers strictly, for the write path.
func (s FormState) WriteConfig() (model.Config, error) {
	return Translate(s.answers, TranslateOptions{})
}

// MoveFocus moves the focused row by delta, wrapping. No-op while an editor is open.
func (s *FormState) MoveFocus(delta int) {
	if s.editing {
		return
	}
	s.focus = Field(((int(s.focus)+delta)%fieldCount + fieldCount) % fieldCount)
}

// Activate handles Enter (or space) on the focused row: toggles the manage flag,
// requests a write from the write row (only when valid), or opens an inline editor.
func (s *FormState) Activate() Action {
	switch s.focus {
	case FieldManageFiles:
		s.answers.LicenseFiles.Manage = !s.answers.LicenseFiles.Manage
		return ActionNone
	case FieldWrite:
		if s.CanWrite() {
			return ActionWrite
		}
		return ActionNone
	default:
		s.openEditor()
		return ActionNone
	}
}

func (s *FormState) openEditor() {
	s.editing = true
	switch s.focus {
	case FieldLicense:
		s.licenseFilter = ""
		s.licenseCursor = s.licenseIndexOf(s.answers.License.SPDXID)
	case FieldHolder:
		s.textBuffer = s.answers.Identity.Holder
	case FieldYear:
		s.textBuffer = s.answers.Identity.Year
	case FieldStyle:
		s.styleCursor = optionIndex(styleOptions, s.answers.HeaderStyle.Style)
	case FieldInclude, FieldExclude:
		s.globBuffer = ""
		s.globCursor = 0
	}
}

// Cancel handles Esc: close an open editor (discarding its buffer), or quit when no
// editor is open.
func (s *FormState) Cancel() Action {
	if s.editing {
		s.editing = false
		return ActionNone
	}
	return ActionQuit
}

// Input feeds a typed rune into the active editor's buffer (license filter, text
// field, or glob entry). No-op outside an editor or for non-text editors.
func (s *FormState) Input(r rune) {
	if !s.editing {
		return
	}
	switch s.focus {
	case FieldLicense:
		s.licenseFilter += string(r)
		s.clampLicenseCursor()
	case FieldHolder, FieldYear:
		s.textBuffer += string(r)
	case FieldInclude, FieldExclude:
		s.globBuffer += string(r)
	}
}

// Backspace deletes the last rune of the active editor's buffer.
func (s *FormState) Backspace() {
	if !s.editing {
		return
	}
	switch s.focus {
	case FieldLicense:
		s.licenseFilter = dropLastRune(s.licenseFilter)
		s.clampLicenseCursor()
	case FieldHolder, FieldYear:
		s.textBuffer = dropLastRune(s.textBuffer)
	case FieldInclude, FieldExclude:
		s.globBuffer = dropLastRune(s.globBuffer)
	}
}

// MoveCursor moves the selection within an open list/choice editor (license choices,
// style options, or existing glob entries).
func (s *FormState) MoveCursor(delta int) {
	if !s.editing {
		return
	}
	switch s.focus {
	case FieldLicense:
		if n := len(s.filteredLicenses()); n > 0 {
			s.licenseCursor = clamp(s.licenseCursor+delta, 0, n-1)
		}
	case FieldStyle:
		s.styleCursor = clamp(s.styleCursor+delta, 0, len(styleOptions)-1)
	case FieldInclude, FieldExclude:
		if n := len(s.globEntries()); n > 0 {
			s.globCursor = clamp(s.globCursor+delta, 0, n-1)
		}
	}
}

// Commit handles Enter inside an editor: choose the selected license/style, store the
// text field, or append the buffered glob (the glob editor stays open for more adds).
func (s *FormState) Commit() {
	if !s.editing {
		return
	}
	switch s.focus {
	case FieldLicense:
		if choices := s.filteredLicenses(); len(choices) > 0 {
			s.answers.License.SPDXID = choices[clamp(s.licenseCursor, 0, len(choices)-1)]
		}
		s.editing = false
	case FieldStyle:
		s.answers.HeaderStyle.Style = styleOptions[clamp(s.styleCursor, 0, len(styleOptions)-1)]
		s.editing = false
	case FieldHolder:
		s.answers.Identity.Holder = strings.TrimSpace(s.textBuffer)
		s.editing = false
	case FieldYear:
		s.answers.Identity.Year = strings.TrimSpace(s.textBuffer)
		s.editing = false
	case FieldInclude:
		if g := strings.TrimSpace(s.globBuffer); g != "" {
			s.answers.Coverage.Include = append(s.answers.Coverage.Include, g)
			s.globBuffer = ""
		}
	case FieldExclude:
		if g := strings.TrimSpace(s.globBuffer); g != "" {
			s.answers.Coverage.Exclude = append(s.answers.Coverage.Exclude, g)
			s.globBuffer = ""
		}
	}
}

// Remove deletes the selected entry from the focused glob list editor.
func (s *FormState) Remove() {
	if !s.editing {
		return
	}
	switch s.focus {
	case FieldInclude:
		s.answers.Coverage.Include = removeAt(s.answers.Coverage.Include, s.globCursor)
		s.clampGlobCursor()
	case FieldExclude:
		s.answers.Coverage.Exclude = removeAt(s.answers.Coverage.Exclude, s.globCursor)
		s.clampGlobCursor()
	}
}

// Validate returns every failing field-level rule. An empty result means the answers
// can be written.
func (s FormState) Validate() []FieldError {
	var errs []FieldError
	if _, ok := spdx.Lookup(strings.TrimSpace(s.answers.License.SPDXID)); !ok {
		errs = append(errs, FieldError{FieldLicense, "choose a supported license"})
	}
	if strings.TrimSpace(s.answers.Identity.Holder) == "" {
		errs = append(errs, FieldError{FieldHolder, "holder is required"})
	}
	if y := strings.TrimSpace(s.answers.Identity.Year); y != "" {
		if _, err := config.ParseYearSpec(y); err != nil {
			errs = append(errs, FieldError{FieldYear, "use current, git, YYYY, or YYYY-YYYY"})
		}
	}
	for _, g := range s.answers.Coverage.Include {
		if !validGlob(g) {
			errs = append(errs, FieldError{FieldInclude, "glob entries cannot be blank"})
			break
		}
	}
	for _, g := range s.answers.Coverage.Exclude {
		if !validGlob(g) {
			errs = append(errs, FieldError{FieldExclude, "glob entries cannot be blank"})
			break
		}
	}
	return errs
}

// CanWrite reports whether the answers pass every validation rule.
func (s FormState) CanWrite() bool {
	return len(s.Validate()) == 0
}

// BlockingReason returns the first validation message, or "" when writable. The
// footer shows this to explain why the write action is disabled.
func (s FormState) BlockingReason() string {
	if errs := s.Validate(); len(errs) > 0 {
		return errs[0].Msg
	}
	return ""
}

func (s FormState) filteredLicenses() []string {
	f := strings.ToLower(strings.TrimSpace(s.licenseFilter))
	if f == "" {
		return s.licenseOptions
	}
	out := make([]string, 0, len(s.licenseOptions))
	for _, id := range s.licenseOptions {
		if strings.Contains(strings.ToLower(id), f) {
			out = append(out, id)
		}
	}
	return out
}

func (s FormState) licenseIndexOf(id string) int {
	return optionIndex(s.filteredLicenses(), id)
}

func (s *FormState) clampLicenseCursor() {
	n := len(s.filteredLicenses())
	if n == 0 {
		s.licenseCursor = 0
		return
	}
	s.licenseCursor = clamp(s.licenseCursor, 0, n-1)
}

func (s FormState) globEntries() []string {
	if s.focus == FieldExclude {
		return s.answers.Coverage.Exclude
	}
	return s.answers.Coverage.Include
}

func (s *FormState) clampGlobCursor() {
	n := len(s.globEntries())
	if n == 0 {
		s.globCursor = 0
		return
	}
	s.globCursor = clamp(s.globCursor, 0, n-1)
}

func validGlob(g string) bool {
	return strings.TrimSpace(g) != ""
}

func optionIndex(values []string, value string) int {
	for i, v := range values {
		if v == value {
			return i
		}
	}
	return 0
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func dropLastRune(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	return string(r[:len(r)-1])
}

func removeAt(values []string, i int) []string {
	if i < 0 || i >= len(values) {
		return values
	}
	return append(values[:i], values[i+1:]...)
}
