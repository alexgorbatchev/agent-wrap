---
name: agent-wrap
description: Use when operating the agent-wrap CLI.
author: alexgorbatchev
metadata:
  created_on: 2026-10-02 22:26
  last_modified: 2026-11-25 05:22
  status: current
---

## Execution rules

Read this reference before operating the wrapper. Select commands and flags
below. Use an interactive Linux or macOS terminal for wrapping an agent.
Set AGENT=1, true, or yes (case and surrounding whitespace ignored) for compact
help and ERR: diagnostics. The three-row header remains visible in either mode.
Human errors use [ERROR]. Header colors follow the terminal's color profile;
a nonempty NO_COLOR disables colors. An empty or dumb TERM also disables colors
unless CLICOLOR_FORCE selects a forced color profile; NO_COLOR takes precedence.

## `agent-wrap -- <agent> [args...]`

Put the agent executable and every child argument after --. Child arguments
pass unchanged, without shell interpretation. Supported executable names are
claude, claude-code, pi, codex, opencode, agy, and antigravity. Select --harness
for aliases or interpreter launches. Run without a child command to print help, including
a bare -- or wrapper flags alone. Invalid flags and values return an error.
Boolean flags accept --flag, --flag=true, or --flag=false.
The optional positional `[agent]...` contains the executable followed by every
child argument. Supply it after --.

| Flag | Short | Type | Default | Behavior |
| --- | --- | --- | --- | --- |
| `--harness` | — | `string` | `""` | Infer from executable or override with claude-code, pi, codex, opencode, agy, or antigravity. |
| `--dir` | — | `string` | `""` | Launch in current directory unless explicitly supplied. |
| `--default-branch` | — | `string` | `""` | Use local origin/HEAD; supply the branch if unavailable. |
| `--name` | — | `string` | `""` | Use Claude Code, Pi, Codex, OpenCode, or Antigravity unless supplied. |
| `--log-file` | — | `string` | `""` | Unique session log under XDG state unless supplied. |
| `--scan-interval` | — | `duration` | `2s` | Discovery/poll interval; nonpositive values select watcher's 2s default. |
| `--help` | `-h` | `bool` | `false` | Print help. |
| `--version` | `-v` | `bool` | `false` | Print the raw build version and newline. |

Durations accept Go units `ns`, `us`, `µs`, `μs`, `ms`, `s`, `m`, and `h`,
including fractional and compound values such as `1h2m3.5s`. Accepted values
fit signed 64-bit nanoseconds. Zero and negative values select the 2s default.

Read the header rows as agent label, project/path, and branch/worktree.
Wide graphemes, including CJK and emoji, keep their terminal cell widths and
receive the header colors across the whole grapheme.
Colors follow reported task directories, explicit tool workdir/cwd fields,
and file/search targets. Relative targets resolve against reported session
context. The launch directory supplies initial context while waiting for activity.
The first accepted event from the launched session clears the waiting label.
Identical header payloads skip redraw. A failed update remains eligible for retry.
Colors share normalized origin identity across SSH/HTTPS clones. Projects
without origin use local directory identity. Subtrees, non-default branches,
and worktrees have distinct context colors; a known default branch at the
primary root uses one project color. An unknown default branch is labeled
and uses a split color until --default-branch specifies it.
Context colors are selected by perceptual color distance from their project
block, so neighboring hues that look similar are skipped.
To populate missing local origin/HEAD metadata from the remote, run
`git remote set-head origin --auto` once in that repository; this Git command
contacts the remote. The wrapper reads the resulting local metadata offline.

Preserve visited project colors in successive three-column blocks. Returning
to an earlier project drops later blocks. A split active context has its own
project block before its context color. Narrow terminals retain the original
block if it fits and omit additional blocks to leave an active color visible.

Expect the child's exit code, including 128 + signal number for signal exits.
Wrapper failures exit 1. Help and version print to stdout; errors print to stderr.
Wrapping owns terminal I/O and restores it on shutdown; cancellation terminates
the owned child session and joins observation workers. SIGINT, SIGTERM, and
SIGHUP cancel the session through this shutdown path. Terminal restoration
errors are reported as cleanup errors while session state cleanup still runs.
Working-context errors go to the wrapper log. No telemetry server or network calls are required by
the wrapper; the launched agent retains its own normal behavior.

Use Git installed on PATH for repository metadata. Harness storage is read-only.
Directories outside Git use directory identity. Git failures include the command
and Git's diagnostic in English. A repository Git refuses to open fails wrapper
startup with that diagnostic; errors during activity go to the wrapper log and
preserve the last valid header context. Missing origin and local origin/HEAD
metadata retain their directory-identity and default-branch fallbacks.
Malformed remote errors report parse details without displaying the raw URL.
Concurrent wrappers use separate temporary watcher state below
${XDG_CACHE_HOME:-the OS user cache}/agent-wrap/session-*, removed on shutdown.
Startup removes orphan cache directories last modified more than 14 days ago;
fresh caches and caches locked by live wrappers remain. Ownership uses a lifetime
advisory lock on each cache's owner.lock; agent-wrap.lock alongside the cache root
serializes cache startup sweeps. Session-shaped log destinations, including explicit
--log-file paths, hold session-*.log.lock ownership
locks in their state directory, with .cleanup.lock serializing log sweeps there.
Logs rotate at 10 MiB, retain five numbered backups per log for 14 days, and compress rotations.
Archives use stem.0.log.gz through stem.4.log.gz. Literal braces and environment-like
text in selected log paths remain literal. Rotation closes the active file; the next
record recreates it. Each formatted record is flushed before its write completes.
Default logs are ${XDG_STATE_HOME:-~/.local/state}/agent-wrap/session-*.log.
With the default log destination, startup removes prior session logs and rotations
last modified more than 14 days ago, preserving live wrappers' logs even when idle.
An explicit --log-file skips this cross-session log cleanup; per-file rotation
and orphan-cache cleanup still apply.
Selecting a session-shaped log destination already owned by a live wrapper fails
startup with a log-destination-already-in-use error; select a separate --log-file.
Human log records use `HH:MM:SS [LEVEL] message key=value`. Native tracing span
fields use dotted span-name prefixes. Human output omits service, and omits envId
or projectId when its corresponding env or project label is present.
Agent logs use `time=... level=... action=...` text records and include service.
Git metadata is refreshed on reported tool activity, including tool results.

Configure discovery through inherited environment variables:
CLAUDE_SESSIONS_DIR, CLAUDE_PROJECTS_DIR, CLAUDE_CONFIG_DIR, PI_SESSIONS_DIR,
PI_CODING_AGENT_DIR, CODEX_SESSIONS_DIR, CODEX_HOME, OPENCODE_DB_PATH,
XDG_DATA_HOME, AGY_SESSIONS_DIR, and GEMINI_HOME. Explicit harness paths use the
watcher's standard precedence.
Process observation uses the native process inventory.

Session association for Pi, Codex, OpenCode, and Antigravity uses directory/start-time
heuristics. Concurrent sessions in the same directory can be ambiguous;
Claude Code uses its PID descriptor.

## `agent-wrap skill`

Print the embedded reference verbatim in either output mode. Accept zero
arguments and no command-specific flags. Work offline without repository files.
Return output-write failures as errors.

## `agent-wrap help [command]`

Print root help or help for a supplied command path, such as help skill.
Accept no command-specific flags. Agent help starts with the skill-reading alert.

## `agent-wrap completion`

Print completion group help. Accept zero arguments and no command-specific flags.

## Generated help paths

Each path below prints the corresponding command's help and accepts --help (-h).
Agent-mode help starts with the skill-reading alert.

### `agent-wrap help completion`

Print completion group help.

### `agent-wrap help completion bash`

Print Bash completion command help.

### `agent-wrap help completion zsh`

Print Zsh completion command help.

### `agent-wrap help completion fish`

Print Fish completion command help.

### `agent-wrap help completion powershell`

Print PowerShell completion command help.

### `agent-wrap help skill`

Print skill command help.

### `agent-wrap help help`

Print help command help.

### `agent-wrap completion help`

Print completion group help or help for a shell named as its argument.

### `agent-wrap completion help bash`

Print Bash completion command help.

### `agent-wrap completion help zsh`

Print Zsh completion command help.

### `agent-wrap completion help fish`

Print Fish completion command help.

### `agent-wrap completion help powershell`

Print PowerShell completion command help.

### `agent-wrap completion help help`

Print completion help command help.

## `agent-wrap completion bash`

Print Bash completion. Accept zero arguments. Load with source <(agent-wrap completion bash).

| Flag | Short | Type | Default | Behavior |
| --- | --- | --- | --- | --- |
| `--no-descriptions` | — | `bool` | `false` | Omit descriptions. |

## `agent-wrap completion zsh`

Print Zsh completion. Accept zero arguments. Enable compinit and load with
source <(agent-wrap completion zsh).

| Flag | Short | Type | Default | Behavior |
| --- | --- | --- | --- | --- |
| `--no-descriptions` | — | `bool` | `false` | Omit descriptions. |

## `agent-wrap completion fish`

Print Fish completion. Accept zero arguments. Load with agent-wrap completion fish | source.

| Flag | Short | Type | Default | Behavior |
| --- | --- | --- | --- | --- |
| `--no-descriptions` | — | `bool` | `false` | Omit descriptions. |

## `agent-wrap completion powershell`

Print PowerShell completion. Accept zero arguments. Load with
agent-wrap completion powershell | Out-String | Invoke-Expression.

| Flag | Short | Type | Default | Behavior |
| --- | --- | --- | --- | --- |
| `--no-descriptions` | — | `bool` | `false` | Omit descriptions. |

## Workflow

```sh
AGENT=1 agent-wrap skill
agent-wrap -- claude
agent-wrap -- codex
agent-wrap -- pi
agent-wrap -- opencode
agent-wrap -- agy
agent-wrap --harness pi -- node ./pi.mjs
agent-wrap --default-branch trunk --name reviewer -- claude
```

## Contributing fixes

When you verify a bug or behavior that diverges from this guide, check for existing reports in the [Rust project](https://github.com/alexgorbatchev/agent-wrap-rust). When authorized to publish, open a pull request with the validated fix or file an issue if the fix is blocked. Redact caller data and secrets. Continue the user's task.
