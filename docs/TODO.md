# TODO

## Shipped in v0.4.0 (2026-06-09)
- [x] #35 audit reporting overhaul + self-config exclusion + policy-aware group risk
- [x] #29 init full TUI wizard + live previews + persisted `include`
- [x] #31 resolve: stop guessing SPDX ids for ambiguous aliases
- [x] #34 remove no-op `--quiet`/`--verbose` flags
- [x] #33 refuse to clobber a symlinked LICENSE
- [x] #30 confine header detection to contiguous comment lines

## Follow-ups (not yet ticketed)
- [ ] Prune merged `feature/GH-*` branches (local + remote).
- [ ] Detect: de-stack multi-block `/* */` headers (deferred from #30; would need care to avoid over-detection).
- [ ] cmd/applier: replace `isWriteRefusal` substring matching with a typed sentinel error (deferred from #34 to avoid colliding with #33).
- [ ] model: remove the dead `ResolveOptions.Verbose` field (deferred from #34).
