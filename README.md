`agent-wrap` keeps the active project, path, and Git branch visible above a coding agent's terminal. It follows Claude Code, Pi, Codex, and OpenCode activity as the agent moves between projects.

# What It Does

- **Follow working context:** Update a three-row header from reported directories and file targets.
- **Recognize projects:** Share project colors across SSH and HTTPS origins, and distinguish branches, subtrees, and worktrees.
- **Retain a visual trail:** Keep visited project colors visible and rewind the trail when returning to an earlier project.
- **Own the terminal lifecycle:** Pass child arguments unchanged, preserve child exit status, restore terminal modes, and remove temporary watcher state.

# How It Works

- Put the agent executable and its arguments after `--`.
- Invoke it without a child, including a bare `--` or wrapper flags alone, to read help.
- Read the header as agent label, project/path, and branch/worktree. It starts at the launch directory while waiting for activity.
- Let the agent work normally. File targets and reported work directories update the header; Git lookup errors preserve the last valid context and go to the wrapper log.
- Exit the agent normally or cancel the wrapper with SIGINT, SIGTERM, or SIGHUP. The wrapper stops the owned session, joins observation workers, and cleans up its state.

# How it Really Works

- The wrapper reads harness session storage and local Git metadata. It does not contact a remote Git server or start a telemetry server. The launched agent keeps its own network behavior.
- Projects with the same normalized origin share colors. A known default branch at the primary root uses a single project color; branches, worktrees, and subtrees use a distinct context color. Use `--default-branch` when local `origin/HEAD` is unavailable.
- Claude Code uses its PID descriptor. Pi, Codex, and OpenCode associate sessions using directory and start-time heuristics; concurrent sessions in the same directory can be ambiguous. Only processes in the owned child session are eligible.
- Each run writes watcher state under `${XDG_CACHE_HOME:-the OS cache}/agent-wrap/session-*` and removes it on shutdown. Startup prunes unlocked cache directories older than 14 days.
- Default logs live under `${XDG_STATE_HOME:-~/.local/state}/agent-wrap/session-*.log`. They rotate at 10 MiB into five compressed numbered archives and expire after 14 days. Live session logs are protected by ownership locks. `--log-file` skips cross-session log pruning.
- Help, skill text, version, and completion scripts use stdout. Errors print once on stderr; working-context diagnostics stay in the log. `AGENT=1`, `true`, or `yes` selects compact help and `ERR:` diagnostics; human errors use `[ERROR]`.
- A normal child exit preserves its code. A signal-terminated child returns `128 + signal number`; wrapper failures return 1. The terminal remains interactive in either output mode. Header colors follow the terminal's color profile; a nonempty `NO_COLOR` disables them.

# Prerequisites

[Git](https://git-scm.com/) on PATH and an interactive Linux or macOS terminal are required. Install the agent you want to wrap; interpreter launches and aliases use `--harness`.

# Installation

The local executable is `bin/agent-wrap`. This checkout has no published Rust release artifact.

# Quick Start

```sh
agent-wrap -- pi
agent-wrap -- codex
agent-wrap -- claude
agent-wrap -- opencode
agent-wrap --harness pi -- node ./pi.mjs
agent-wrap --default-branch trunk --name reviewer -- claude
AGENT=1 agent-wrap skill
```

The header sits above the agent's terminal. For a project named `first` on its default branch `trunk`, its text is:

```text
Pi · waiting for agent activity
first · ~/projects/first
trunk
```

# Options & Flags

| Flag | Short | Default | Description |
| :--- | :--- | :--- | :--- |
| `--harness <name>` | — | `""` | Infer from executable; override with claude-code, pi, codex, or opencode. |
| `--dir <path>` | — | `""` | Launch in the current directory unless supplied. |
| `--default-branch <branch>` | — | `""` | Use local origin/HEAD unless unavailable. |
| `--name <label>` | — | `""` | Use the detected agent's display name unless supplied. |
| `--log-file <path>` | — | `""` | Use a unique session log unless supplied. |
| `--scan-interval <duration>` | — | `2s` | Go duration syntax, including Unicode microseconds; nonpositive values select 2s. |
| `--help` | `-h` | `false` | Print help. |
| `--version` | `-v` | `false` | Print the raw version and newline. |

`agent-wrap completion bash|zsh|fish|powershell` prints shell completion. Each shell command accepts `--no-descriptions` (default `false`). `agent-wrap help [command]` prints command help; `agent-wrap skill` prints the complete embedded reference.

Harness storage follows `CLAUDE_SESSIONS_DIR`, `CLAUDE_PROJECTS_DIR`, `PI_SESSIONS_DIR`, `PI_CODING_AGENT_DIR`, `CODEX_SESSIONS_DIR`, `CODEX_HOME`, `OPENCODE_DB_PATH`, and `XDG_DATA_HOME`. The skill describes precedence and runtime files.

# License

[MIT](LICENSE), copyright 2026 Alex Gorbatchev.
