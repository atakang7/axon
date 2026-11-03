---
title: Sessions
description: Persistence, listing, switching, and undo.
---

## Persistence

Sessions auto-save after each message append. One session per working directory by default.

```
~/.local/share/agent/sessions/
├── myproject-a3f2c1.json
├── backend-b7e4d2.json
└── ...
```

Each session file contains: messages, edits (for undo), current task, turn counter, title.

## Session struct

```go
type Session struct {
    ID          string
    StartedAt   time.Time
    Cwd         string
    Title       string       // derived from first user message
    Messages    []Msg
    Edits       []Edit       // for undo
    CurrentTask *Task
    Turn        int
}
```

## Listing sessions

```go
metas := axon.ListSessions(agent.SessionsDir(), agent.SessionPath())
for _, m := range metas {
    fmt.Printf("%s  %s  (turn %d)%s\n",
        m.ID, m.Title, m.Turn,
        map[bool]string{true: " ← current", false: ""}[m.Current])
}
```

`ListSessions` scans the sessions directory and returns lightweight metadata — it does not load full transcripts. Sorted newest first.

```go
type SessionMeta struct {
    ID        string
    Cwd       string
    StartedAt time.Time
    Title     string
    Turn      int
    Path      string
    Current   bool
}
```

## Switching sessions

```go
err := agent.SwitchSession("/path/to/other-session.json")
```

This:
1. Kills all background shells
2. Loads the session (or creates a new one at that path)
3. Rebuilds the system prompt and tool bindings

## Reset

```go
agent.Reset()
```

Wipes the session, kills background shells, rebuilds tools. The agent keeps its model, system prompt, and custom tools.

## Undo

```go
path, ok := agent.Undo()
```

Reverts the last file edit atomically by writing the pre-edit content back. Returns the path that was restored. A system message is appended so the model knows the edit was reverted.

Edit history is capped at 8MiB total.
