# TODO: GH-29 Init TUI Wizard

## Definition
- [ ] Define full feature specification and MVP boundaries.
- [ ] Update issue #29 with implementation plan once finalized.

## Implementation
- [ ] Persist `include` patterns in `.license-tool.yaml`.
- [ ] Add init wizard sample-language catalog and C fallback.
- [ ] Add live preview rendering for example source files.
- [ ] Replace interactive `init` form with full-screen TUI.
- [ ] Preserve deterministic non-TTY `init` behavior.
- [ ] Update README/DEVELOPERS documentation.

## Verification
- [ ] `gofmt -l .`
- [ ] `go vet ./...`
- [ ] `golangci-lint run`
- [ ] `go test ./... -race -coverpkg=./internal/...,./cmd/... -covermode=atomic -coverprofile=cover.out`
- [ ] `go run github.com/vladopajic/go-test-coverage/v2@v2.18.8 --config=.testcoverage.yml`
- [ ] `go build ./...`
