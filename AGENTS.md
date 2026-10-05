# agent-wrap

Standalone terminal wrapper for the Claude Code, Pi, Codex, and OpenCode harnesses supported by `agent-watcher`.

## Requirements

- Use `github.com/alexgorbatchev/go-tui-frame` directly; reserve exactly three header rows showing agent name, project/path, and branch/worktree.
- Follow the agent's reported tool/task directories; operate independently of the agent-status daemons. Process directories serve only session discovery and launch context.
- Calculate deterministic bright project colors from normalized Git remote identity so clones on different machines match. Use directory identity when no remote exists.
- Give subdirectories, non-default branches, and worktrees distinct bright subcolors. The repository's default branch at its primary root has a solid project background.
- Keep original project colors as successive 3×3 blocks when moving A → B → C, with the active context filling the remainder. Returning to an earlier project reuses its color and removes the later history blocks.
- Maintain this utility as its own project with module `github.com/alexgorbatchev/agent-wrap`; keep logging and test tooling inside this project and require no agent-status source checkout.
- Publish the source to the public GitHub repository `alexgorbatchev/agent-wrap` with a description and repository topics; keep `origin` pointed at that repository.

## Commands

- Development prerequisites: Go 1.27.1, Zig 0.16.0, Git, `just`, `pkg-config`, `curl`, `tar`, and `shasum`. The native recipe builds the exact Ghostty source and checksum required by the pinned `go-tui-frame` module under the main checkout's `.tmp/native/`; Linux builds use Zig's musl target and static linkage.
- Lint requires `golangci-lint` built with Go 1.27 or newer (verified with v2.14.0); an older build cannot analyze the workspace toolchain.
- Always reuse the main checkout's `.tmp/native/` for static dependencies, including Ghostty sources, archives, installation prefixes, and Zig caches, from every linked worktree. Resolve the main checkout through Git worktree metadata rather than the current branch name.
- Go build caches and temporary files stay in each checkout's `.tmp/`; binaries stay in each checkout's `bin/`.
- The project owns a Go 1.27.1 workspace because the frame requires it; run commands with `just` at the project root.
- Supported runtime platforms: Linux and macOS; builds and PTY integration tests are verified on the host platform. Windows is unsupported by the terminal-frame dependency.

- Build: `just build` (binary in `bin/agent-wrap`)
- Run: `just run -- claude` or `just run-ai -- codex`
- Test: `just test` (race detector and >=90% statement coverage)
- Lint: `just lint`
- Native Git isolation regression: `bash scripts/check-native-isolation.sh` (real tagged, untagged, inherited Git environment, and linked-worktree builds in an independent scratch clone under `.tmp/`).

## Boundaries

- Always: update the embedded `cmd/agent-wrap/SKILL.md` in the same change as commands, flags, defaults, environment variables, output, or side effects; maintain interface drift tests.
- Always: change behavioral tests with runtime changes and verify red/green, including temporarily disabling the implementation; require >=90% statement coverage in every Go package (scripts are excluded).
- Always: isolate each wrapper's watcher state and restrict events to its child session; join workers on shutdown.
- Always: use the project's internal structured logging to files while the frame owns terminal I/O.
- Always: record new user instructions here; check with the user if they conflict.
- Never: execute reported shell commands, inspect harness authentication files, transmit telemetry, or mutate harness logs/databases.
- Never: publish releases automatically without explicit authorization.
