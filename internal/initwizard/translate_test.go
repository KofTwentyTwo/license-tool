package initwizard

import (
	"testing"

	"github.com/KofTwentyTwo/license-tool/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTranslateStrict drives the write-path gate: a valid case plus each rejection.
// WHY exhaustive: this is the single gate both the wizard and the flag-only init path
// funnel through, so every validation/parse arm must reject identically.
func TestTranslateStrict(t *testing.T) {
	t.Run("valid answers build a config from defaults", func(t *testing.T) {
		cfg, err := Translate(Answers{
			License:      LicenseAnswer{SPDXID: "MIT"},
			Identity:     IdentityAnswer{Holder: "Acme, LLC", Year: "2021-2026"},
			HeaderStyle:  HeaderStyleAnswer{Style: "reuse"},
			LicenseFiles: LicenseFilesAnswer{Manage: false},
			Coverage:     CoverageAnswer{Include: []string{"src/**"}, Exclude: []string{"**/vendor/**"}},
		}, TranslateOptions{})
		require.NoError(t, err)
		assert.Equal(t, "MIT", cfg.License)
		assert.Equal(t, "Acme, LLC", cfg.Holder)
		assert.Equal(t, model.YearRange, cfg.Year.Kind)
		assert.Equal(t, 2021, cfg.Year.Start)
		assert.Equal(t, 2026, cfg.Year.End)
		assert.Equal(t, model.StyleReuse, cfg.Style)
		assert.False(t, cfg.ManageLicenseFile)
		assert.Equal(t, []string{"src/**"}, cfg.Includes)
		assert.Equal(t, []string{"**/vendor/**"}, cfg.Excludes)
	})

	t.Run("empty year and style keep the built-in defaults", func(t *testing.T) {
		cfg, err := Translate(Answers{
			License:      LicenseAnswer{SPDXID: "MIT"},
			Identity:     IdentityAnswer{Holder: "Acme"},
			LicenseFiles: LicenseFilesAnswer{Manage: true},
		}, TranslateOptions{})
		require.NoError(t, err)
		assert.Equal(t, model.YearGit, cfg.Year.Kind)
		assert.Equal(t, model.StyleReusePlusNotice, cfg.Style)
	})

	t.Run("unknown license rejected", func(t *testing.T) {
		_, err := Translate(Answers{
			License:  LicenseAnswer{SPDXID: "NOT-A-LICENSE"},
			Identity: IdentityAnswer{Holder: "Acme"},
		}, TranslateOptions{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not a recognized SPDX license identifier")
	})

	t.Run("valid but unrenderable license rejected", func(t *testing.T) {
		// 0BSD is a real SPDX id; if the tool does not ship rendering for it, strict
		// mode must reject it with the cannot-render message rather than the
		// not-recognized one. Pick an id that validates but is not curated.
		_, err := Translate(Answers{
			License:  LicenseAnswer{SPDXID: "Glide"},
			Identity: IdentityAnswer{Holder: "Acme"},
		}, TranslateOptions{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot render")
	})

	t.Run("empty holder rejected", func(t *testing.T) {
		_, err := Translate(Answers{License: LicenseAnswer{SPDXID: "MIT"}}, TranslateOptions{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "copyright holder is required")
	})

	t.Run("bad year rejected", func(t *testing.T) {
		_, err := Translate(Answers{
			License:  LicenseAnswer{SPDXID: "MIT"},
			Identity: IdentityAnswer{Holder: "Acme", Year: "not-a-year"},
		}, TranslateOptions{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "year")
	})

	t.Run("bad style rejected", func(t *testing.T) {
		_, err := Translate(Answers{
			License:     LicenseAnswer{SPDXID: "MIT"},
			Identity:    IdentityAnswer{Holder: "Acme"},
			HeaderStyle: HeaderStyleAnswer{Style: "fancy"},
		}, TranslateOptions{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown style")
	})
}

// TestTranslatePlaceholders verifies preview mode never errors and substitutes a
// placeholder holder, so the live preview keeps rendering mid-edit.
func TestTranslatePlaceholders(t *testing.T) {
	opts := TranslateOptions{AllowPlaceholders: true}

	t.Run("empty holder becomes placeholder", func(t *testing.T) {
		cfg, err := Translate(Answers{License: LicenseAnswer{SPDXID: "MIT"}}, opts)
		require.NoError(t, err)
		assert.Equal(t, placeholderHolder, cfg.Holder)
	})

	t.Run("unparseable year and style fall back to defaults without error", func(t *testing.T) {
		cfg, err := Translate(Answers{
			License:     LicenseAnswer{SPDXID: "MIT"},
			Identity:    IdentityAnswer{Holder: "Acme", Year: "not-a-year"},
			HeaderStyle: HeaderStyleAnswer{Style: "fancy"},
		}, opts)
		require.NoError(t, err)
		assert.Equal(t, model.YearGit, cfg.Year.Kind)
		assert.Equal(t, model.StyleReusePlusNotice, cfg.Style)
	})

	t.Run("valid placeholder values pass through", func(t *testing.T) {
		cfg, err := Translate(Answers{
			License:     LicenseAnswer{SPDXID: "MIT"},
			Identity:    IdentityAnswer{Holder: "Acme", Year: "current"},
			HeaderStyle: HeaderStyleAnswer{Style: "notice"},
		}, opts)
		require.NoError(t, err)
		assert.Equal(t, "Acme", cfg.Holder)
		assert.Equal(t, model.YearCurrent, cfg.Year.Kind)
		assert.Equal(t, model.StyleNotice, cfg.Style)
	})
}
