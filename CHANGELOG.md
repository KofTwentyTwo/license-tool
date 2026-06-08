# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- `init` now opens a full-screen Bubble Tea wizard with project-model, license, identity, header-style, license-file, coverage, and review steps.
- Added live example source previews for 15 language families, plus `.license-tool.yaml` preview and C fallback when no supported source language is detected.
- Added persisted `include` config support and wired wizard/flag include values into audit, apply, license, and init behavior.

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
