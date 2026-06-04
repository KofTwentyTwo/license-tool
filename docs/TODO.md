# TODO: Audit Fixes

## Tracking
- [x] Worker agents assigned for all implementation slices.
- [x] GitHub issue comments reviewed for #6 through #27.
- [ ] Keep `docs/SESSION-STATE.md` current after each integrated slice.
- [ ] Keep coverage at the repository gate: total coverage 100% for the gated set.

## Header Boundary Preservation
- [ ] #6: Preserve Go `//go:build` and `// +build` constraints before inserted headers.
- [ ] #7: Preserve CSS `@charset` before inserted headers.
- [ ] #8: Preserve HTML/XML `<!DOCTYPE>` before inserted headers.

## File-Type Coverage
- [ ] #9: Add Perl, PowerShell, R, Makefile, TOML, and Batch file-type support.
- [ ] #10: Detect extensionless script type from shebang content.

## README Docs
- [ ] #11: Document the init wizard and audit findings summary accurately.

## Write Safety
- [ ] #12: Make `license` honor write gates and either implement or reject commit options.
- [ ] #13: Prevent dirty-tree apply commits from staging unrelated work.
- [ ] #14: Make apply honor include and gitignore scope controls.
- [ ] #15: Emit unified diffs during dry-run apply and license runs.

## Header Placement
- [ ] #24: Preserve Python `coding=` pragmas before inserted headers.
- [ ] #27: Deduplicate shared sentinel and header placement behavior.

## CLI And Reports
- [ ] #16: Print errors and map documented exit codes.
- [ ] #17: Make check fail-condition CLI overrides affect behavior.
- [ ] #18: Honor audit/check output controls and documented dependency skip behavior.
- [ ] #25: Reject invalid dependency resolver tier values.

## Policy And SPDX
- [ ] #19: Align accepted SPDX IDs with renderable license support.
- [ ] #20: Validate policy required, allow, and deny IDs or expressions.

## Dependencies And Release
- [ ] #21: Pin release workflow actions and release tooling.
- [ ] #22: Make Gradle tool-tier behavior explicit or implemented.
- [ ] #23: Discover nested dependency manifests.

## Public Docs
- [ ] #26: Refresh public support, security-policy, and scaffolding docs.

## Final Gates
- [ ] `gofmt -l .`
- [ ] `go vet ./...`
- [ ] `golangci-lint run`
- [ ] `go test ./... -race -cover`
- [ ] `go test ./... -race -coverpkg=./internal/...,./cmd/... -covermode=atomic -coverprofile=cover.out`
- [ ] `go run github.com/vladopajic/go-test-coverage/v2@v2.18.8 --config=.testcoverage.yml`
- [ ] `go build ./...`
- [ ] `gitleaks detect --source . --no-banner --redact --exit-code 1`
- [ ] Merge passing feature branch to local `develop`.
