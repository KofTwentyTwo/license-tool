package main

import (
	"errors"

	"github.com/charmbracelet/huh"

	"github.com/KofTwentyTwo/license-tool/internal/config"
	"github.com/KofTwentyTwo/license-tool/internal/spdx"
)

// errEmptyHolder is the in-form validation error shown when the holder is blank. A
// non-empty holder is required to render a meaningful copyright line, so the wizard
// refuses to advance until one is entered.
var errEmptyHolder = errors.New("copyright holder is required")

// initAnswers holds the raw string answers collected by the interactive init
// wizard, before they are validated and parsed into a model.Config. WHY raw strings
// (not a model.Config): the form binds directly to these fields, and answersToConfig
// owns the single validation/parse pass, so the wizard stays a thin I/O shell with
// no business logic (this file is excluded from coverage for exactly that reason).
type initAnswers struct {
	License           string
	Holder            string
	Year              string
	Style             string
	ManageLicenseFile bool
	Excludes          []string
}

// licenseSelectOptions builds the filterable license picker options: the curated
// CommonIDs first (each labeled "<id> (common)" so a user knows they are the
// recommended set), then the remaining non-deprecated SPDX ids in sorted order with
// no duplicates. WHY common-first: the operator sees sensible defaults at the top
// before the long alphabetical tail they can still filter into.
func licenseSelectOptions() []huh.Option[string] {
	common := spdx.CommonIDs()
	seen := make(map[string]bool, len(common))
	opts := make([]huh.Option[string], 0, len(common))
	for _, id := range common {
		seen[id] = true
		opts = append(opts, huh.NewOption(id+" (common)", id))
	}
	for _, id := range spdx.IDs() {
		if seen[id] {
			continue
		}
		opts = append(opts, huh.NewOption(id, id))
	}
	return opts
}

// runInitWizard drives the interactive init form, binding each answer into a. WHY
// the Year/License validators reuse the config parsers: the wizard must reject the
// same inputs answersToConfig would later reject, so the user fixes a bad value
// in-place rather than getting a post-submit error.
func runInitWizard(a *initAnswers) error {
	// Pre-populate the year field with the safe "git" default so an operator who just
	// presses enter accepts the same default the config layer would have applied.
	if a.Year == "" {
		a.Year = "git"
	}
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Target SPDX license").
				Options(licenseSelectOptions()...).
				Filtering(true).
				Value(&a.License),
			huh.NewInput().
				Title("Copyright holder").
				Value(&a.Holder).
				Validate(func(s string) error {
					if s == "" {
						return errEmptyHolder
					}
					return nil
				}),
			huh.NewInput().
				Title("Year policy (current|YYYY|YYYY-YYYY|git)").
				Value(&a.Year).
				Validate(func(s string) error {
					if s == "" {
						return nil
					}
					_, err := config.ParseYearSpec(s)
					return err
				}),
			huh.NewSelect[string]().
				Title("Header style").
				Options(
					huh.NewOption("reuse", "reuse"),
					huh.NewOption("notice", "notice"),
					huh.NewOption("reuse+notice", "reuse+notice"),
				).
				Value(&a.Style),
			huh.NewConfirm().
				Title("Manage top-level LICENSE file?").
				Value(&a.ManageLicenseFile),
		),
	)
	return form.Run()
}

// collectInteractive runs the wizard to populate a when interactive is true,
// otherwise it is a no-op (the non-TTY path relies entirely on flags). WHY a seam:
// commands.go swaps this out in tests so the init command's non-interactive flow can
// be exercised without a terminal.
func collectInteractive(a *initAnswers, interactive bool) error {
	if !interactive {
		return nil
	}
	return runInitWizard(a)
}
