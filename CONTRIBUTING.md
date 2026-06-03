# Contributing

## Development Setup

Install the Go toolchain (`go 1.26`) and `golangci-lint`.

Useful local checks:

```bash
gofmt -l .
go vet ./...
golangci-lint run
go test ./... -race -cover
go build ./...
```

All Go files must be `gofmt`-clean (tabs, not spaces). CI fails if `gofmt -l .` reports any file.

## Branches

Use `feature/*` branches for pull requests into `develop`.

Release candidates stabilize on `release/*` or `rc/*` branches. Production releases are tagged from `main`.

## Pull Requests

Pull requests should include:

- A short description of the change.
- Tests for header generation, header detection, comment-syntax, or policy behavior when those paths change.
- Golden-file updates when rendering output changes, with the diff explained in the PR.

`apply` and `license` must stay dry-run by default. A change that writes to disk without an explicit `--write` is a bug, not a feature.

The tool never invents license text. New or updated license text must come from the vendored SPDX snapshot, not hand-edited copy.
