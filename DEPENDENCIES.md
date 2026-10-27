---
created_on: 2026-10-14 10:04
last_modified: 2026-10-14 11:11
status: current
---

# Native dependencies

The wrapper directly consumes sibling `agent-parser`, `agent-watcher`, `agent-common`, `cli-command-kit`, and `tui-frame` libraries. `tui-frame` owns the pinned Ghostty library and portable-pty fork; its `DEPENDENCIES.md` records native sources and patches. The wrapper consumes that same portable-pty package identity for unchanged native launch arguments and signal exit metadata.

Clap and clap_complete own argument validation and completion. Native palette implements CIEDE2000 color distance; Ratatui handles grapheme widths and cell styling. Git runs directly with a fixed English diagnostic locale. No reported tool command is evaluated. The maintained [`log4rs` 1.4.0](https://docs.rs/log4rs/1.4.0/log4rs/) rolling-file policies own size rotation, bounded archive counts and gzip compression.

Go duration syntax uses the native [`go-duration` 0.2.0](https://docs.rs/go-duration/0.2.0/go_duration/) `GoDuration` parser. Its checked signed nanoseconds, Unicode microsecond units, compound syntax, and bounded long-fraction parsing are exercised through Clap. The wrapper maps nonpositive values to the watcher's existing default-selection value. Bare signed zero is handled explicitly because the dependency requires units.

## log4rs literal path patch

`vendor/log4rs` preserves crates.io 1.4.0 source and upstream `LICENSE-MIT` and `LICENSE-APACHE`. The local patch changes exactly:

- `src/append/rolling_file/mod.rs`: adds `RollingFileAppenderBuilder::build_literal`, accepting exact native filesystem paths. The existing `build` keeps upstream environment expansion.
- `src/append/rolling_file/policy/compound/roll/fixed_window.rs`: adds `FixedWindowRollerBuilder::build_literal(prefix, suffix, count)` and a native path representation. Only the decimal index is inserted between exact path prefix and suffix; literal braces, `$ENV{...}` and non-UTF-8 bytes retain their meaning. Existing string-pattern construction retains upstream behavior. Compression failures propagate without upstream's extra stdout diagnostic.

The wrapper integrates these APIs directly and uses synchronous native rotation. Each formatted tracing record is flushed before append returns. The active file may be absent immediately after a record triggers rotation; the next record recreates it. Archives use `stem.0.log.gz` through `stem.4.log.gz`, rather than timestamp names. Expiry checks apply to those five owned paths and propagate filesystem failures.

Real filesystem regression coverage lives in `tests/files.rs`, including exact paths, gzip headers, count bounds, expiry, blocked destinations, cross-cache ownership and symlink locks. Native package tests also exercise unchanged upstream rotation behavior.
