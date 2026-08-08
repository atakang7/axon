---
title: Session
description: Persistent session structure, path helpers, message IDs, workspace, edits, and task APIs.
---

## Structure

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

The on-disk path and edit mutex are private runtime fields.

## Loading

```go
LoadOrCreateSession()
LoadOrCreateSessionAt(path)
```

A valid existing JSON file is decoded, internal path restored, Cwd ensured, and missing message IDs assigned.

If an existing file cannot be decoded, Axon attempts to rename it to:

```text
<path>.corrupt.<unix-seconds>
```

prints a diagnostic to stderr, and starts a fresh session.

## Save

```go
func (s *Session) Save() error
```

Creates the parent directory with 0755, marshals indented JSON, and writes the session file with 0600.

The write is a direct `os.WriteFile`, not the atomic writer used for tool file edits.

## Message IDs

`Append` assigns `mN` IDs to non-system messages with non-empty content when they have no ID.

`assignIDs` performs the same repair for loaded sessions and advances `NextBlockID` to the maximum discovered value.

## Workspace methods

```go
Dir() string
ResolvePath(path string) string
SetCwd(path string) error
```

Absolute paths are returned unchanged by `ResolvePath`. Relative paths join against session Cwd (or process cwd fallback).

## Edits

```go
RecordEdit(path, before string)
Undo() (Edit, bool)
```

The edit ledger retains prior file contents with a total target cap of 8 MiB while keeping at least the newest entry.

## Tasks

```go
RegisterTask(goal string, steps []TaskStep) error
AdvanceTask() (string, error)
ReplanTask(goal string, steps []TaskStep) error
TaskBlock() string
```

Task mutations save immediately. `TaskBlock` renders the active goal/plan/current step for injection into model context.

## Context projection

```go
ContextMessages(windowBlocks int) []Msg
Park(id, summary, reason string) error
```

`ContextMessages` returns provider-facing message copies with internal IDs/park metadata stripped, old content collapsed as needed, matching tool results omitted when their tool-call parent is collapsed, and task block appended as a system message.
