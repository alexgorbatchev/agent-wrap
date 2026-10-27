---
created_on: 2026-10-14 10:04
last_modified: 2026-10-14 10:04
status: current
---

# agent-wrap

Interactive agent terminal with a three-row project header. Parent instructions in `../AGENTS.md` apply.

## Commands

- Build the local artifact: `just build` (`bin/agent-wrap`).
- Run: `just run -- pi`; compact diagnostics: `just run-ai -- pi`.
- Tests: `just test`; strict format/lint: `just lint`; coverage: `just coverage`.
- Documentation: `just doc`; complete checks: `just check`.
- Format only this package: `cargo fmt --package agent-wrap`.

## Native setup

- Keep sibling library checkouts named by `Cargo.toml`. The terminal dependency builds its checksum-pinned Ghostty source using `../tui-frame/scripts/native.sh`; consult `../tui-frame/AGENTS.md` for Zig, libclang, SDK, and build prerequisites.
- Git on PATH is required for metadata resolution and real subprocess tests. Tests use isolated fixtures under `.tmp/` and real PTYs; they do not require installed agent executables or user transcripts.
- Preserve upstream licenses and record local native dependency patches in `DEPENDENCIES.md`. Literal log paths use the vendored log4rs native path APIs; never reintroduce environment expansion or lossy path conversion.

## Runtime contracts

- Use Clap for all argument and value parsing. Keep executable and child arguments as native `OsString` values and pass them directly to the PTY command without shell interpretation.
- Keep the header exactly three rows. Use terminal cell widths, preserve the original color block on narrow screens, and rewind the visited-project trail when returning to an earlier project.
- Resolve task directories from typed parsed events and existing paths. Never execute shell text reported by an agent. Preserve the last valid context when a Git/path lookup fails, including the last successfully resolved per-session base.
- Observe only processes in the launched child's native session. Declare alias candidates from captured metadata without changing process names, command lines, or creation times. Keep candidate PID reuse validation in the shared watcher.
- Use a frame controller in observation callbacks. Cancel and join observation workers before dropping logging or temporary state; keep the console borrowed and let its native owner restore terminal modes.
- Keep wrapper tracing out of terminal stdout/stderr. Install the shared logging dispatch on all worker and blocking threads; return logging/cleanup failures through the one top-level error printer.
- Hold advisory ownership locks for the lifetime of temporary state and session-shaped logs. Never prune live owners, follow lock symlinks, or apply cross-session log pruning to an explicit log destination. Keep every watcher checkpoint in the owned session cache.

## Documentation and verification

- `skills/agent-wrap/SKILL.md` is the canonical operational guide and is embedded verbatim by `agent-wrap skill`, preserving this project's source contract. Update it in the same change as any command, argument, flag, default, environment, output, exit, or side-effect change; update `last_modified` and keep the live command-tree drift test passing.
- Any code change affecting runtime results requires a corresponding test-file change and at least 90% line coverage; `scripts/` is excluded. Use real processes, PTYs and filesystem fixtures for lifecycle and ownership behavior. Temporarily disable the implementation, verify the behavioral test fails, restore it, and rerun checks.
- Use the `termctrl` skill for interactive terminal smoke checks and keep evidence under `.tmp/`. Native macOS runtime behavior requires macOS execution; Linux tests and cross-compilation alone do not prove it.
- Keep standing rules here; keep progress and test-run history in `.tmp/` or conversation. Never publish releases, tags, packages, or production deployments without explicit authorization.
