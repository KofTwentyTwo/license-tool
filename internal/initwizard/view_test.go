package initwizard

import (
	"strings"
	"testing"

	"github.com/KofTwentyTwo/license-tool/internal/model"
	"github.com/stretchr/testify/assert"
)

func cSample() Sample { return SelectSample(nil) }

func TestRenderTooSmall(t *testing.T) {
	out := Render(validForm(), PanelPlan{TooSmall: true}, cSample(), 2026)
	assert.Contains(t, out, "Terminal too small")
	assert.Contains(t, out, "--license")
}

func TestRenderWideShowsAllPanels(t *testing.T) {
	s := NewForm(Answers{
		License:  LicenseAnswer{SPDXID: "MIT"},
		Identity: IdentityAnswer{Holder: "Acme, LLC", Year: "git"},
		Coverage: CoverageAnswer{Include: []string{"src/**"}, Exclude: []string{"**/vendor/**"}},
	}, Detected{})
	out := Render(s, Layout(140, 40), cSample(), 2026)

	assert.Contains(t, out, "license-tool init")
	assert.Contains(t, out, "License")
	assert.Contains(t, out, "MIT")
	assert.Contains(t, out, "Acme, LLC")
	assert.Contains(t, out, "src/**")       // include row (listOrAll non-empty)
	assert.Contains(t, out, "**/vendor/**") // exclude row (listOrNone non-empty)
	assert.Contains(t, out, "Source preview")
	assert.Contains(t, out, "stdio")          // C sample body
	assert.Contains(t, out, "license: MIT")   // YAML preview
	assert.Contains(t, out, "ready to write") // footer, valid form
}

func TestRenderListPlaceholders(t *testing.T) {
	out := Render(validForm(), Layout(140, 40), cSample(), 2026)
	assert.Contains(t, out, "all supported files") // empty include
	assert.Contains(t, out, "Manage license files: no")
}

func TestRenderInvalidFormDisablesWrite(t *testing.T) {
	s := NewForm(Answers{License: LicenseAnswer{SPDXID: "MIT"}}, Detected{}) // empty holder
	out := Render(s, Layout(140, 40), cSample(), 2026)
	assert.Contains(t, out, "(required)")    // holder placeholder
	assert.Contains(t, out, "(blocked)")     // write row disabled
	assert.Contains(t, out, "cannot write:") // footer reason
}

func TestRenderDetectedBadges(t *testing.T) {
	s := NewForm(Answers{
		License:  LicenseAnswer{SPDXID: "MIT"},
		Identity: IdentityAnswer{Holder: "Acme"},
	}, Detected{License: true, Holder: true, Manage: true})
	out := Render(s, Layout(140, 40), cSample(), 2026)
	assert.GreaterOrEqual(t, strings.Count(out, "(detected)"), 3)
}

func TestRenderEditors(t *testing.T) {
	open := func(f Field, mutate func(*FormState)) string {
		s := validForm()
		s.focus = f
		s.Activate()
		if mutate != nil {
			mutate(&s)
		}
		return Render(s, Layout(140, 40), cSample(), 2026)
	}

	assert.Contains(t, open(FieldLicense, nil), "filter:")
	assert.Contains(t, open(FieldStyle, nil), "reuse+notice")
	assert.Contains(t, open(FieldHolder, nil), "Acme, LLC_")
	assert.Contains(t, open(FieldInclude, nil), "(no entries yet)")
	assert.Contains(t, open(FieldInclude, func(s *FormState) {
		s.globBuffer = "src/**"
		s.Commit()
	}), "add:")

	noMatch := open(FieldLicense, func(s *FormState) {
		for _, r := range "zzzz" {
			s.Input(r)
		}
	})
	assert.Contains(t, noMatch, "no matching licenses")
}

func TestRenderSourceUnavailable(t *testing.T) {
	s := NewForm(Answers{
		License:  LicenseAnswer{SPDXID: "Glide"}, // valid SPDX id, not renderable
		Identity: IdentityAnswer{Holder: "Acme"},
	}, Detected{})
	out := Render(s, Layout(140, 40), cSample(), 2026)
	assert.Contains(t, out, "source preview unavailable")
}

func TestRenderStacked(t *testing.T) {
	out := Render(validForm(), Layout(70, 24), cSample(), 2026)
	assert.Contains(t, out, "license-tool init")
	assert.Contains(t, out, "Source preview")
}

func TestPreviewYear(t *testing.T) {
	assert.Equal(t, "2026", previewYear(model.YearSpec{Kind: model.YearCurrent}, 2026))
	assert.Equal(t, "2030", previewYear(model.YearSpec{Kind: model.YearExplicit, Start: 2030}, 2026))
	assert.Equal(t, "2021-2026", previewYear(model.YearSpec{Kind: model.YearExplicit}, 2026)) // Start==0 fallback
	assert.Equal(t, "2020-2025", previewYear(model.YearSpec{Kind: model.YearRange, Start: 2020, End: 2025}, 2026))
	assert.Equal(t, "2021-2026", previewYear(model.YearSpec{Kind: model.YearRange}, 2026)) // incomplete fallback
	assert.Equal(t, "2021-2026", previewYear(model.YearSpec{Kind: model.YearGit}, 2026))   // git/default
}

func TestSmallViewHelpers(t *testing.T) {
	assert.Equal(t, "> ", cursorMark(true))
	assert.Equal(t, "  ", cursorMark(false))
	assert.Equal(t, "fallback", orPlaceholder("  ", "fallback"))
	assert.Equal(t, "value", orPlaceholder("value", "fallback"))
	assert.Equal(t, "yes", yesNo(true))
	assert.Equal(t, "no", yesNo(false))
	assert.Equal(t, "all supported files", listOrAll(nil))
	assert.Equal(t, "a, b", listOrAll([]string{"a", "b"}))
	assert.Equal(t, "none", listOrNone(nil))
	assert.Equal(t, "x", listOrNone([]string{"x"}))
	assert.Equal(t, "    a\n    b", indentBlock("a\nb"))
	assert.Equal(t, 5, maxInt(5, 1))
	assert.Equal(t, 5, maxInt(1, 5))
}

func TestWindow(t *testing.T) {
	s, e := window(0, 3, 6)
	assert.Equal(t, [2]int{0, 3}, [2]int{s, e}) // total <= rows

	s, e = window(0, 20, 6)
	assert.Equal(t, [2]int{0, 6}, [2]int{s, e}) // start clamped to 0

	s, e = window(19, 20, 6)
	assert.Equal(t, [2]int{14, 20}, [2]int{s, e}) // start+rows > total
}

func TestFitBlockAndTruncate(t *testing.T) {
	assert.Equal(t, "", fitBlock("x", 0, 5))
	assert.Equal(t, "", fitBlock("x", 5, 0))

	got := fitBlock("a\nb\nc\nd", 10, 2)
	assert.Equal(t, "a\n...", got) // overflow replaces last visible line

	assert.Equal(t, "ab", truncateLine("ab", 10))    // fits
	assert.Equal(t, "ab", truncateLine("abcdef", 2)) // width <= 3
	assert.Equal(t, "a...", truncateLine("abcdef", 4))
}
