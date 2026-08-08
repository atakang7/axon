---
title: Built-in tools
description: Exact model-facing modes and executable behavior for Axon's seven standard tools.
---

Default built-ins:

```text
read
write
exec
bash_output
kill_shell
search
task
```

They may be excluded by exact name in `Config.ExcludeBuiltins`.

## `read`

### Input

| Field | Required | Meaning |
| --- | --- | --- |
| `path` | yes | relative/absolute file or directory |
| `mode` | no | `slice` (default) or `full` |
| `offset` | no | 1-based start line for slice |
| `limit` | no | slice line count |

Directory paths return directories first (with `/` suffix), then files; mode is ignored.

Slice defaults offset to 1 and non-positive limit to configured `read.lines`. Each displayed line is capped at 8192 characters.

Full reads stat the file first and refuse when size exceeds configured `read.max_bytes`. Output includes line numbers and an approximate `len(data)/4` token hint.

Likely binary files are refused based on an 8 KiB sample using NUL/control-byte/UTF-8 heuristics.

## `write`

Required: `path`, `mode`, `content`.

| Mode | Additional fields | Behavior |
| --- | --- | --- |
| `save` | — | create parents; create/replace whole file |
| `replace_string` | `old_str` | require exactly one exact match |
| `replace_lines` | `start_line`, `end_line` | replace inclusive 1-based range; end clamps to EOF |
| `insert_at_line` | `start_line` | insert before 1-based position |

All mutation modes record pre-edit contents before the atomic write.

Atomic writes use a same-directory temp file + rename, preserve existing permission bits, and use 0644 for new destinations before umask effects.

**No formatter is executed by the current write path**, despite stale text that may appear in the built-in tool description.

## `exec`

### Input

| Field | Required by schema | Runtime behavior |
| --- | --- | --- |
| `command` | yes | must be non-blank |
| `tail_lines` | no in schema | **required > 0 for foreground execution** |
| `expected_outcome` | no | echoed into result; not evaluated by runtime |
| `dir` | no | working-directory override resolved through Workspace |
| `timeout_seconds` | no | defaulted/clamped by exec limits |
| `run_in_background` | no | when true, spawn and return handle immediately |

Foreground executes `sh -lc`, stdin `/dev/null`, combined stdout/stderr into one capped buffer, and process group enabled.

On timeout/cancel Axon SIGKILLs the group. If output-copy cleanup still does not finish within `kill_grace`, the result notes an escaped child may still hold the pipe.

## `bash_output`

Input: `shell_id` required, optional `tail_lines`, optional `max_bytes`.

Returns shell status and only log bytes appended since the previous poll.

If new data exceeds selected `max_bytes`, the newest bytes are returned and the read offset advances past dropped data. File truncation or replacement restarts from the beginning of the new filesystem object.

Positive per-call `max_bytes` is not clamped by the configured default.

## `kill_shell`

Input: `shell_id` required.

Sends SIGTERM to the process group, waits configured exec kill grace, then SIGKILLs if needed. Killing an already-finished shell succeeds.

## `search`

Input: `query` required.

| Field | Meaning |
| --- | --- |
| `mode` | `literal` default or `regex` |
| `path` | root, default `.` |
| `globs` | repeated ripgrep `-g` filters |
| `case_sensitive` | false by default → `--ignore-case` |
| `max_matches` | default from settings when non-positive |

Runs `rg -n --no-heading --color never -g !.git --hidden ...` from Workspace dir.

Literal mode adds `--fixed-strings`. `max_matches` becomes ripgrep `--max-count` and is therefore per file. Exit status 1 means “no matches,” not error.

Requires `rg` in `PATH`.

## `task`

Input: `action` required.

### `register`

Requires non-blank `goal` and at least one non-blank step after trimming. Stores a new task at current step 0.

### `advance`

Marks current step done and moves to the next. Returns either next description, `done — answer the user`, or `all steps already complete` depending on state.

### `replan`

Requires at least one non-blank replacement step. Non-empty goal replaces existing goal; empty goal preserves it. Current step resets to zero.

More than four steps emits a warning string but is not rejected.
