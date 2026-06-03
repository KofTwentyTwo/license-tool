# license-tool — Design

> Status: DRAFT for review. Companion to `license-tool-requirements.md` (the contract).
> Per the request cadence: review this before any implementation code is written.

## Purpose

A single Go CLI with two modes, audit (read-only) and apply (writes), for standardizing license metadata across many codebases. See the requirements doc for the agreed scope; this doc specifies the interface, internals, edge cases, dependencies, tests, and how it ships.

## CLI interface

Binary: `license-tool`. Cobra-style subcommands. A `[path]` argument defaults to `.`.

```
license-tool audit   [path]   # Mode A: report, read-only
license-tool check   [path]   # Mode A: CI gate, non-zero exit on policy violation
license-tool apply   [path]   # Mode B: write/update headers (dry-run unless --write)
license-tool license [path]   # Manage top-level LICENSE + LICENSES/<id>.txt (REUSE)
license-tool init    [path]   # Scaffold a .license-tool.yaml (interactive on a TTY)
license-tool version          # Version, commit, build date
```

Shared flags: `--config <file>`, `--include <glob>` (repeatable), `--exclude <glob>` (repeatable), `--no-gitignore`, `--quiet|-q`, `--verbose|-v`.

`audit` / `check` flags: `--format text|json|markdown` (default `text`; `check` forces machine-friendly), `--output <file>`, `--deps|--no-deps` (default on), `--resolve-deps ondisk|tool|off` (default `ondisk`), `--fail-on <conditions>` (check only; default `missing-header,unknown-license,policy-violation`).

`apply` / `license` flags: `--license <SPDX>` (validated against vendored list), `--holder <text>`, `--year <spec>` (`current` | `YYYY` | `YYYY-YYYY` | `git`; default `git`), `--style reuse|notice|reuse+notice` (default from config, falls back to `reuse+notice`), `--write` (without it: dry-run + unified diff), `--allow-dirty`, `--force` (non-git dirs), `--commit` (one atomic conventional commit per repo), `--commit-message <tmpl>`.

Exit codes: `0` ok; `1` policy/check failure; `2` usage error; `3` write refused (dirty tree / non-git without `--force`); `4` internal error.

## Configuration

Layered, precedence high to low: flags > repo `.license-tool.yaml` > user/global (`$XDG_CONFIG_HOME/license-tool/config.yaml`) > built-in defaults. Missing required fields (license, holder) prompt on a TTY and hard-error in CI.

```yaml
# .license-tool.yaml (committed per repo; doubles as the check expectation)
license: AGPL-3.0-or-later        # target SPDX id
holder: "Kingsrook, LLC"          # copyright holder
year: git                         # current | YYYY | YYYY-YYYY | git
style: reuse+notice               # reuse | notice | reuse+notice
manage_license_file: true         # write top-level LICENSE + LICENSES/<id>.txt
exclude:                          # gitignore-style, in addition to .gitignore
  - "**/generated/**"
  - "**/*.pb.go"
policy:                           # drives audit classification + check exit
  required: AGPL-3.0-or-later
  allow: [AGPL-3.0-or-later, Apache-2.0, MIT, BSD-3-Clause]
  deny:  [GPL-2.0-only]
  fail_on: [missing-header, unknown-license, policy-violation]
file_types:                       # optional overrides/additions to the built-in table
  ".myext": { style: line, line: "// " }
```

## Data flow

1. **Resolve config** (layer + prompt/error as above).
2. **Enumerate files**: `git ls-files` when inside a git repo (inherits `.gitignore` correctly), else a pathspec walker honoring `.gitignore` plus config excludes. Skip symlinks. Skip binaries via a null-byte/UTF-8-validity heuristic. Classify each file via the file-type table.
3. **Detect headers** (per source file): read the leading region, step past preserve-first prefixes, then identify an existing license header by sentinel / `SPDX-License-Identifier` / known `standardLicenseHeader` / curated phrase fingerprint. Extract current SPDX id, holder, year when present. Non-license leading comments are recorded as "no managed header."
4. **Resolve dependency licenses** (audit only): detect ecosystems by manifest presence (Maven `pom.xml`, npm/pnpm `package.json`+lockfiles, Gradle `build.gradle[.kts]`, plus extensible others); per ecosystem run the tiered `Resolver` (on-disk metadata; optional shell-out; else `unresolved`).
5. **Classify + apply policy**: map each detected license to a category from SPDX metadata; evaluate allow/deny/required and the curated incompatibility table; mark violations.
6. **Render or mutate**:
   - audit/check: build one report model, render text/JSON/Markdown; `check` sets exit per `fail_on`.
   - apply/license: render the new header from the template (vendored SPDX text + holder/year/style) and the target LICENSE files; compute a unified diff per file; with `--write`, atomically replace the identified header block (or insert at the correct position when absent), preserving preserve-first prefixes, line endings (LF/CRLF), and trailing newline; optionally `--commit`.

## File-type table (data-driven)

Each entry: extensions/filenames, comment style (`block` with open/close delimiters, or `line` with a prefix), and an ordered list of preserve-first rules. Shipped defaults cover Java, Kotlin, Swift, C/C++/H/Obj-C, Go, Rust, Python, JS/TS/JSX/TSX, shell, Nix, HCL/Terraform, YAML, XML/HTML, CSS/SCSS/LESS, SQL, Ruby, PHP, Lua, Dockerfile, and more. Users add/override via `file_types` in config. Uncommentable formats (JSON and friends) are explicitly marked skip-and-report.

The header text is rendered once as license-id + holder + year + style, then re-wrapped into each file's comment syntax (e.g. `/* ... */` for Java, `# ...` for shell/YAML, `<!-- ... -->` after the XML declaration for `pom.xml`).

## Key edge cases (must be tested)

- **Preserve-first ordering**: shebang (`#!`), `<?xml ...?>`, `<?php`, UTF-8 BOM, encoding pragma (`# -*- coding: utf-8 -*-`), Java/Kotlin `package`. Header is placed after these where required, before `package`.
- **Idempotency**: second `apply` is a no-op; re-run never stacks or drifts.
- **Relicense**: AGPL→Apache removes the old notice block entirely and writes the new one (no stacked/contradictory notices).
- **Foreign leading comment**: a top-of-file Javadoc/JSDoc/module doc that is not a license is preserved untouched and reported.
- **Uncommentable / empty / all-header / generated / very large / CRLF / no-trailing-newline** files handled without corruption.
- **Mixed line endings and BOM** preserved exactly outside the header region.

## Dependencies (Go)

Proposed, to be pinned at implementation and signed off (per your dependency rule):

- CLI: `github.com/spf13/cobra` (+ a light config layerer; `knadh/koanf` or `spf13/viper`).
- SPDX data: vendored snapshot of `spdx/license-list-data` (texts + standard headers); `github.com/github/go-spdx` for SPDX-expression validation.
- License classification (recognize license text in headers/LICENSE files and dependency metadata): `github.com/google/licensecheck`.
- YAML: `gopkg.in/yaml.v3`.
- Unified diff rendering: `github.com/hexops/gotextdiff` (or equivalent).
- gitignore semantics fallback: prefer shelling to `git ls-files`; `github.com/sabhiram/go-gitignore` for the non-git path.
- Tests: stdlib `testing` + golden files; `github.com/stretchr/testify` for assertions.

## Test plan

- **Table-driven comment generation** across representative types (Java block; TS line and block; Python with shebang and pragma; shell with shebang; XML with declaration; YAML; Nix; C; Rust; HCL) and every preserve-first edge case.
- **Golden-file rendering** per license/style profile (AGPL-3.0-or-later reuse+notice, Apache-2.0 reuse+notice, MIT reuse).
- **Idempotency + relicense**: apply twice equals once; AGPL→Apache replaces, never stacks.
- **Detection**: recognizes the current non-SPDX AGPL header via fingerprint; never touches a non-license doc comment.
- **Audit/check**: fixture repos per ecosystem (Maven, npm/pnpm, Gradle); policy and exit-code matrix; `unresolved` path when toolchain/metadata absent.
- **Safety**: dirty-tree refusal; atomic write under simulated crash; no-corruption on uncommentable/binary.
- **Coverage floors** (from `4-preferences.yaml`, adapted to Go): 0.70 overall, 0.90 on the core header-generation and detection packages.

## Distribution and repo scaffolding (mirror notion-sql)

Repo `github.com/KofTwentyTwo/license-tool`, MIT, Go module `github.com/KofTwentyTwo/license-tool`, default branch `develop` with gitflow. The productization mirrors notion-sql artifact for artifact, translated Rust to Go.

### notion-sql to license-tool mapping

| notion-sql (Rust) | license-tool (Go) | Notes |
|---|---|---|
| `Cargo.toml` (version, metadata) | `go.mod` + version via `-ldflags` | |
| `src/*.rs` | `cmd/license-tool/` + `internal/` | |
| `dist-workspace.toml` (cargo-dist) | `.goreleaser.yaml` | release config |
| `release.yml` (dist-generated) | `release.yml` (GoReleaser action on `v*` tags) | same trigger pattern |
| `ci.yml` (fmt/clippy/test/build) | `ci.yml` (gofmt/vet/golangci-lint/`go test -race`/build) | same branches, same per-OS artifact uploads |
| `.tar.xz` archives | `.tar.gz` (zip on Windows) | GoReleaser default |
| `Formula/notion-sql.rb` in tap | `Formula/license-tool.rb` (GoReleaser-generated) | same `KofTwentyTwo/homebrew-tap` |
| `dependabot.yml` (cargo + actions) | `dependabot.yml` (`gomod` + actions) | |
| `RELEASING.md` (dist commands) | `RELEASING.md` (GoReleaser commands) | gitflow identical |
| `scripts/verify-release.sh` (cargo+dist gates, emoji) | same 6-gate shape (go+goreleaser gates, plain text) | emoji removed per rules |
| `SECURITY/CONTRIBUTING/CODE_OF_CONDUCT/CHANGELOG` | same, adapted | Keep a Changelog + SemVer |
| issue/PR templates (SQL domain) | issue/PR templates (license/header domain) | |

### Repo layout

```
cmd/license-tool/            # main, version vars set via ldflags
internal/                    # config, enumerate, detect, render, resolve, policy, report
data/spdx/                   # vendored SPDX license-list-data snapshot (embedded via go:embed)
testdata/                    # golden headers + per-ecosystem fixture repos
docs/ examples/ scripts/
.github/workflows/{ci.yml,release.yml}
.github/ISSUE_TEMPLATE/{bug_report.md,feature_request.md,config.yml}
.github/{pull_request_template.md,dependabot.yml}
.goreleaser.yaml  go.mod  README.md  LICENSE  CHANGELOG.md
CONTRIBUTING.md  CODE_OF_CONDUCT.md  SECURITY.md  RELEASING.md  .gitignore
```

### `.goreleaser.yaml` (sketch)

```yaml
builds:
  - main: ./cmd/license-tool
    binary: license-tool
    env: [CGO_ENABLED=0]
    goos: [darwin, linux, windows]
    goarch: [amd64, arm64]
    ignore: [{goos: windows, goarch: arm64}]
    ldflags: ["-s -w -X main.version={{.Version}} -X main.commit={{.Commit}} -X main.date={{.Date}}"]
archives:
  - formats: [tar.gz]
    format_overrides: [{goos: windows, formats: [zip]}]
checksum: {name_template: checksums.txt}
release: {prerelease: auto}
brews:
  - repository: {owner: KofTwentyTwo, name: homebrew-tap, token: "{{ .Env.HOMEBREW_TAP_TOKEN }}"}
    name: license-tool
    homepage: https://github.com/KofTwentyTwo/license-tool
    description: "Audit and standardize license headers and metadata across codebases"
    license: MIT
```

### CI / release workflows

- `ci.yml`: triggers on PR and push to `develop`, `main`, `release/**`, `rc/**`; `permissions: contents: read`. Linux job runs `gofmt -l` (fail if non-empty), `go vet`, `golangci-lint run`, `go test ./... -race -cover`, `go build ./...`. macOS and Windows jobs build on push and upload `license-tool-<run_id>-<sha>-<os>` artifacts.
- `release.yml`: on `v[0-9]+.[0-9]+.[0-9]+*` tags, checkout with full history, set up Go, run `goreleaser release --clean` with `GITHUB_TOKEN` and `HOMEBREW_TAP_TOKEN` in env.

### Required GitHub setup (same as notion-sql)

- `KofTwentyTwo/homebrew-tap` exists (confirmed; already holds `notion-sql` and `limen` formulas).
- Add Actions secret `HOMEBREW_TAP_TOKEN` (contents-write on the tap) to the `license-tool` repo.
- Branch protection on `main` and `develop`; verify with `gh auth status` and `git ls-remote`.
- `verify-release.sh` gates before tagging: format/vet/lint/test, `goreleaser check` and a `goreleaser release --snapshot --clean` dry run, tap repo + token existence, CHANGELOG date set, SECURITY/README content checks, clean working tree.

## Post-approval sequencing (deferred until you approve this design)

1. Create the KofTwentyTwo GitHub issue describing the tool (tracking).
2. Create `github.com/KofTwentyTwo/license-tool`; scaffold the full notion-sql-equivalent skeleton (community files, CI/release workflows, `.goreleaser.yaml`, layout) and commit to `main`, then branch `develop` as the default working branch with protection on both.
3. Add the `HOMEBREW_TAP_TOKEN` Actions secret.
4. Branch `feature/GH-<n>-initial-implementation` off `develop`; implement against this design with tests; PR into `develop`.
5. Promote `develop` to `main` and tag `v0.1.0` to validate the GoReleaser + Homebrew path end to end (`brew install KofTwentyTwo/tap/license-tool`).
