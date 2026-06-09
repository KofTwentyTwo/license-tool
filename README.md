# license-tool

`license-tool` is a Go CLI for auditing and standardizing license metadata across many codebases.

It brings every source file's license header, and a repo's top-level license files, to one canonical, configurable standard. The target license, copyright holder, year policy, and header style are all inputs, not assumptions. AGPL-3.0-or-later with the Kingsrook notice is only the default profile.

The tool has two halves: an audit half that reports the licenses in use across dependencies and source files (and gates CI), and an apply half that adds or updates the canonical header on a file, directory, or whole repo. License text is never invented; it comes from a vendored snapshot of the SPDX license list.

## Install

Homebrew releases are configured through GoReleaser:

```bash
brew install --cask KofTwentyTwo/tap/license-tool
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
license-tool audit --deps=false
license-tool audit --resolve-deps tool
license-tool audit --summary                 # counts only; no per-file/dependency lists
license-tool audit --group-by license        # group source files by license
license-tool audit --group-by directory      # group by top-level directory
license-tool audit --summary --group-by type # per-group counts only
license-tool audit --group-by directory --depth 2  # group by two path segments
license-tool audit --group-by license --sort count # most common first
license-tool audit --only missing,copyleft         # list only problem files
```

By default the report lists every source file. `--summary` keeps the `findings:`
overview and the by-SPDX / by-category / by-file-type rollups but omits the per-file
and per-dependency lists and any pending diffs. `--group-by license|category|type|directory`
organizes the source-file listing under each value of the dimension instead of a flat
list; combined with `--summary` it shows per-group counts only. Each group reports its
worst license **risk** (`high`/`medium`/`low`, or `unknown` for a headerless group)
and, for non-license groupings, its license breakdown, so a `directory` view is not
license-blind. Group risk is **policy-aware**: it escalates to `high` when the group's
license is party to a repo-level hard incompatibility (e.g. an Apache group beside an
AGPL group) or a file in it carries a policy violation, so a group is never reported
"low" while it is actually an audit liability. `--sort key|count`
orders rollups and groups; `--depth N` widens directory keys to N path segments;
`--only missing,unknown,copyleft,violations` narrows the file listing to problem files
without distorting the rollups. The flags apply to text, markdown, and JSON (which
always emits the complete report — `--summary` only trims the human formats). An
unknown `--group-by`/`--sort`/`--only` value is a usage error.

Every format now carries the `findings` summary (coverage, license mix, risk level,
copyleft, dependency resolution, policy) and **attributable policy violations** — each
violation names the offending license, the rule, and the file (text/markdown) or a
structured `violationDetails` array (JSON), so both engineers and tools can see *why*
a check failed, not just that it did. Rollups show per-row percentages and totals.

The tool's own `.license-tool.yaml` is treated as metadata, not coverable source: it
is listed as skipped (reason `tool config`) and never counted toward source-header
coverage, so it cannot inflate the missing-header tally.

Audit output is read-only. Audit always prints a "not legal advice" disclaimer.
Text output starts with a `findings:` summary that calls out source-file header
coverage, license types, unknown or unrecognized license ids, copyleft licenses,
the policy result, and dependency resolution counts when dependencies are found.

```text
findings:
  source files: 12 (headered 10, missing 2)
  license types: AGPL-3.0-or-later 10, none 2
  unknown/unrecognized: 0
  copyleft: AGPL-3.0-or-later
  dependencies: 8 (resolved 6, unresolved 2)
  policy: FAIL (1: policy-violation)
```

`--group-by` turns the flat file list into a grouped view. For example,
`audit --group-by license` shows exactly which files carry which license, and where
the gaps are:

```text
source files by license:
  (none) (1)
    src/legacy.go  [no managed header]
  AGPL-3.0-or-later (2)
    src/a.go  [AGPL-3.0-or-later]
    src/b.go  [AGPL-3.0-or-later]
  Apache-2.0 (1)
    scripts/run.sh  [Apache-2.0]
  MIT (2)
    web/c.ts  [MIT]
    web/d.ts  [MIT]
  (skipped: 6)
```

For a tree-shaped view of coverage, `audit --summary --group-by directory` answers
"which parts of the repo are licensed" at a glance, with no per-file noise:

```text
source files by directory:
  scripts (1)
  src (3)
  web (2)
  (skipped: 6)
```

And `audit --group-by license --format json` emits the same grouping as machine data
(a `groups` array of `{key, count, files}`) for dashboards and CI summaries.

Dependency audit discovers manifests in the root and in subdirectories, while
honoring git, `.gitignore`, configured excludes, and common vendor-heavy
directories such as `node_modules`, `vendor`, `build`, `dist`, and `.gradle`.
The default `ondisk` tier reads already-resolved metadata. The `tool` tier
currently augments Maven resolution only; npm uses installed `node_modules`
metadata, and Gradle remains detect-only with an explicit unsupported-tool-tier
reason in the dependency report.

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

Scaffold a `.license-tool.yaml` for the repo. On a TTY, `init` runs a wizard with
a filterable SPDX license picker that lists common licenses first, then prompts
for holder, year policy, header style, and whether to manage the top-level
`LICENSE` files. The holder prompt requires a non-empty value, and the license
and year prompts validate against the same parsers used by the flag path.

```bash
license-tool init
license-tool init --license MIT --holder "Example, Inc." --year git --style reuse+notice
```

In a non-TTY environment, `init` skips the wizard, uses only the supplied flags,
and validates through the same gate. Missing or invalid license and holder values
are usage errors. If `.license-tool.yaml` already exists, `init` refuses to
overwrite it unless `--force` is passed.

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

Configuration is layered. Precedence, high to low: flags, the per-repo `.license-tool.yaml`, the user/global config (`$XDG_CONFIG_HOME/license-tool/config.yaml`), then built-in defaults. The committed `.license-tool.yaml` declares the repo's license identity and doubles as the `check` expectation. For `init`, missing required fields prompt through the wizard on a TTY; non-TTY runs skip the wizard, use flags only, and report missing or invalid values as usage errors.

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

## Support

Use [GitHub Issues](https://github.com/KofTwentyTwo/license-tool/issues) for public
bug reports, feature requests, and documentation defects. Do not report security
vulnerabilities in public issues; follow [SECURITY.md](SECURITY.md) instead.

## Release Automation

Release automation is handled by GoReleaser.

- Branch pushes and PRs run CI.
- Pushes to `develop`, `release/*`, `rc/*`, and `main` also upload branch build artifacts.
- Version tags matching `vX.Y.Z` create production GitHub Releases and publish the Homebrew cask.
- Version tags carrying an rc or prerelease suffix create GitHub prereleases.
- The Homebrew cask publishes to `KofTwentyTwo/homebrew-tap`, which requires the `HOMEBREW_TAP_TOKEN` repository secret.

See [RELEASING.md](RELEASING.md) for the full flow.
