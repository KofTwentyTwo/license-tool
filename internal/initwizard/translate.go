package initwizard

import (
	"fmt"
	"strings"

	"github.com/KofTwentyTwo/license-tool/internal/config"
	"github.com/KofTwentyTwo/license-tool/internal/model"
	"github.com/KofTwentyTwo/license-tool/internal/spdx"
)

// placeholderHolder stands in for an empty holder while previewing, so the live
// source/YAML preview is never blank before the user has typed a holder.
const placeholderHolder = "Example, Inc."

// TranslateOptions tunes Translate. AllowPlaceholders selects preview mode: an empty
// holder is filled with a placeholder and parse failures fall back to defaults
// instead of erroring, so the live preview always renders.
type TranslateOptions struct {
	AllowPlaceholders bool
}

// Translate is the single conversion from wizard Answers to a model.Config, used by
// BOTH the live preview and the write path. WHY one function: a second translator
// (the old wizard previewConfig vs commands answersToConfig) could render a preview
// that disagreed with what got written. Strict mode (AllowPlaceholders false) is the
// write gate: it rejects an unrenderable license, an empty holder, and unparseable
// year/style. Preview mode tolerates all of those so the panel keeps rendering.
func Translate(a Answers, opts TranslateOptions) (model.Config, error) {
	licenseID := strings.TrimSpace(a.License.SPDXID)
	holder := strings.TrimSpace(a.Identity.Holder)
	year := strings.TrimSpace(a.Identity.Year)
	style := strings.TrimSpace(a.HeaderStyle.Style)

	if !opts.AllowPlaceholders {
		if !spdx.Validate(licenseID) {
			return model.Config{}, fmt.Errorf("init: %q is not a recognized SPDX license identifier", licenseID)
		}
		if _, ok := spdx.Lookup(licenseID); !ok {
			return model.Config{}, fmt.Errorf("init: %q is a recognized SPDX license identifier, but license-tool cannot render it", licenseID)
		}
		if holder == "" {
			return model.Config{}, fmt.Errorf("init: copyright holder is required")
		}
	} else if holder == "" {
		holder = placeholderHolder
	}

	cfg := config.Defaults()
	cfg.License = licenseID
	cfg.Holder = holder

	if year != "" {
		ys, err := config.ParseYearSpec(year)
		switch {
		case err == nil:
			cfg.Year = ys
		case !opts.AllowPlaceholders:
			return model.Config{}, err
		}
	}
	if style != "" {
		st, err := config.ParseStyle(style)
		switch {
		case err == nil:
			cfg.Style = st
		case !opts.AllowPlaceholders:
			return model.Config{}, err
		}
	}

	cfg.ManageLicenseFile = a.LicenseFiles.Manage
	cfg.Includes = append([]string(nil), a.Coverage.Include...)
	cfg.Excludes = append([]string(nil), a.Coverage.Exclude...)
	return cfg, nil
}
