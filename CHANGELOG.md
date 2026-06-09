# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.4.0] - 2026-06-09

### Install
```bash
brew install --cask KofTwentyTwo/tap/license-tool
```

### Added
- `init` now opens a full-screen Bubble Tea wizard with project-model, license, identity, header-style, license-file, coverage, and review steps (#29).
- Live example source previews for 15 language families, plus a `.license-tool.yaml` preview and a C fallback when no supported source language is detected (#29).
- Persisted `include` config support, wired through audit, apply, license, and init (#29).
- `audit --summary`: a counts-only report (findings plus the by-SPDX, by-category, and by-file-type rollups) that omits the per-file and per-dependency lists and any pending diffs (#35).
- `audit --group-by license|category|type|directory`: organizes the source-file listing under each value of the dimension; combined with `--summary` it shows per-group counts only (#35).
- `audit --sort key|count`, `--depth N` (widen directory keys to N path segments), and `--only missing,unknown,copyleft,violations` (narrow the file listing to problem files without distorting the rollups) (#35).
- Per-group **risk** markers and per-group license breakdowns, so a `directory` or `type` grouping is not license-blind (#35).
- Policy-aware group risk: a group's risk escalates to `high` when its license is party to a repo-level hard incompatibility (e.g. an Apache group beside an AGPL group) or a file in it carries a policy violation, instead of reporting the license category's risk alone (#35).
- Attributable policy violations in every format: each violation names the offending license, the rule, and the file (text/markdown) or a structured `violationDetails` array (JSON) (#35).
- `findings` summary, per-row percentages/totals, and `riskLevel`/`worstCategory` in the JSON and markdown reports (previously text-only) (#35).

### Changed
- The JSON report always emits the complete report; `--summary` only trims the human-readable (text/markdown) formats (#35).
- Renamed the headerless source-file bucket label to `(no-header)` (#35).
- Dependency resolution no longer guesses: ambiguous license aliases (a bare `bsd`, a bare LGPL name, EPL spellings) now resolve to *unresolved* with a reason instead of a fabricated SPDX id, and alias targets are held to the curated set so a "resolved" dependency is always classifiable (#31).

### Removed
- Removed the no-op `--quiet`/`-q` and `--verbose`/`-v` flags, which were bound but never read; they implied behavior the tool did not deliver (#34).

### Fixed
- Confined license-header detection to contiguous comment lines and required the `SPDX-License-Identifier:` tag to begin a comment line, so `apply` no longer risks deleting an adjacent doc comment separated from the header by a blank line (#30).
- Refuse to overwrite a symlinked `LICENSE` / `LICENSES/*.txt` instead of silently converting the symlink into a regular file via the atomic write (#33).
- Excluded the tool's own `.license-tool.yaml` from source-header coverage (it is listed as skipped with reason `tool config`), so it no longer inflates the source-file and missing-header counts and `check` no longer fails on it for lacking a header (#35).
- Made per-group risk reflect the full repo's incompatibilities under `--only`: the risk marker is now derived from the whole report, so narrowing the listing no longer understates a group's risk (#35).

## [0.3.0] - 2026-06-04

### Install
```bash
brew install --cask KofTwentyTwo/tap/license-tool
```

### Added
- Added first-class header support for Perl, PowerShell, R, Makefile, TOML, and Batch files.
- Added conservative shebang-based type detection for extensionless scripts.
- Added GitHub issue templates, a pull request template, support guidance, and refreshed security-policy documentation.

### Changed
- Aligned init/apply/license SPDX validation with the licenses that can actually be rendered by the bundled SPDX data.
- Validated policy `required`, `allow`, and `deny` entries as exact SPDX identifiers, matching the current exact-match policy engine.
- Extended dependency audit discovery to nested manifests while preserving ignore and exclude behavior.
- Made Gradle tool-tier dependency resolution report an explicit unsupported reason.
- Pinned release workflow actions and GoReleaser, and updated release packaging to publish the Homebrew cask.

### Fixed
- Preserve leading Go build constraints, CSS `@charset` rules, and markup doctypes before inserted headers so `apply` does not silently change build selection, stylesheet encoding, or browser rendering mode.
- Preserve Python `coding=` and `coding:` pragmas before inserted headers.
- Centralized header sentinel and placement logic so audit and render paths agree on preserve-first rules.
- Made the `license` command use the same write safety gates and commit support as `apply`.
- Scoped apply/license commits to files touched by the license run so unrelated dirty working-tree changes are not staged.
- Made `apply` honor include and gitignore scope controls before writing files.
- Rendered unified diffs for dry-run `apply` and `license` runs, including JSON diff entries only when present.
- Restored CLI error output and documented exit-code behavior.
- Made `check --fail-on` override policy fail conditions, including repeated and comma-separated flag values.
- Honored audit/check `--output` controls, rendered check reports, and bound the documented `--no-deps` flag.
- Rejected invalid `--resolve-deps` values instead of silently falling back to `ondisk`.

## [0.2.1] - 2026-06-04

### Fixed
- Preserve a leading `#!` shebang universally (not per-type), so `apply` never inserts the header before the shebang. Fixes corruption of shebang-bearing scripts whose type lacked an explicit shebang rule (e.g. a PHP CLI script `#!/usr/bin/env php` + `<?php`), where the block-comment header was written at byte 0, breaking the file.

## [0.2.0] - 2026-06-03

### Added
- `init`: interactive wizard (charmbracelet/huh) to scaffold `.license-tool.yaml` — filterable SPDX license picker (common licenses first), holder/year/style prompts with validation, and an overwrite guard; non-TTY path uses flags
- `audit`: findings-summary block at the top of the text report (source files headered/missing, license types, unknown, copyleft flags, dependencies resolved/unresolved, policy PASS/FAIL)

### Fixed
- `isTTY` now uses `golang.org/x/term` so `/dev/null` and other char devices are not mistaken for a terminal (correct non-interactive behavior in CI/scripts)

## [0.1.1] - 2026-06-03

### Fixed
- Build: set the go directive to 1.24 to match dependencies and the CI golangci-lint build version
- CI: bump golangci-lint-action to v8 for the golangci-lint v2 config schema
- Tests: pin `git init -b main` in gitutil tests (CI runners default to master)

## [0.1.0] - 2026-06-03

### Added
- `audit` mode: report licenses across dependencies and source files (text, JSON, Markdown)
- `check` mode: CI gate with non-zero exit on policy violation
- `apply` mode: add or update canonical license headers (dry-run by default, `--write` to apply); idempotent; preserves shebang/XML-decl/BOM/pragma ordering; clean relicensing; git-native safety
- `license` mode: manage top-level LICENSE and LICENSES/<id>.txt (REUSE)
- `init` mode: scaffold a `.license-tool.yaml`
- Vendored SPDX license-list-data snapshot as the source of truth for license text
- Layered YAML configuration with per-repo `.license-tool.yaml`
- 100% test coverage with a CI coverage gate; GoReleaser + Homebrew tap distribution
