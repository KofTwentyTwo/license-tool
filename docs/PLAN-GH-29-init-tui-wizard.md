# PLAN: GH-29 Init TUI Wizard

## Goal
Redesign `license-tool init` into a full-screen configuration wizard that explains each choice through a live example source-file preview before writing `.license-tool.yaml`.

## Approach
Keep non-TTY `init` scriptable, but replace the interactive `huh` form with a Bubble Tea TUI. The wizard remains a configuration tool, not legal advice: it presents licensing models, common SPDX choices, private/internal paths, holder/year/style controls, include/exclude coverage, and a final review screen. The preview defaults to an example source file rendered with the current answers; detected repo language chooses the example, and C is the fallback.

## Files Affected
- `cmd/license-tool/wizard.go` - replace linear form shell with TUI entrypoint.
- `cmd/license-tool/commands.go` - wire persisted include settings and init-specific flags.
- `cmd/license-tool/commands_test.go` - CLI/init behavior tests.
- `internal/config/config.go` - add persisted `include` schema and merge/render support.
- `internal/config/config_test.go` - config schema and layering tests.
- `internal/initwizard/` - new wizard state, sample selection, preview rendering, and TUI model.
- `internal/render/` - reuse existing header rendering for previews; no direct changes expected.
- `README.md`, `DEVELOPERS.md` - document the interactive wizard and include/exclude config.

## User Flow
1. Choose project model: open source, private/internal, existing project detection, or advanced/manual.
2. Choose target license path: common SPDX, full searchable SPDX renderable list, or private/internal.
3. Enter copyright holder.
4. Choose year policy.
5. Choose header style with plain-language descriptions.
6. Choose whether to manage `LICENSE` and `LICENSES/<id>.txt`.
7. Configure file coverage: detected defaults, selected extensions, or advanced glob lists.
8. Review generated config, preview header on an example file, and confirm write.

## Preview Requirements
- Default panel is an example source file with the generated header applied.
- Secondary panels show `.license-tool.yaml`, license-file behavior, and a policy/coverage summary.
- Include examples for JavaScript/TypeScript, Python, Java, Go, C, C++, C#, PHP, Ruby, Swift, Kotlin, Rust, Shell, PowerShell, and R.
- If none of those languages are detected, default to C.

## Steps
1. [ ] Add persisted config `include` support with TDD.
2. [ ] Add init answers and preview model capable of rendering config and sample headers.
3. [ ] Add language sample catalog and deterministic detection/fallback.
4. [ ] Implement Bubble Tea wizard model with review flow.
5. [ ] Wire interactive `init` to the new TUI while preserving non-TTY behavior.
6. [ ] Update docs and issue status.
7. [ ] Run full local quality and 100% coverage gates.

## Open Questions
- How complete should the private/internal license path be in this first slice if no proprietary license renderer exists yet?
