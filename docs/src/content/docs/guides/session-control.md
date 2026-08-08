---
title: Control a session
description: Change workspaces, interrupt turns, undo edits, reset state, and choose custom sessions.
---

## Change working directory

```go
path, err := agent.Cd("../other-project")
```

`Cd` verifies the target is an existing directory, stores the resolved path on the session, attempts to save, and returns the resolved directory.

Remember: this changes path resolution for tools; it is not a security boundary.

## Interrupt the active turn

From another goroutine/UI action:

```go
if agent.Interrupt() {
    // a turn cancel function was active
}
```

`Step` maps cancellation of its active turn context to `ErrInterrupted`. `Run` treats that sentinel as “skip this interrupted step and continue asking for input.”

## Undo the last built-in edit

```go
path, ok := agent.Undo()
```

When available, Axon restores the recorded pre-edit bytes with its atomic writer, appends a system note explaining the revert, and saves the session.

Undo is one-edit-at-a-time local recovery, not a replacement for Git.

## Reset conversational state

```go
agent.Reset()
```

Reset:

- kills background shells;
- resets the session while preserving its on-disk path;
- resets turn/task/message/edit state;
- recreates the initial system message;
- rebinds built-ins with the same exclusions;
- reattaches the same caller/MCP tool objects.

MCP subprocesses are not restarted.

## Supply your own Session

```go
session := axon.LoadOrCreateSessionAt("/my/state/session.json")

agent, err := axon.New(axon.Config{
    Model:        model,
    SystemPrompt: "...",
    Session:      session,
})
```

Axon reuses the exact pointer. This bypasses normal configured session-path selection.

## Concurrency rule

Treat an `Agent` as one active turn machine. `Step`, `Run`, `Reset`, `Undo`, `Cd`, and mutable model/pruner operations are not a general concurrently-safe command surface.

`Interrupt` is the explicit goroutine-safe control path, implemented through an atomic cancel-function pointer.
