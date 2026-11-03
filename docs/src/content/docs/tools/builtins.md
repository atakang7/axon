---
title: Built-in Tools
description: Model-visible tools registered by Agent.New.
---

`New` registers nine built-in tools unless their names appear in `Config.ExcludeBuiltins`.

| Tool | Arguments | Action |
|------|-----------|--------|
| `read_file` | `path`, optional `offset` and `limit` | Read a file or directory |
| `edit` | `path`, `old_string`, `new_string` | Replace text in a file already read |
| `write_file` | `path`, `content` | Create or replace a file |
| `bash` | `command`, optional `timeout` | Run a shell command |
| `bash_output` | `shell_id` | Poll a background shell |
| `kill_shell` | `shell_id` | Stop a background shell |
| `grep` | `pattern`, optional `path` and `glob` | Search file contents |
| `glob` | `pattern`, optional `path` | Find matching file names |
| `todo_write` | `todos` | Record plan items and their status |

## Limits

- `edit` and replacement via `write_file` require the file to be read first.
- `bash` defaults to a 2-minute timeout; `tools.exec.max_timeout` caps requests at 10 minutes.
- Background shells are terminated by `agent.Close()`.
