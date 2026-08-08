---
title: Agent
description: Constructor contract, turn APIs, lifecycle operations, and mutable controls.
---

## `Config`

```go
type Config struct {
    Model           Model
    SystemPrompt    string
    Tools           []Tool
    ExcludeBuiltins []string
    Pruner          Model
    Cwd             string
    Session         *Session
    OnEvent         func(context.Context, Event)
    MCPServers      []MCPServer
    Settings        Settings
}
```

### Required

- `Model` — nil returns `ErrNoModel`.
- `SystemPrompt` — empty returns `ErrNoSystemPrompt`.

### Construction effects

`New` applies settings defaults, selects/reuses a session, optionally changes Cwd, creates shell ownership, starts MCP servers, binds tools, rejects incomplete/duplicate tools, seeds a system message only for an empty session, optionally builds a pruner, and emits `session_start`.

## `Step`

```go
func (a *Agent) Step(ctx context.Context, userInput string) (StepResult, error)
```

Rejects empty input. Runs one user turn until a model response has no more tool calls.

```go
type StepResult struct {
    Assistant string
    ToolCalls []ToolCall
    Turn      int
}
```

`Assistant` is the latest non-empty assistant content observed in the turn. `ToolCalls` contains calls encountered across model-loop iterations.

## `Run`

```go
func (a *Agent) Run(ctx context.Context, input InputFunc) error
```

Repeatedly reads `(line, ok)` from `InputFunc` and calls `Step`. Stops at `ok == false` or parent-context failure.

`ErrInterrupted` from one step is swallowed and input continues.

## `Interrupt`

```go
func (a *Agent) Interrupt() bool
```

Atomically invokes the active turn cancel function when present. Returns false when no cancel pointer is installed.

## Model/pruner mutation

```go
func (a *Agent) SetModel(m Model)
func (a *Agent) SetPrunerModel(m Model)
func (a *Agent) SetPruneMode(mode PruneMode)
```

`SetPrunerModel` creates a pruner from current settings when none exists. Invalid modes passed to `SetPruneMode` are ignored.

## Session/workspace

```go
func (a *Agent) Session() *Session
func (a *Agent) SessionPath() string
func (a *Agent) Cd(target string) (string, error)
func (a *Agent) Undo() (string, bool)
func (a *Agent) Reset()
```

See [Session reference](/axon/reference/session/).

## Close

```go
func (a *Agent) Close() error
```

Emits `session_end`, kills background shells, kills MCP subprocesses, and currently returns nil.

## Concurrency

The public code comment states that `Step`, `Run`, and `Reset` must not be called concurrently and that `Interrupt` is goroutine-safe. Other mutable session/model operations likewise should be externally serialized around an active turn.
