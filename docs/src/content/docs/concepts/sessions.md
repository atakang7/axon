---
title: Sessions
description: Saved turns and tracked file edits.
---

Axon saves sessions as JSON under the data directory, one file per working directory by default.

| API | What |
|-----|------|
| `agent.SessionPath()` | Current JSON file |
| `agent.SessionsDir()` | Directory containing sessions |
| `axon.ListSessions(dir, current)` | Session metadata, newest first |
| `agent.SwitchSession(path)` | Load or create another session |
| `agent.Reset()` | Clear the current session |
| `agent.Undo()` | Restore the previous tracked edit |

## Config

| Var | Default | What |
|-----|---------|------|
| `AXON_DATA_DIR` | `~/.local/share/agent` | State root |
| `AXON_SESSION_PATH` | derived from working directory | Use one session file |
| `XDG_DATA_HOME` | unset | Changes the default state root |

Session files use `sessions/{basename}-{hash}.json` under the state root.

## Limits

- `SwitchSession` and `Reset` terminate background shells.
- Edit history is capped at 8 MiB.
