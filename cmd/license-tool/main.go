// Command license-tool is the CLI entry point. It builds the cobra command tree
// and executes it. The version/commit/date vars are populated at release time via
// -ldflags by GoReleaser.
package main

import (
	"fmt"
	"os"
)

// Build metadata, injected via -ldflags by GoReleaser (see .goreleaser.yaml).
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	root := newRootCmd(buildInfo{version: version, commit: commit, date: date})
	if err := root.Execute(); err != nil {
		// Cobra has already printed the error; exit non-zero. Richer per-mode exit
		// codes (1 check failure, 3 write refused, 4 internal) are produced inside
		// the subcommand RunE bodies, which call os.Exit directly when they need a
		// specific code.
		os.Exit(2)
	}
}

// buildInfo carries the ldflags-injected version metadata into the command tree so
// the version subcommand can print it without referencing package-level vars.
type buildInfo struct {
	version string
	commit  string
	date    string
}

// String renders the build info in the one-line form the version command prints.
func (b buildInfo) String() string {
	return fmt.Sprintf("license-tool %s (commit %s, built %s)", b.version, b.commit, b.date)
}
