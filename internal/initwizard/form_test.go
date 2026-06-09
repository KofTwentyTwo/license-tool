package initwizard

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validForm() FormState {
	return NewForm(Answers{
		License:  LicenseAnswer{SPDXID: "MIT"},
		Identity: IdentityAnswer{Holder: "Acme, LLC", Year: "git"},
	}, Detected{})
}

func TestFieldLabel(t *testing.T) {
	for f, want := range map[Field]string{
		FieldLicense:     "License",
		FieldHolder:      "Holder",
		FieldYear:        "Year",
		FieldStyle:       "Header style",
		FieldManageFiles: "Manage license files",
		FieldInclude:     "Include globs",
		FieldExclude:     "Exclude globs",
		FieldWrite:       "Write .license-tool.yaml",
	} {
		assert.Equal(t, want, f.Label())
	}
}

func TestNewFormDefaults(t *testing.T) {
	s := NewForm(Answers{}, Detected{})
	require.NotEmpty(t, s.licenseOptions)
	assert.Equal(t, s.licenseOptions[0], s.answers.License.SPDXID)
	assert.Equal(t, "git", s.answers.Identity.Year)
	assert.Equal(t, "reuse+notice", s.answers.HeaderStyle.Style)
	assert.Equal(t, FieldLicense, s.Focused())
	assert.False(t, s.Editing())
}

func TestNewFormPreservesPresets(t *testing.T) {
	s := NewForm(Answers{
		License:     LicenseAnswer{SPDXID: "Apache-2.0"},
		Identity:    IdentityAnswer{Year: "2024"},
		HeaderStyle: HeaderStyleAnswer{Style: "reuse"},
	}, Detected{License: true})
	assert.Equal(t, "Apache-2.0", s.answers.License.SPDXID)
	assert.Equal(t, "2024", s.answers.Identity.Year)
	assert.Equal(t, "reuse", s.answers.HeaderStyle.Style)
}

func TestMoveFocusWraps(t *testing.T) {
	s := validForm()
	s.MoveFocus(-1)
	assert.Equal(t, FieldWrite, s.Focused())
	s.MoveFocus(1)
	assert.Equal(t, FieldLicense, s.Focused())

	s.editing = true
	s.MoveFocus(1) // no-op while editing
	assert.Equal(t, FieldLicense, s.Focused())
}

func TestActivateManageToggles(t *testing.T) {
	s := validForm()
	s.focus = FieldManageFiles
	assert.False(t, s.answers.LicenseFiles.Manage)
	assert.Equal(t, ActionNone, s.Activate())
	assert.True(t, s.answers.LicenseFiles.Manage)
	assert.False(t, s.Editing())
}

func TestActivateWrite(t *testing.T) {
	t.Run("valid form requests write", func(t *testing.T) {
		s := validForm()
		s.focus = FieldWrite
		assert.Equal(t, ActionWrite, s.Activate())
	})
	t.Run("invalid form does not write", func(t *testing.T) {
		s := NewForm(Answers{License: LicenseAnswer{SPDXID: "MIT"}}, Detected{}) // empty holder
		s.focus = FieldWrite
		assert.Equal(t, ActionNone, s.Activate())
	})
}

func TestActivateOpensEditors(t *testing.T) {
	for _, f := range []Field{FieldLicense, FieldHolder, FieldYear, FieldStyle, FieldInclude, FieldExclude} {
		s := validForm()
		s.focus = f
		assert.Equal(t, ActionNone, s.Activate())
		assert.True(t, s.Editing(), "field %v should open an editor", f)
	}
}

func TestCancel(t *testing.T) {
	s := validForm()
	s.focus = FieldHolder
	s.Activate()
	require.True(t, s.Editing())
	assert.Equal(t, ActionNone, s.Cancel())
	assert.False(t, s.Editing())

	assert.Equal(t, ActionQuit, s.Cancel())
}

func TestLicenseFilterAndCommit(t *testing.T) {
	s := validForm()
	s.focus = FieldLicense
	s.Activate()
	for _, r := range "apache" {
		s.Input(r)
	}
	choices := s.filteredLicenses()
	require.NotEmpty(t, choices)
	assert.Contains(t, choices[0], "Apache")
	s.MoveCursor(1) // exercise the license-cursor move with a non-empty result set
	s.Commit()
	assert.False(t, s.Editing())
	assert.Equal(t, "Apache-2.0", s.answers.License.SPDXID)
}

func TestLicenseBackspaceAndEmptyFilter(t *testing.T) {
	s := validForm()
	s.focus = FieldLicense
	s.Activate()
	for _, r := range "zzz" {
		s.Input(r)
	}
	assert.Empty(t, s.filteredLicenses())
	s.MoveCursor(1) // n==0 path
	s.Commit()      // choices==0 path: license unchanged, editor closes
	assert.Equal(t, "MIT", s.answers.License.SPDXID)
	assert.False(t, s.Editing())

	s.Activate()
	s.Input('m')
	s.Backspace()
	assert.Equal(t, s.licenseOptions, s.filteredLicenses())
}

func TestStyleEditor(t *testing.T) {
	s := validForm()
	s.focus = FieldStyle
	s.Activate()
	s.MoveCursor(-100) // clamps to first
	s.Commit()
	assert.Equal(t, "reuse", s.answers.HeaderStyle.Style)

	s.Activate()
	s.MoveCursor(100) // clamps to last
	s.Commit()
	assert.Equal(t, "reuse+notice", s.answers.HeaderStyle.Style)
}

func TestTextEditors(t *testing.T) {
	s := validForm()
	s.focus = FieldHolder
	s.Activate()
	s.Backspace() // trim a char from "Acme, LLC"
	for _, r := range " Inc" {
		s.Input(r)
	}
	s.Commit()
	assert.Equal(t, "Acme, LL Inc", s.answers.Identity.Holder)
	assert.False(t, s.Editing())

	s.focus = FieldYear
	s.Activate()
	s.textBuffer = "2020-2025"
	s.Commit()
	assert.Equal(t, "2020-2025", s.answers.Identity.Year)
}

func TestGlobEditorAddRemove(t *testing.T) {
	s := validForm()
	s.focus = FieldInclude
	s.Activate()
	for _, r := range "src/**" {
		s.Input(r)
	}
	s.Commit() // adds, stays open
	assert.True(t, s.Editing())
	assert.Equal(t, []string{"src/**"}, s.answers.Coverage.Include)

	s.globBuffer = "  " // blank add is a no-op
	s.Commit()
	assert.Equal(t, []string{"src/**"}, s.answers.Coverage.Include)

	for _, r := range "docs/**" {
		s.Input(r)
	}
	s.Commit()
	assert.Equal(t, []string{"src/**", "docs/**"}, s.answers.Coverage.Include)

	s.MoveCursor(1) // select second
	s.Remove()
	assert.Equal(t, []string{"src/**"}, s.answers.Coverage.Include)

	// Exclude path mirrors include.
	s.focus = FieldExclude
	s.Activate()
	for _, r := range "**/vendor/**" {
		s.Input(r)
	}
	s.Commit()
	assert.Equal(t, []string{"**/vendor/**"}, s.answers.Coverage.Exclude)
	s.Remove()
	assert.Empty(t, s.answers.Coverage.Exclude)
}

func TestTransitionsNoopWhenNotEditing(t *testing.T) {
	s := validForm()
	// None of these should panic or mutate when no editor is open.
	s.Input('x')
	s.Backspace()
	s.MoveCursor(1)
	s.Commit()
	s.Remove()
	assert.False(t, s.Editing())
	assert.Equal(t, "MIT", s.answers.License.SPDXID)
}

func TestValidate(t *testing.T) {
	t.Run("valid form has no errors", func(t *testing.T) {
		assert.Empty(t, validForm().Validate())
		assert.True(t, validForm().CanWrite())
		assert.Empty(t, validForm().BlockingReason())
	})

	t.Run("each rule reports", func(t *testing.T) {
		s := NewForm(Answers{
			License:  LicenseAnswer{SPDXID: "NOPE"},
			Identity: IdentityAnswer{Year: "bad"},
			Coverage: CoverageAnswer{Include: []string{" "}, Exclude: []string{"\t"}},
		}, Detected{})
		errs := s.Validate()
		fields := map[Field]string{}
		for _, e := range errs {
			fields[e.Field] = e.Msg
		}
		assert.Contains(t, fields, FieldLicense)
		assert.Contains(t, fields, FieldHolder)
		assert.Contains(t, fields, FieldYear)
		assert.Contains(t, fields, FieldInclude)
		assert.Contains(t, fields, FieldExclude)
		assert.False(t, s.CanWrite())
		assert.NotEmpty(t, s.BlockingReason())
	})
}

func TestWriteConfig(t *testing.T) {
	cfg, err := validForm().WriteConfig()
	require.NoError(t, err)
	assert.Equal(t, "MIT", cfg.License)

	_, err = NewForm(Answers{License: LicenseAnswer{SPDXID: "MIT"}}, Detected{}).WriteConfig()
	assert.Error(t, err) // empty holder
}

func TestSmallHelpers(t *testing.T) {
	assert.Equal(t, 0, optionIndex([]string{"a", "b"}, "missing"))
	assert.Equal(t, 1, optionIndex([]string{"a", "b"}, "b"))
	assert.Equal(t, 5, clamp(10, 0, 5))
	assert.Equal(t, 0, clamp(-2, 0, 5))
	assert.Equal(t, 3, clamp(3, 0, 5))
	assert.Equal(t, "", dropLastRune(""))
	assert.Equal(t, "ab", dropLastRune("abc"))
	assert.False(t, validGlob("  "))
	assert.True(t, validGlob("src/**"))
	assert.Equal(t, []string{"a"}, removeAt([]string{"a"}, 5)) // out of range
	assert.Equal(t, []string{"a"}, removeAt([]string{"a"}, -1))
}

func TestGlobCursorClampOnEmpty(t *testing.T) {
	s := validForm()
	s.focus = FieldInclude
	s.Activate()
	s.globCursor = 4
	s.clampGlobCursor() // no entries -> resets to 0
	assert.Equal(t, 0, s.globCursor)
}

func TestAnswersAccessor(t *testing.T) {
	s := validForm()
	assert.Equal(t, "MIT", s.Answers().License.SPDXID)
}

func TestGlobEditorBackspaceAndEmptyCursor(t *testing.T) {
	s := validForm()
	s.focus = FieldExclude
	s.Activate()
	for _, r := range "vend" {
		s.Input(r)
	}
	s.Backspace()   // glob-buffer backspace path
	s.MoveCursor(1) // no entries yet -> n==0 path
	s.Commit()
	assert.Equal(t, []string{"ven"}, s.answers.Coverage.Exclude)
}

func TestSelectOptionsDedupAndAppend(t *testing.T) {
	// renderable carries an id absent from common, exercising the append branch the
	// vendored snapshot cannot (its common set covers every renderable id).
	got := selectOptions([]string{"MIT", "Apache-2.0"}, []string{"Apache-2.0", "MIT", "ZZ-Extra"})
	assert.Equal(t, []string{"MIT", "Apache-2.0", "ZZ-Extra"}, got)
}
