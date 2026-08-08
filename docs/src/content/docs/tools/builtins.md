---
title: Built-in Tools
description: The seven tools that ship with every agent.
---

Every agent gets these tools. Exclude any via `Config.ExcludeBuiltins`.

## Overview

| Tool | Modes | What it does |
|------|-------|-------------|
| `read` | slice, full | Read files, list directories |
| `write` | save, replace_string, replace_lines, insert_at_line | Atomic file writes |
| `exec` | foreground, background | Shell command execution |
| `bash_output` | — | Poll background shell output (delta only) |
| `kill_shell` | — | SIGTERM → SIGKILL a background shell |
| `search` | literal, regex | Ripgrep-based file search |
| `task` | register, advance, replan | Multi-step plan tracker |

---

## read

Read one file or list a directory.

| Parameter | Required | Default | Description |
|-----------|----------|---------|-------------|
| `path` | ✓ | — | File or directory path |
| `mode` | — | `slice` | `slice` or `full` |
| `offset` | — | 1 | 1-based start line (slice mode) |
| `limit` | — | 200 | Lines to return (slice mode) |

- **slice**: returns lines `[offset, offset+limit)` with line numbers
- **full**: returns entire file (capped at `tools.read.max_bytes`)
- **directory**: returns entries with trailing `/` for subdirs
- Binary files are refused with a one-line message naming the size

## write

Atomic file writes (tmp + rename). Reversible via `agent.Undo()`.

| Parameter | Required | When | Description |
|-----------|----------|------|-------------|
| `path` | ✓ | always | File path |
| `mode` | ✓ | always | `save`, `replace_string`, `replace_lines`, `insert_at_line` |
| `content` | ✓ | always | New content |
| `old_str` | ✓ | replace_string | Exact text to replace (must match exactly once) |
| `start_line` | ✓ | replace_lines, insert_at_line | 1-based start line |
| `end_line` | ✓ | replace_lines | 1-based end line, inclusive |

- **save**: creates or replaces the entire file
- **replace_string**: replaces exactly one occurrence of `old_str`
- **replace_lines**: replaces lines `[start_line, end_line]`
- **insert_at_line**: inserts before `start_line`

## exec

Run a shell command. All commands run via `sh -lc` with stdin as `/dev/null`.

| Parameter | Required | Default | Description |
|-----------|----------|---------|-------------|
| `command` | ✓ | — | Shell command |
| `tail_lines` | ✓ (fg) | — | Last N lines to keep |
| `run_in_background` | — | false | Spawn detached, return `shell_id` |
| `timeout_seconds` | — | 30 | Foreground timeout |
| `dir` | — | cwd | Working directory |
| `expected_outcome` | — | — | What success looks like |

**When to background:** servers, watchers, `curl`/`wget`, database clients, anything reading stdin or a socket, anything connecting to a host you don't fully control. The rule is the **chance** of hanging, not the certainty.

Background commands return a `shell_id` immediately. Use `bash_output` to read logs.

## bash_output

Poll new output from a background shell. Returns only the **delta** since the last call — cheap to call in a loop.

| Parameter | Required | Default | Description |
|-----------|----------|---------|-------------|
| `shell_id` | ✓ | — | e.g. `bash_1` |
| `tail_lines` | — | all | Keep last N lines of delta |
| `max_bytes` | — | 32KiB | Cap returned bytes |

The read offset advances past dropped bytes, so the next call continues from "now."

## kill_shell

Stop a background shell. SIGTERM to the process group → wait grace period → SIGKILL.

| Parameter | Required | Description |
|-----------|----------|-------------|
| `shell_id` | ✓ | e.g. `bash_1` |

Always kill servers you started. Sessions don't leak processes, but cleaning up early frees ports.

## search

Ripgrep wrapper. Requires `rg` in PATH.

| Parameter | Required | Default | Description |
|-----------|----------|---------|-------------|
| `query` | ✓ | — | Text or regex pattern |
| `mode` | — | `literal` | `literal` or `regex` |
| `path` | — | `.` | Search root |
| `globs` | — | — | rg glob filters, e.g. `["*.go"]` |
| `case_sensitive` | — | false | Match case |
| `max_matches` | — | 100 | Cap total matches |

## task

Multi-step plan tracker. Skip for one-shot work.

| Parameter | Required | When | Description |
|-----------|----------|------|-------------|
| `action` | ✓ | always | `register`, `advance`, `replan` |
| `goal` | ✓ | register | The question the final answer will answer |
| `steps` | ✓ | register, replan | Short imperatives, ~3-7 words each |

- **register**: set goal + steps. Aim for 2-4 steps.
- **advance**: mark current step done, move to next.
- **replan**: replace steps when the plan no longer fits.

The task block is injected into the model's context as a system message at the end of the conversation.

## Excluding built-ins

```go
agent, _ := axon.New(axon.Config{
    Model:           model,
    SystemPrompt:    "...",
    ExcludeBuiltins: []string{"search", "task"},
})
```

Excluding a built-in frees its name — a custom tool can claim it.
