# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
