// Package initwizard defines the structured answer model shared by the init
// command and the interactive collector.
package initwizard

import "errors"

// ErrAborted is returned when the operator exits the interactive wizard without
// confirming the review step.
var ErrAborted = errors.New("init wizard aborted")

// ProjectModel describes the licensing posture the wizard is collecting for.
type ProjectModel string

const (
	// ProjectModelOpenSource is for public source distributed under an SPDX license.
	ProjectModelOpenSource ProjectModel = "open-source"
	// ProjectModelPrivateInternal is for private source with internal policy needs.
	ProjectModelPrivateInternal ProjectModel = "private-internal"
	// ProjectModelExistingProject is for an existing repo where current files guide defaults.
	ProjectModelExistingProject ProjectModel = "existing-project"
	// ProjectModelAdvancedManual is for explicit flag or manual wizard choices.
	ProjectModelAdvancedManual ProjectModel = "advanced-manual"
)

// Step is the high-level screen/state currently active in the collector.
type Step int

const (
	// StepProjectModel collects the project licensing model.
	StepProjectModel Step = iota
	// StepLicense collects the SPDX or future private license choice.
	StepLicense
	// StepIdentity collects holder and year policy values.
	StepIdentity
	// StepHeaderStyle collects the source header style.
	StepHeaderStyle
	// StepLicenseFiles collects top-level license file management intent.
	StepLicenseFiles
	// StepCoverage collects include and exclude coverage choices.
	StepCoverage
	// StepReview presents the final review before writing config.
	StepReview
)

// Answers is the structured payload returned by the interactive collector.
type Answers struct {
	// Project records the project model choice.
	Project ProjectAnswer
	// License records the target license choice.
	License LicenseAnswer
	// Identity records copyright holder and year policy.
	Identity IdentityAnswer
	// HeaderStyle records the source header format.
	HeaderStyle HeaderStyleAnswer
	// LicenseFiles records whether top-level license files should be managed.
	LicenseFiles LicenseFilesAnswer
	// Coverage records include and exclude file scope answers.
	Coverage CoverageAnswer
	// Review records the final confirmation state.
	Review ReviewAnswer
}

// ProjectAnswer holds the project model selection.
type ProjectAnswer struct {
	// Model is the selected project model.
	Model ProjectModel
}

// LicenseAnswer holds the target license selection.
type LicenseAnswer struct {
	// SPDXID is the SPDX identifier selected for generated config.
	SPDXID string
	// Private marks a future private/internal license path without SPDX rendering.
	Private bool
}

// IdentityAnswer holds copyright identity answers.
type IdentityAnswer struct {
	// Holder is the copyright holder text.
	Holder string
	// Year is the raw year policy token.
	Year string
}

// HeaderStyleAnswer holds the raw header style token.
type HeaderStyleAnswer struct {
	// Style is reuse, notice, or reuse+notice.
	Style string
}

// LicenseFilesAnswer records license file management intent.
type LicenseFilesAnswer struct {
	// Manage enables top-level LICENSE and LICENSES/<id>.txt management.
	Manage bool
}

// CoverageAnswer records source file scope selections.
type CoverageAnswer struct {
	// Include is the selected include glob list.
	Include []string
	// Exclude is the selected exclude glob list.
	Exclude []string
}

// ReviewAnswer records whether the final review was confirmed.
type ReviewAnswer struct {
	// Confirmed is true when the operator accepted the review screen.
	Confirmed bool
}

// Preview is the narrow shell expected from the future preview engine.
type Preview struct {
	// Source is the rendered example source preview.
	Source string
	// Config is the rendered config preview.
	Config string
	// LicenseFiles is the license file behavior summary.
	LicenseFiles string
	// Coverage is the file coverage summary.
	Coverage string
}

// PreviewProvider is intentionally narrow until the preview engine lands.
type PreviewProvider interface {
	// Preview returns display text for the current answers.
	Preview(Answers) (Preview, error)
}
