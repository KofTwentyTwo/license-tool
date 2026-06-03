# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Initial scaffolding: repo layout, GoReleaser config, CI and release workflows, community files
- `audit` mode: report licenses across dependencies and source files (text, JSON, Markdown)
- `check` mode: CI gate with non-zero exit on policy violation
- `apply` mode: add or update canonical license headers (dry-run by default, `--write` to apply)
- `license` mode: manage top-level LICENSE and LICENSES/<id>.txt (REUSE)
- `init` mode: scaffold a `.license-tool.yaml`
- Vendored SPDX license-list-data snapshot as the source of truth for license text
- Layered YAML configuration with per-repo `.license-tool.yaml`

### Changed
- None

### Fixed
- None
