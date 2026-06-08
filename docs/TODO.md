# TODO: GH-29 Init TUI Wizard

## Definition
- [x] Define full feature specification and MVP boundaries.
- [x] Update issue #29 with implementation plan once finalized.

## Implementation
- [x] Persist `include` patterns in `.license-tool.yaml`.
- [x] Add init wizard sample-language catalog and C fallback.
- [x] Add live preview rendering for example source files.
- [x] Replace interactive `init` form with full-screen TUI.
- [x] Preserve deterministic non-TTY `init` behavior.
- [x] Update README/DEVELOPERS documentation.

## Verification
- [x] `gofmt -l .`
- [x] `go vet ./...`
- [x] `golangci-lint run`
- [x] `go test ./... -race -coverpkg=./internal/...,./cmd/... -covermode=atomic -coverprofile=cover.out`
- [x] `go run github.com/vladopajic/go-test-coverage/v2@v2.18.8 --config=.testcoverage.yml`
- [x] `go build ./...`
