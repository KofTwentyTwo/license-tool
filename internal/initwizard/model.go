// Package initwizard defines the structured answer model shared by the init
// command and the interactive collector.
package initwizard

import "errors"

// ErrAborted is returned when the operator exits the interactive wizard without
// confirming the review step.
var ErrAborted = errors.New("init wizard aborted")

// Answers is the structured payload the init form edits and returns. WHY no project
// model or review step: the redesign detects defaults from the repo (see Seed) rather
// than asking a project-model question, and the single-screen form is itself the
// review, so the former Project/Review/Step machinery was removed.
type Answers struct {
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
}

// LicenseAnswer holds the target license selection.
type LicenseAnswer struct {
	// SPDXID is the SPDX identifier selected for generated config.
	SPDXID string
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
