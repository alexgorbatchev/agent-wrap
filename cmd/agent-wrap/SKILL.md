---
name: agent-wrap
description: Use when operating the agent-wrap CLI.
author: alexgorbatchev
metadata:
  created_on: 2026-10-02 22:26
  last_modified: 2026-10-05 11:20
  status: current
---

## Execution rules

Read this reference before operating the wrapper. Select commands and flags
below. Use an interactive Linux or macOS terminal for wrapping an agent.
Set AGENT=1, true, or yes (case and surrounding whitespace ignored) for compact
help and ERR: diagnostics. The three-row colored header remains visible in
either mode. Human errors use [ERROR].

## `agent-wrap -- <agent> [args...]`

Put the agent executable and every child argument after --. Child arguments
pass unchanged, without shell interpretation. Supported executable names are
claude, claude-code, pi, codex, and opencode; select --harness for aliases or
interpreter launch commands. Run without arguments to print help.

| Flag | Short | Type | Default | Behavior |
| --- | --- | --- | --- | --- |
| `--harness` | — | `string` | `""` | Infer from executable; override with claude-code, pi, codex, opencode. |
| `--dir` | — | `string` | `""` | Launch in current directory unless explicitly supplied. |
| `--default-branch` | — | `string` | `""` | Use local origin/HEAD; supply the branch if unavailable. |
| `--name` | — | `string` | `""` | Use Claude Code, Pi, Codex, or OpenCode unless supplied. |
| `--log-file` | — | `string` | `""` | Unique session log under XDG state unless supplied. |
| `--scan-interval` | — | `duration` | `2s` | Discovery/poll interval; nonpositive values select watcher's 2s default. |
| `--help` | `-h` | `bool` | `false` | Print help. |
| `--version` | `-v` | `bool` | `false` | Print the raw build version and newline. |

Read the header rows as agent label, project/path, and branch/worktree.
Colors follow reported task directories, explicit tool workdir/cwd fields,
and file/search targets. Relative targets resolve against reported session
context. The launch directory supplies initial context while waiting for activity.
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
the owned child session and joins observation workers. Working-context errors
go to the wrapper log. No telemetry server or network calls are required by
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
Logs rotate at 10 MB, retain five backups for 14 days, and compress rotations.
Default logs are ${XDG_STATE_HOME:-~/.local/state}/agent-wrap/session-*.log.
Human log records use `HH:MM:SS [LEVEL] message key=value`; zero timestamps
are omitted. Attribute groups expand into dotted keys, attributes retain the
group active when added, and empty attributes/groups are omitted. The human
format hides the service field and preserves other fields, including ID fields.
Agent logs use slog text records with `action` for the message and include service.
Git metadata is refreshed on reported tool activity, including tool results.

Configure discovery through inherited environment variables:
CLAUDE_SESSIONS_DIR, CLAUDE_PROJECTS_DIR, PI_SESSIONS_DIR,
PI_CODING_AGENT_DIR, CODEX_SESSIONS_DIR, CODEX_HOME, OPENCODE_DB_PATH,
and XDG_DATA_HOME. Explicit harness paths use the watcher's standard precedence.
The AGENT_STATUS_TEST_MOCK_PROC variable is reserved by the watcher for tests.

Session association for Pi, Codex, and OpenCode uses directory/start-time
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
agent-wrap --harness pi -- node ./pi.mjs
agent-wrap --default-branch trunk --name reviewer -- claude
```
