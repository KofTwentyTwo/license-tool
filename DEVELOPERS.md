# Developers

Use feature branches for normal work, merge through `develop`, and promote only green, release-ready commits to `main`. The required local quality gate is the same shape as CI: `gofmt -l .`, `go vet ./...`, `golangci-lint run`, race-enabled tests with `cover.out`, the configured 100% coverage checker, and `go build ./...`.

The interactive `init` wizard keeps terminal rendering in `cmd/license-tool/wizard.go`. Covered behavior belongs in `answersToConfig`, `internal/config`, and `internal/initwizard`; the TUI shell should stay thin and delegate preview rendering to the pure initwizard helpers.

Releases are documented in [RELEASING.md](RELEASING.md). In short: update [CHANGELOG.md](CHANGELOG.md), run `./scripts/verify-release.sh`, commit the release notes with a conventional commit, push `main`, wait for CI and CodeQL, then push a signed semver tag such as `v0.3.0`. The tag triggers GoReleaser, which creates the GitHub Release and publishes the Homebrew cask.
