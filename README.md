`agent-wrap` gives each AI coding agent a three-row terminal header so you can recognize its project, branch, and working directory at a glance. It wraps Claude Code, Pi, Codex, and OpenCode independently of the agent-status services.

# What It Does

- **Project colors:** Give clones of the same Git origin the same deterministic bright background.
- **Context colors:** Split the header for a subtree, worktree, or non-default branch, keeping a 3×3 project-color block on the left.
- **Project history:** Keep earlier projects as 3×3 blocks when an agent moves across repositories; returning to a project removes later blocks.
- **Terminal behavior:** Pass arguments and terminal input to the agent and preserve its exit code.

# How It Works

- Start an agent with `agent-wrap -- <agent> [args...]`.
- Read the three header rows as agent name, project/path, and branch/worktree.
- Reported task directories and tool targets update the header as the agent works.
- A known default branch at the primary repository root fills the header with one project color. Other contexts use a distinct bright color after the project block.
- Moving A → B → C leaves A and B as blocks and fills the remainder with C. Returning to A reuses its color and clears B and C.

# How it Really Works

- Normalize the origin URL so SSH and HTTPS clones share an identity. Repositories without an origin use their local directory; directories outside Git use their own path.
- Read local Git metadata on tool activity without contacting a remote. Use `--default-branch` if the repository lacks a local `origin/HEAD`; the header labels an unknown default branch and uses a context color.
- Observe harness storage read-only and restrict events to the launched process session. Explicit `workdir`/`cwd` and file/search targets determine the working context; relative targets use the reported session directory.
- Pi, Codex, and OpenCode session matching uses the watcher's directory/start-time heuristics. Concurrent sessions in the same directory can be ambiguous; Claude Code has a PID descriptor.
- Create separate watcher state under `${XDG_CACHE_HOME:-the OS user cache}/agent-wrap/session-*` and remove it on shutdown. Startup removes orphan cache directories last modified more than 14 days ago, preserving fresh caches and caches owned by live wrappers.
- Keep logs under `${XDG_STATE_HOME:-~/.local/state}/agent-wrap/`, with 10 MB rotation, five backups per log, 14-day backup retention, and compression. Startup with the default log destination removes prior session logs and rotations last modified more than 14 days ago; live wrappers' logs remain, even when idle or explicitly selected with `--log-file`. An explicit `--log-file` skips this cross-session log cleanup.
- Use `--log-file` to choose a log destination. Context-resolution errors appear in that log while the header keeps its last valid context.
- Reserve exactly three rows while keeping the agent interactive. Narrow terminals preserve the original project block when it fits and omit further blocks to leave room for the active color.
- Return the agent's exit code; signal exits use `128 + signal number`. Wrapper errors return 1 and print to stderr. Help, version, and the usage reference print to stdout. `AGENT=1` selects compact help and diagnostics.

# Installation

This utility is under development; this repository has no published releases. The local development binary is `bin/agent-wrap`. Development setup is documented in [AGENTS.md](AGENTS.md).

# Setup

- Use an interactive Linux or macOS terminal with an inactive alternate screen when starting the wrapper.
- Install [Git](https://git-scm.com/) on `PATH` to identify repositories, and install the agent you want to launch.
- Inherit the agent's normal storage settings. Discovery supports `CLAUDE_SESSIONS_DIR`, `CLAUDE_PROJECTS_DIR`, `PI_SESSIONS_DIR`, `PI_CODING_AGENT_DIR`, `CODEX_SESSIONS_DIR`, `CODEX_HOME`, `OPENCODE_DB_PATH`, and `XDG_DATA_HOME`.

# Quick Start

```sh
agent-wrap -- claude
agent-wrap -- codex
agent-wrap -- pi
agent-wrap -- opencode
agent-wrap --name reviewer --default-branch trunk -- claude
AGENT=1 agent-wrap skill
```

Sample Output (header text; background colors depend on the project):

```text
Codex
agent-status · /home/alex/development/projects/agent-status
main
```

# Options & Flags

| Flag | Short | Default | Description |
| :--- | :--- | :--- | :--- |
| `--harness <name>` | — | `""` | Infer from executable; override with `claude-code`, `pi`, `codex`, or `opencode`. |
| `--dir <path>` | — | `""` | Launch in the current directory unless supplied. |
| `--default-branch <branch>` | — | `""` | Read local `origin/HEAD`; supply the default branch when unavailable. |
| `--name <label>` | — | `""` | Use the detected agent name unless supplied. |
| `--log-file <path>` | — | `""` | Use a unique log under XDG state unless supplied. |
| `--scan-interval <duration>` | — | `2s` | Discover activity at this interval; nonpositive values select the watcher's default. |
| `--help` | `-h` | `false` | Print help. |
| `--version` | `-v` | `false` | Print the raw build version. |

# License

[MIT](LICENSE). Copyright (c) 2026 Alex Gorbatchev.
