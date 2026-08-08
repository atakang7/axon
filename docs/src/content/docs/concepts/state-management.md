---
title: Sessions & state
description: Persistent conversation state, workspace state, task state, and undo.
---

A `Session` is both the durable conversation record and the workspace capability used by built-in tools.

## What is persisted

```go
type Session struct {
    ID          string
    StartedAt   time.Time
    Cwd         string
    Messages    []Msg
    Edits       []Edit
    CurrentTask *Task
    NextBlockID int
    Turn        int
}
```

The private on-disk path and edit mutex are runtime-only fields.

## One default session per process working directory

When `Config.Session` is nil, `New` calls `LoadOrCreateSessionAt(settings.Session.SessionFile())`.

Unless a session path is pinned, the filename is derived from the **process current working directory at construction time**:

```text
<basename>-<12 hex chars from SHA-256(abs cwd)>.json
```

That distinction matters: `Config.Cwd` is applied **after** the session file is selected. Setting `Config.Cwd` changes where tools operate, not which default per-project session file was chosen.

## Message block IDs

Non-system messages with non-empty content receive monotonically increasing IDs such as `m1`, `m2`, ... . Loaded legacy messages without IDs are assigned IDs on load.

Tool results later use these IDs as the stable handles for context breadcrumbs and pruning.

## Save behavior

`Session.Save`:

- creates parent directories with mode `0755`;
- serializes the session as indented JSON;
- writes the session file with mode `0600`.

If an existing session file cannot be decoded, Axon attempts to rename it to:

```text
<path>.corrupt.<unix-seconds>
```

and starts a fresh session instead of deleting the only copy of the broken history.

## Working directory

`Session.Cwd` is the default base for built-in paths and shell commands.

`ResolvePath` joins relative paths to that directory and leaves absolute paths untouched. `SetCwd` resolves a target, verifies it exists and is a directory, then updates the session.

`Agent.Cd` calls `SetCwd` and attempts to persist the change.

## Edit ledger and Undo

Every built-in write records the file's **pre-write content** through `Workspace.RecordEdit`.

The session keeps an edit ledger capped at roughly 8 MiB of previous content, while preserving at least the newest edit. `Agent.Undo` pops the latest edit, atomically writes its old contents back, appends a system message explaining the revert, and saves the session.

A file created from nothing records an empty previous value, so undoing that write currently leaves an empty file rather than deleting the path.

## Task state

The task tool stores a goal, ordered steps, completion flags, and a current-step index in the session. On every model-context projection, `TaskBlock()` renders that plan into a system message appended after the conversation projection.

Task state therefore survives process restarts with the rest of the session.

## Reset

`Agent.Reset`:

- kills all background shells;
- resets the session object while preserving its path;
- rebuilds the initial system message;
- rebinds the same built-ins and caller-supplied tools.

MCP clients are not restarted by `Reset`; they stay attached until `Close`.
