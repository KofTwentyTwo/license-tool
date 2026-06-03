# Releasing

Releases are built with [GoReleaser](https://goreleaser.com) and published by GitHub Actions.

## Changelog Management

All releases must update the [CHANGELOG.md](./CHANGELOG.md) file with:

1. Version number and release date
2. Category of changes (Added, Changed, Fixed)
3. Breaking changes if any

Follow the [Keep a Changelog](https://keepachangelog.com/en/1.0.0/) format.

## Branch Flow

Use the repository's gitflow-style branches:

- `feature/*` branches hold active work and open PRs into `develop`.
- `develop` receives merged feature work and runs build/test CI plus branch artifact publishing.
- `release/*` or `rc/*` branches stabilize release candidates and run the same CI plus branch artifact publishing.
- `main` is production and should only receive release-ready changes.

Before pushing release tags, verify GitHub authentication:

```bash
gh auth status
git ls-remote --heads origin
```

## Local Release Prep

1. Update [CHANGELOG.md](./CHANGELOG.md) with release notes and the release date.
2. Run the pre-release gate:

   ```bash
   ./scripts/verify-release.sh
   ```

   It runs, in order: format/vet/lint/test, `goreleaser check`, a
   `goreleaser release --snapshot --clean` dry run, tap repo and token existence,
   the CHANGELOG date check, the SECURITY/README content checks, and a clean-tree check.

   You can run the individual GoReleaser gates directly:

   ```bash
   gofmt -l .
   go vet ./...
   golangci-lint run
   go test ./... -race -cover
   goreleaser check
   goreleaser release --snapshot --clean
   ```

3. Commit the changelog and any version-related changes.

The build version is injected at release time via `-ldflags` (`main.version`,
`main.commit`, `main.date`); there is no version constant to bump in source.

## Release Candidate

From a release candidate branch:

```bash
git tag vX.Y.Z-rc.N
git push origin vX.Y.Z-rc.N
```

The `Release` workflow builds all configured target binaries and creates a GitHub prerelease
(the `release.prerelease: auto` setting marks any tag with a prerelease suffix as a prerelease).

## Production Release

From `main`:

1. Update [CHANGELOG.md](./CHANGELOG.md) with the release date.
2. Commit changelog changes:

   ```bash
   git commit -am "Update CHANGELOG for v$VERSION"
   ```

3. Tag and push:

   ```bash
   git tag vX.Y.Z
   git push origin vX.Y.Z
   ```

The `Release` workflow runs `goreleaser release --clean`, which cross-compiles the
mac/linux arm64+amd64 and windows amd64 binaries, builds the archives and `checksums.txt`,
creates the GitHub Release, and publishes the Homebrew formula to:

```text
KofTwentyTwo/homebrew-tap
```

End users install with:

```bash
brew install KofTwentyTwo/tap/license-tool
```

## Required GitHub Setup

Create or verify a public Homebrew tap repository:

```text
KofTwentyTwo/homebrew-tap
```

Add a repository Actions secret on `KofTwentyTwo/license-tool` named:

```text
HOMEBREW_TAP_TOKEN
```

The token needs contents write access to `KofTwentyTwo/homebrew-tap`, because the release
workflow checks out that repository and commits formula updates. Do not hardcode this token
in workflow files.

Before the first production release:

- Protect or manage the `main` and `develop` branches according to the branch flow above.
- Confirm `HOMEBREW_TAP_TOKEN` can write to `KofTwentyTwo/homebrew-tap`.
- Confirm the local push path works with `git ls-remote --heads origin`.
- Re-run `goreleaser check` and `goreleaser release --snapshot --clean`.
