---
title: Built-in tools
description: Exact built-in tool modes, execution behavior, and limits.
---

Axon attaches seven built-ins unless names are listed in `Config.ExcludeBuiltins`.

## `read`

Reads one file or lists a directory.

**Input:** `path` required; `mode`, `offset`, `limit` optional.

- `slice` (default): 1-based `offset`, default 1; default line count is `tools.read.lines`.
- `full`: reads the whole file only when file size is at most `tools.read.max_bytes`.
- directory path: returns directory entries; mode is ignored.

Slice output prefixes line numbers and truncates an individual displayed line after 8,192 characters. Full output prefixes line numbers and an approximate token-count header.

The tool samples the first 8 KiB and refuses likely binary files based on NUL/control-byte/UTF-8 heuristics.

## `write`

**Required:** `path`, `mode`, `content`.

Modes:

| Mode | Behavior |
| --- | --- |
| `save` | create parent directories and set the entire file contents |
| `replace_string` | replace exactly one exact `old_str`; zero or multiple matches fail |
| `replace_lines` | replace inclusive 1-based line range; end is clamped to EOF |
| `insert_at_line` | insert before a 1-based line position |

Every successful path records pre-edit content for undo. Writes use same-directory temp file + rename, preserve an existing destination's permission bits, and create new files as `0644` before umask effects.

:::note
The executable write path does **not** run a formatter after the write.
:::

## `exec`

Runs `sh -lc <command>`.

Fields include `command`, `tail_lines`, `expected_outcome`, `dir`, `timeout_seconds`, and `run_in_background`.

Foreground behavior:

- `command` required;
- `tail_lines > 0` enforced by runtime;
- timeout defaults to `tools.exec.timeout` and is capped by `max_timeout`;
- stdout/stderr share one byte-capped buffer;
- stdin is `/dev/null`;
- process runs in its own process group;
- timeout/cancel sends SIGKILL to the group;
- formatted result reports command, directory, expected string, exit code, truncation, and captured tail.

`expected_outcome` is displayed in the result; Axon does not itself judge whether the output met that expectation.

Background behavior starts the process immediately and returns a `bash_N` shell ID.

## `bash_output`

Reads only bytes appended to a background shell log since the previous call.

- optional `tail_lines` trims the new delta;
- optional `max_bytes` caps the new delta, defaulting to `tools.bash_output.max_bytes`;
- when backlog exceeds the byte cap, the newest bytes are kept and the read offset advances past dropped bytes;
- log truncation or file replacement resets reading to the beginning of the new file.

## `kill_shell`

Sends SIGTERM to the background process group, waits `tools.exec.kill_grace`, then sends SIGKILL when needed.

## `search`

Runs `rg` (ripgrep), so `rg` must exist in `PATH`.

**Required:** `query`.

- `mode`: `literal` (default) or `regex`;
- `path`: defaults to `.`;
- `globs`: passed as ripgrep `-g` filters;
- case-insensitive by default;
- hidden files are included;
- `.git` is excluded;
- output and runtime are capped by settings.

`max_matches` is implemented with ripgrep `--max-count`, which means the cap applies **per searched file**, even though the field name can read like a global match count.

Ripgrep exit code 1 (no matches) is treated as a successful empty result.

## `task`

Actions:

- `register`: store goal + non-empty steps;
- `advance`: mark current step done and move forward;
- `replan`: replace steps and reset current index to zero; optional non-empty goal replaces the existing goal.

The tool recommends 2–4 short steps but does not enforce a maximum. More than four produces a warning string.
