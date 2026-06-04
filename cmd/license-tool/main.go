// Command license-tool is the CLI entry point. It builds the cobra command tree
// and executes it. The version/commit/date vars are populated at release time via
// -ldflags by GoReleaser.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Build metadata, injected via -ldflags by GoReleaser (see .goreleaser.yaml).
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

const (
	exitOK           = 0
	exitCheckFailure = 1
	exitUsage        = 2
	exitWriteRefused = 3
	exitInternal     = 4
)

func main() {
	root := newRootCmd(buildInfo{version: version, commit: commit, date: date})
	os.Exit(execute(root))
}

func execute(root *cobra.Command) int {
	if err := root.Execute(); err != nil {
		fmt.Fprintln(root.ErrOrStderr(), err)
		return exitCode(err)
	}
	return exitOK
}

type commandError struct {
	code int
	err  error
}

func (e commandError) Error() string {
	return e.err.Error()
}

func (e commandError) Unwrap() error {
	return e.err
}

func withExitCode(code int, err error) error {
	if err == nil {
		return nil
	}
	return commandError{code: code, err: err}
}

func exitCode(err error) int {
	var ce commandError
	if errors.As(err, &ce) {
		return ce.code
	}
	return exitUsage
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
