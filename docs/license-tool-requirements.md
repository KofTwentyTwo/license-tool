# license-tool — Requirements (the contract)

> Status: DRAFT for review. Produced from a `/grill-me` session on 2026-06-03.
> This file is the agreed scope. The companion `license-tool-design.md` says how it is built.
> Nothing here is implemented yet; repo, issue, and branch creation are deferred to your go-ahead.

## 1. Purpose

A standalone, public CLI for **auditing and standardizing license metadata across many codebases**. Its center of gravity is bulk migration: point it at a repo (or a fleet of them) and bring every source file's license header, and the repo's top-level license files, to one canonical, configurable standard. The audit half exists largely to serve and verify the apply half, and to gate CI.

It is intentionally generic: the target license, copyright holder, year policy, and header style are all inputs, not assumptions. AGPL-3.0-or-later with the Kingsrook notice is only the default profile.

## 2. Locked decisions

| # | Decision | Resolution |
|---|---|---|
| 1 | Implementation language | **Go** (single static binary; deliberate deviation from `4-preferences.yaml`, justified by fleet-wide distribution) |
| 2 | Scan scope | **Per-repo**, takes a path arg defaulting to cwd; runs locally or in that repo's CI. Fleet-wide runs are a thin outer loop over repos |
| 3 | Dependency-license resolution | **Tiered `Resolver` per ecosystem**: read already-resolved on-disk metadata by default; optionally shell out to the ecosystem's native tool behind a flag; deps that resolve to neither are reported **`unresolved`** with a reason, never guessed |
| 4 | License engine | **Fully generic / license-agnostic and parameterized**. Writes source-file headers **and** the top-level `LICENSE` file **and** `LICENSES/<id>.txt` (REUSE). License, holder, year, style are all inputs |
| 5 | Source of truth for license text | **Vendored snapshot of SPDX `license-list-data`** (real license texts + official `standardLicenseHeader` where defined). We never invent license text. Snapshot is pinned and refreshable |
| 6 | Canonical header format | **SPDX-first**. Default profile = full AGPL-3.0-or-later notice + REUSE tags. REUSE tags (`SPDX-FileCopyrightText` + `SPDX-License-Identifier`) are universal; the full notice block is emitted when the chosen license defines one and the style asks for it. SPDX identifier corrected to **`AGPL-3.0-or-later`** (the request's `AGPL-3.0` is deprecated and contradicts the "or later" grant) |
| 7 | Old-header recognition | **Signature-match + sentinel**. A leading comment is treated as a replaceable license header only if it carries our sentinel, any `SPDX-License-Identifier`, a known SPDX `standardLicenseHeader`, or a curated phrase fingerprint (e.g. "GNU Affero General Public License"). Anything ambiguous is **preserved and reported**, never edited |
| 8 | File-type coverage | **Broad, data-driven, user-extensible** comment-syntax table covering all common languages. Ordering rules (shebang, `<?xml?>`, `<?php`, BOM, encoding pragma, package/module decl) are built in. Uncommentable types (e.g. JSON) are **skipped and reported** |
| 9 | Apply safety | **Dry-run + unified diff by default**; explicit `--write` to apply. **Clean git working tree required** (override `--allow-dirty`); **atomic** temp-then-rename writes; git is the undo; **opt-in `--commit`** makes one atomic conventional commit per repo. Never deletes non-header content. Non-git dirs require `--force` |
| 10 | Configuration | **Layered YAML**. Committed per-repo `.license-tool.yaml` declares the repo's license identity and is both the apply input and the check expectation. Precedence: flags > repo config > user/global config > built-in defaults. Interactive prompt fills missing required fields on a TTY; a **hard error (no hang) in CI/non-TTY** |
| 11 | Audit policy | **Policy-driven classification**: category-tag each license (permissive / weak-copyleft / strong-copyleft / network-copyleft / proprietary / unknown) from SPDX metadata; flag heterogeneity and a curated set of well-known hard incompatibilities; enforce a config-defined policy (allow/deny SPDX lists, required license, fail conditions). **Always prints "not legal advice."** |
| 12 | Distribution | New **public** repo `github.com/KofTwentyTwo/license-tool`, **MIT** licensed (its own code). Productized like `notion-sql` (see §4): **GoReleaser** (not cargo-dist), gitflow branches, GitHub Actions CI + release, tagged releases with artifacts, Homebrew via `KofTwentyTwo/homebrew-tap` (`brew install KofTwentyTwo/tap/license-tool`), full community scaffolding |
| 13 | Name | `license-tool` |

## 3. Modes (from the request)

- **Mode A — Audit (read-only).** Report licenses in use across (a) dependencies, by detecting the ecosystems present and resolving per decision #3, and (b) source files, by detecting headers and flagging missing ones. Break results down by SPDX id, by file type, and by source-vs-dependency. Surface unknown licenses, missing headers, policy conflicts, and copyleft/permissive classification. Output to terminal (human), JSON (machine), and Markdown (report file). A `check` sub-mode exits non-zero to gate CI.
- **Mode B — Apply (writes files).** Add or update the canonical header on a single file, a directory (recursive), or the whole repo. Validate the target SPDX id against the vendored list. Generate per-language-correct comment syntax that will not break compilation or parsing, honoring preserve-first constructs. Produce legally proper text from canonical templates. Idempotent (detect-and-replace, never stack). Dry-run by default; explicit flag to write; never delete file content other than a positively-identified header block.

## 4. Repo, CI/CD, and publishing requirements (mirror notion-sql)

The repo must match notion-sql's productization, translated Rust to Go (GoReleaser in place of cargo-dist; every user-facing outcome identical).

**Repo identity**
- Public repo `github.com/KofTwentyTwo/license-tool`, MIT, Go module `github.com/KofTwentyTwo/license-tool`.
- Default branch `develop`. Gitflow: `feature/*` opens PRs into `develop`; `develop` runs CI + branch artifacts; `release/*` or `rc/*` stabilize; `main` is production. Branch protection on `main` and `develop`. (Matches notion-sql and `3-rules.md`.)

**CI (`.github/workflows/ci.yml`)**
- Triggers on PR and push to `develop`, `main`, `release/**`, `rc/**`; `permissions: contents: read`.
- Linux lint+test job: `gofmt`/`goimports` check, `go vet`, `golangci-lint` (warnings as errors), `go test ./... -race -cover`, `go build`.
- Per-OS branch builds on push (macOS, Windows) uploading artifacts named `license-tool-<run_id>-<sha>-<os>`.

**Release / publishing (`.github/workflows/release.yml` + `.goreleaser.yaml`)**
- Tag matching `v[0-9]+.[0-9]+.[0-9]+*` runs GoReleaser: cross-compile mac/linux arm64+amd64 and windows amd64, archives + `checksums.txt`, GitHub Release (prerelease when the tag carries an rc/prerelease suffix), and Homebrew formula publish to `KofTwentyTwo/homebrew-tap` as `Formula/license-tool.rb`.
- Install UX: `brew install KofTwentyTwo/tap/license-tool`.
- Requires Actions secret `HOMEBREW_TAP_TOKEN` with contents-write on the tap; never hardcoded.

**Governance / community files (mirror notion-sql, adapted to this domain)**
- `README.md`, `LICENSE` (MIT), `CHANGELOG.md` (Keep a Changelog + SemVer, with `[Unreleased]`), `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`, `SECURITY.md` (supported-versions table + private advisory reporting), `RELEASING.md` (gitflow + GoReleaser commands), `scripts/verify-release.sh` (pre-release gate), `.github/ISSUE_TEMPLATE/{bug_report.md,feature_request.md,config.yml}`, `.github/pull_request_template.md`, `.github/dependabot.yml` (`gomod` + `github-actions`, weekly, limit 5), `.gitignore` (Go build output, `/dist/`, env/secrets, OS/editor cruft, agent state `.serena/.claude/.codex/.agents/.cursor`, `REQUEST*.md`).

**Divergence note:** notion-sql's `verify-release.sh` uses emoji status markers. Per your no-emoji rule, our copy will use plain `[ok]`/`[FAIL]` text unless you want a byte-for-byte emoji match.

## 5. Non-goals (v1)

- No authoritative legal compatibility determination (policy enforcement only, with disclaimer).
- No network-based dependency-license resolution (registry fetch) in v1; on-disk + optional shell-out only.
- No fleet orchestrator binary in v1; multi-repo sweeps are a documented outer loop (shell) over the per-repo tool.
- Ecosystems with zero presence in the current fleet (Go modules, Cargo, Python, Ruby, PHP) are lower priority than Maven, npm/pnpm, and Gradle for the dependency resolver; the source-header half is language-broad regardless.

## 6. Open items to confirm during design review

- Year policy default (proposed: derive `first-commit-year-to-current` range from git, configurable to `current` / explicit).
- Whether top-level `LICENSE` / `LICENSES/` management is a flag on `apply` or a dedicated subcommand (proposed: `license-tool license` subcommand + `apply` flag).
- Exact Go library choices (CLI framework, SPDX data access, license classifier) — pinned in design, validated at implementation.
