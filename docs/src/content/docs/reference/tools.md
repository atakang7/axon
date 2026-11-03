---
title: Built-in tools
description: Tool names registered by the runtime.
---

`axon.New` binds these nine tools unless their names appear in `Config.ExcludeBuiltins`.

| Tool | Inputs | Result |
| --- | --- | --- |
| `read_file` | `path`, optional `offset`, `limit` | File contents with line numbers or directory tree |
| `edit` | Existing file and exact text edits | File change and parse/build check |
| `write_file` | `path`, `content` | File write and parse/build check |
| `bash` | `command`, optional working directory and timeout | Process output or background shell ID |
| `bash_output` | Shell ID | New process output |
| `kill_shell` | Shell ID | Stops the process group |
| `grep` | Search expression and path | Text matches |
| `glob` | File pattern | Matching paths |
| `todo_write` | Plan steps | Updated task state |

`edit` and `write_file` check the session's record of previously read files. Each `Tool` has `Name`, `Description`, `Schema` and `Fn` fields.
