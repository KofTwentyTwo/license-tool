# Releases

This is the full, end-to-end runbook for cutting a `license-tool` release. It expands
on [RELEASING.md](./RELEASING.md) (which is the short reference) with the complete
process: integrating feature work, choosing a version, running the gate, tagging, and
verifying the published artifacts.

Releases are built by [GoReleaser](https://goreleaser.com) and published by the
`Release` GitHub Actions workflow. There is **no version constant in source** — the
version is injected at build time via `-ldflags` (`main.version`, `main.commit`,
`main.date`) when the workflow runs against a pushed tag.

---

## 1. Branch model

Gitflow-style:

- `feature/*` — active work; each opens a PR into `develop`.
- `develop` — the integration branch and the repository default branch. Merged feature
  work lands here. **Issues with `Closes #N` close when their PR merges into `develop`.**
- `main` — production. Only release-ready commits land here.
- Tags `vX.Y.Z` on `main` trigger the published release.

```
feature/GH-XX  ──PR──▶  develop  ──merge──▶  main  ──tag vX.Y.Z──▶  Release workflow
```

---

## 2. Versioning (SemVer, pre-1.0 aware)

Pick the next version from the nature of the `[Unreleased]` changelog entries:

| Change set | Bump | Example |
|------------|------|---------|
| New features (backward-compatible) | minor | 0.3.0 → 0.4.0 |
| Only bug fixes | patch | 0.4.0 → 0.4.1 |
| Breaking changes | minor while < 1.0 (major once ≥ 1.0) | 0.4.0 → 0.5.0 |

While `0.x`, a minor bump may legitimately include a breaking change (e.g. removing a
flag); call it out under `### Removed`/`### Changed` and in the PR. Record the decision
in the changelog so the bump is self-documenting.

---

## 3. Integrate feature PRs into `develop`

Most releases bundle several feature PRs. When multiple PRs are open they will conflict
(at minimum on `CHANGELOG.md`, often in shared files like `cmd/.../commands.go`), so
integrate them locally in one pass and validate the **combined** result — a set of
individually-green PRs can still break when merged together.

```bash
git fetch origin
git checkout develop && git reset --hard origin/develop

# Merge each feature branch with a merge commit (so GitHub marks the PR "Merged"
# and the linked issue closes on the merge into develop).
for b in feature/GH-AAA feature/GH-BBB ... ; do
  git merge --no-ff "$b" -m "Merge $b (#N): <summary>"
  # Resolve conflicts:
  #  - CHANGELOG.md / SESSION-STATE.md: take one side now (git checkout --ours <f>);
  #    you will rewrite the changelog cleanly in step 4.
  #  - code/test files: keep the UNION of independent additions; never blindly --ours
  #    a file that also received auto-merged hunks from the other side, or you will
  #    drop them. Watch for cross-branch refactors (a symbol renamed on one branch and
  #    referenced on another) — the build/vet step below catches these.
  git add -A && git commit --no-edit
done
```

Conflict-resolution tips learned in practice:
- A rename on one branch (`repoConfigName` → `RepoConfigName`) plus a new reference on
  another branch surfaces as an `undefined:` build error, not a merge conflict — always
  run `go build ./...` after each merge (do **not** pipe it to `tail`, or you mask the
  exit code).
- A function moved between packages on one branch with a test left behind on another
  yields an orphaned test referencing a now-undefined symbol — delete the orphan (its
  coverage moved with the function).

---

## 4. Finalize the changelog

Rewrite the `[Unreleased]` section into a dated version section with every merged
change, grouped Added / Changed / Removed / Fixed, each line referencing its issue:

```markdown
## [Unreleased]

## [X.Y.Z] - YYYY-MM-DD

### Install
` ``bash
brew install --cask KofTwentyTwo/tap/license-tool
` ``

### Added
- ... (#NN)
### Changed
- ... (#NN)
### Removed
- ... (#NN)
### Fixed
- ... (#NN)
```

The date **must** be today's date in `YYYY-MM-DD` — `scripts/verify-release.sh` checks
it. Keep an empty `## [Unreleased]` header above the new section.

---

## 5. Run the pre-release gate

From the repo root, the full local gate (the same checks CI enforces, plus the
GoReleaser dry run):

```bash
./scripts/verify-release.sh
```

It runs, in order: `gofmt`, `go vet`, `golangci-lint`, race tests with the CI coverage
profile, the 100% coverage checker, `goreleaser check`, `goreleaser release --snapshot
--clean`, tap-repo/token existence, the CHANGELOG date check, the SECURITY/README
content checks, and a clean-tree check.

If `goreleaser` is not installed locally, run the non-GoReleaser gates directly and let
CI run GoReleaser on the tag:

```bash
gofmt -l .                       # expect no output
go vet ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run
go test ./... -race -coverpkg=./internal/...,./cmd/... -covermode=atomic -coverprofile=cover.out
go run github.com/vladopajic/go-test-coverage/v2@v2.18.8 --config=.testcoverage.yml
```

The coverage gate is governed by `.testcoverage.yml` (the interactive TUI in
`cmd/.../main.go` and `wizard.go` is excluded as untestable). The checker prints
`Total coverage threshold (100%) satisfied: PASS` when the gate is met.

---

## 6. Push `develop`, then promote to `main`

```bash
git push origin develop
# wait for develop CI + CodeQL to go green:
gh run list --branch develop --limit 5

git checkout main && git reset --hard origin/main
git merge --no-ff develop -m "Release vX.Y.Z"
git push origin main
# wait for main CI + CodeQL to go green:
gh run list --branch main --limit 5
```

Branch protection currently enforces no required checks, so direct pushes succeed; still
wait for CI/CodeQL to pass on the release commit before tagging.

---

## 7. Tag and publish

From `main`, with CI green:

```bash
git tag -s vX.Y.Z -m "vX.Y.Z"     # signed annotated tag preferred
git push origin vX.Y.Z
```

Pushing the tag triggers the `Release` workflow, which runs `goreleaser release --clean`
to: cross-compile macOS/Linux arm64+amd64 and Windows amd64, build archives +
`checksums.txt`, create the GitHub Release, and publish the Homebrew cask to
`KofTwentyTwo/homebrew-tap`.

A tag with a prerelease suffix (`vX.Y.Z-rc.N`) is published as a GitHub **prerelease**
(`release.prerelease: auto`); use these from a `release/*` or `rc/*` branch to stabilize.

---

## 8. Verify the published release

```bash
gh run watch $(gh run list --workflow Release --limit 1 --json databaseId -q '.[0].databaseId')
gh release view vX.Y.Z                       # assets + checksums present
git -C "$(brew --repository KofTwentyTwo/homebrew-tap)" log --oneline -3   # cask bumped
brew update && brew install --cask KofTwentyTwo/tap/license-tool && license-tool version
```

`license-tool version` should print the new `vX.Y.Z`, the release commit, and the build
date injected by the workflow.

---

## 9. Required GitHub setup (one-time)

- Public Homebrew tap repo: `KofTwentyTwo/homebrew-tap`.
- Actions secret on `KofTwentyTwo/license-tool` named `HOMEBREW_TAP_TOKEN` (a token that
  can push to the tap).
- `gh auth status` must show write access before tagging.

---

## 10. If a release goes wrong

- **Workflow failed before publishing:** fix forward on `main`, then move the tag to the
  new commit and re-push:
  ```bash
  git tag -fas vX.Y.Z -m "vX.Y.Z"
  git push --force origin vX.Y.Z
  ```
  (Only safe before anyone has installed the release.)
- **Bad release already published:** do **not** reuse the version. Ship a `vX.Y.(Z+1)`
  patch with the fix; optionally mark the bad GitHub Release as a prerelease or delete it
  with `gh release delete vX.Y.Z`. The Homebrew cask should be rolled forward by the new
  tag, not hand-edited.
- **Changelog date check failed in CI:** the `## [X.Y.Z] - YYYY-MM-DD` date must match the
  release; correct it on `main` and re-tag.
