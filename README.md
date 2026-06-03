# license-tool

`license-tool` is a Go CLI for auditing and standardizing license metadata across many codebases.

It brings every source file's license header, and a repo's top-level license files, to one canonical, configurable standard. The target license, copyright holder, year policy, and header style are all inputs, not assumptions. AGPL-3.0-or-later with the Kingsrook notice is only the default profile.

The tool has two halves: an audit half that reports the licenses in use across dependencies and source files (and gates CI), and an apply half that adds or updates the canonical header on a file, directory, or whole repo. License text is never invented; it comes from a vendored snapshot of the SPDX license list.

## Install

Homebrew releases are configured through GoReleaser:

```bash
brew install KofTwentyTwo/tap/license-tool
```

From source:

```bash
go install github.com/KofTwentyTwo/license-tool/cmd/license-tool@latest
```

## Pre-built Binaries

Pre-built binaries are available for all major platforms:

- **macOS**: ARM64 (Apple Silicon) and x86_64
- **Linux**: ARM64 and x86_64
- **Windows**: x86_64

Download from the [GitHub Releases](https://github.com/KofTwentyTwo/license-tool/releases) page.

## Usage

```bash
license-tool audit   [path]   # report licenses in use, read-only
license-tool check   [path]   # CI gate, non-zero exit on policy violation
license-tool apply   [path]   # add or update headers (dry-run unless --write)
license-tool license [path]   # manage top-level LICENSE + LICENSES/<id>.txt (REUSE)
license-tool init    [path]   # scaffold a .license-tool.yaml (interactive on a TTY)
license-tool version          # version, commit, build date
```

A `[path]` argument defaults to the current directory.

### audit

Report the licenses in use across dependencies and source files. Results break down by SPDX id, by file type, and by source-vs-dependency, and surface unknown licenses, missing headers, policy conflicts, and copyleft/permissive classification.

```bash
license-tool audit
license-tool audit ./some/repo
license-tool audit --format json --output audit.json
license-tool audit --format markdown --output LICENSE-AUDIT.md
license-tool audit --no-deps
license-tool audit --resolve-deps tool
```

Audit output is read-only. Audit always prints a "not legal advice" disclaimer.

### check

Run the audit as a CI gate. `check` exits non-zero when the configured fail conditions are met, and forces a machine-friendly output format.

```bash
license-tool check
license-tool check --fail-on missing-header,unknown-license,policy-violation
```

### apply

Add or update the canonical header on a single file, a directory (recursive), or the whole repo. `apply` is dry-run by default and prints a unified diff; pass `--write` to mutate files.

```bash
license-tool apply                         # dry-run, prints unified diff
license-tool apply --write                 # apply changes
license-tool apply --license Apache-2.0 --holder "Example, Inc." --write
license-tool apply --year git --style reuse+notice --write
license-tool apply --write --commit        # one atomic conventional commit per repo
license-tool apply ./src/file.go --write   # single file
```

Apply requires a clean git working tree (override with `--allow-dirty`), writes atomically (temp-then-rename), and is idempotent: a second apply is a no-op and headers never stack. Non-git directories require `--force`. Git is the undo.

### license

Manage the top-level `LICENSE` file and the `LICENSES/<id>.txt` tree (REUSE). Shares the apply flags.

```bash
license-tool license --write
license-tool license --license AGPL-3.0-or-later --holder "Kingsrook, LLC" --write
```

### init

Scaffold a `.license-tool.yaml` for the repo. On a TTY this prompts for missing required fields; in CI or a non-TTY it hard-errors rather than hanging.

```bash
license-tool init
```

### Shared flags

```text
--config <file>        explicit config file
--include <glob>       restrict to matching paths (repeatable)
--exclude <glob>       skip matching paths (repeatable)
--no-gitignore         do not honor .gitignore during enumeration
--quiet, -q            suppress non-essential output
--verbose, -v          verbose output
```

### Exit codes

```text
0  ok
1  policy / check failure
2  usage error
3  write refused (dirty tree, or non-git without --force)
4  internal error
```

## Configuration

Configuration is layered. Precedence, high to low: flags, the per-repo `.license-tool.yaml`, the user/global config (`$XDG_CONFIG_HOME/license-tool/config.yaml`), then built-in defaults. The committed `.license-tool.yaml` declares the repo's license identity and doubles as the `check` expectation. Missing required fields (license, holder) prompt on a TTY and hard-error in CI.

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

## Development

```bash
gofmt -l .
go vet ./...
golangci-lint run
go test ./... -race -cover
go build ./...
```

The risky code paths are covered by tests:

- Comment generation across representative file types and every preserve-first edge case.
- Golden-file header rendering per license/style profile.
- Idempotency and relicense (apply twice equals once; AGPL to Apache replaces and never stacks).
- Header detection (recognize an existing license header; never touch a non-license doc comment).
- Audit/check policy and exit-code matrix, including the `unresolved` dependency path.

## Release Automation

Release automation is handled by GoReleaser.

- Branch pushes and PRs run CI.
- Pushes to `develop`, `release/*`, `rc/*`, and `main` also upload branch build artifacts.
- Version tags matching `vX.Y.Z` create production GitHub Releases and publish the Homebrew formula.
- Version tags carrying an rc or prerelease suffix create GitHub prereleases.
- The Homebrew formula publishes to `KofTwentyTwo/homebrew-tap`, which requires the `HOMEBREW_TAP_TOKEN` repository secret.

See [RELEASING.md](RELEASING.md) for the full flow.
