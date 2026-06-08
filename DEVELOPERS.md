# Developers

Use feature branches for normal work, merge through `develop`, and promote only green, release-ready commits to `main`. The required local quality gate is the same shape as CI: `gofmt -l .`, `go vet ./...`, `golangci-lint run`, race-enabled tests with `cover.out`, the configured 100% coverage checker, and `go build ./...`.

The interactive `init` wizard is a thin Bubble Tea adapter in `cmd/license-tool/wizard.go` (excluded from coverage). All behavior — the form state machine, live validation, adaptive layout, view rendering, repo detection (`Seed`), and the single `Translate` from answers to config — lives in `internal/initwizard` and is unit-tested to 100%. Keep the adapter logic-free: forward keystrokes to `FormState` transitions and render via `initwizard.Render`. There is exactly one `Answers`->`Config` translator (`initwizard.Translate`); do not reintroduce a second one for the preview.

Releases are documented in [RELEASING.md](RELEASING.md). In short: update [CHANGELOG.md](CHANGELOG.md), run `./scripts/verify-release.sh`, commit the release notes with a conventional commit, push `main`, wait for CI and CodeQL, then push a signed semver tag such as `v0.3.0`. The tag triggers GoReleaser, which creates the GitHub Release and publishes the Homebrew cask.
