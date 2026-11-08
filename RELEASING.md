# Releasing

> For the complete end-to-end runbook (integrating multiple feature PRs, versioning,
> conflict resolution, tagging, and post-release verification) see [RELEASES.md](./RELEASES.md).
> This file is the short reference.

CI and releases run on Gitea Actions (`.gitea/workflows/build.yaml`, the shared
`KofTwentyTwo/ci-workflows` Go workflow). A `v*` tag runs every gate, then
`scripts/build-release.sh` builds the archives and CI attaches them with a `SHA256SUMS`
to the tag's Gitea release. `.goreleaser.yaml` remains for local snapshot checks.

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

   It runs, in order: formatting, vet, `golangci-lint`, race tests with the
   same coverage profile used by CI, the 100% coverage checker, `goreleaser check`,
   a `goreleaser release --snapshot --clean` dry run, tap repo and token existence,
   the CHANGELOG date check, the SECURITY/README content checks, and a clean-tree check.

   You can run the individual GoReleaser gates directly:

   ```bash
   gofmt -l .
   go vet ./...
   golangci-lint run
   go test ./... -race -coverpkg=./internal/...,./cmd/... -covermode=atomic -coverprofile=cover.out
   go run github.com/vladopajic/go-test-coverage/v2@v2.18.8 --config=.testcoverage.yml
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

The tag build creates a Gitea prerelease (any tag with a `-` suffix) with the archives and
`SHA256SUMS`.

## Production Release

From `main`:

1. Update [CHANGELOG.md](./CHANGELOG.md) with the release date.
2. Commit changelog changes:

   ```bash
   git commit -am "docs: changelog for v$VERSION"
   ```

3. Push `main` and wait for CI to pass on the release commit.
4. Tag and push. Prefer a signed annotated tag:

   ```bash
   git tag -s vX.Y.Z -m "vX.Y.Z"
   git push origin vX.Y.Z
   ```

The tag build runs every CI gate, then `scripts/build-release.sh <version>` cross-compiles the
mac/linux arm64+amd64 and windows amd64 binaries into `dist/` archives, and CI attaches them
and a `SHA256SUMS` to the tag's Gitea release.

The Homebrew cask in `KofTwentyTwo/homebrew-tap` is not updated by CI; update it by hand from
the release's `SHA256SUMS` until the tap has its own workflow.

