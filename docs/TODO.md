# TODO: Audit Fixes

## Tracking
- [x] Worker agents assigned for all implementation slices.
- [x] GitHub issue comments reviewed for #6 through #27.
- [x] Keep `docs/SESSION-STATE.md` current after each integrated slice.
- [x] Keep coverage at the repository gate: total coverage 100% for the gated set.

## Header Boundary Preservation
- [x] #6: Preserve Go `//go:build` and `// +build` constraints before inserted headers.
- [x] #7: Preserve CSS `@charset` before inserted headers.
- [x] #8: Preserve HTML/XML `<!DOCTYPE>` before inserted headers.

## File-Type Coverage
- [x] #9: Add Perl, PowerShell, R, Makefile, TOML, and Batch file-type support.
- [x] #10: Detect extensionless script type from shebang content.

## README Docs
- [x] #11: Document the init wizard and audit findings summary accurately.

## Write Safety
- [x] #12: Make `license` honor write gates and either implement or reject commit options.
- [x] #13: Prevent dirty-tree apply commits from staging unrelated work.
- [x] #14: Make apply honor include and gitignore scope controls.
- [x] #15: Emit unified diffs during dry-run apply and license runs.

## Header Placement
- [x] #24: Preserve Python `coding=` pragmas before inserted headers.
- [x] #27: Deduplicate shared sentinel and header placement behavior.

## CLI And Reports
- [x] #16: Print errors and map documented exit codes.
- [x] #17: Make check fail-condition CLI overrides affect behavior.
- [x] #18: Honor audit/check output controls and documented dependency skip behavior.
- [x] #25: Reject invalid dependency resolver tier values.

## Policy And SPDX
- [x] #19: Align accepted SPDX IDs with renderable license support.
- [x] #20: Validate policy required, allow, and deny IDs or expressions.

## Dependencies And Release
- [x] #21: Pin release workflow actions and release tooling.
- [x] #22: Make Gradle tool-tier behavior explicit or implemented.
- [x] #23: Discover nested dependency manifests.

## Public Docs
- [x] #26: Refresh public support, security-policy, and scaffolding docs.

## Final Gates
- [x] `gofmt -l .`
- [x] `go vet ./...`
- [x] `golangci-lint run`
- [x] `go test ./... -race -cover`
- [x] `go test ./... -race -coverpkg=./internal/...,./cmd/... -covermode=atomic -coverprofile=cover.out`
- [x] `go run github.com/vladopajic/go-test-coverage/v2@v2.18.8 --config=.testcoverage.yml`
- [x] `go build ./...`
- [x] `gitleaks detect --source . --no-banner --redact --exit-code 1`
- [ ] Merge passing feature branch to local `develop`.
