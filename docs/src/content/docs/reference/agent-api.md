---
title: Agent API
description: Public runtime contracts for constructing and controlling an agent.
---

## `New(Config)`

Constructs an `*Agent`.

Required:

- `Config.Model`;
- non-empty `Config.SystemPrompt`.

Optional:

| Field | Purpose |
| --- | --- |
| `Tools` | custom tool implementations |
| `ExcludeBuiltins` | remove built-in names before custom-tool validation |
| `Pruner` | secondary `Model` for permanent context parking |
| `Cwd` | set the session's tool working directory after session selection |
| `Session` | provide an existing/in-memory session object |
| `OnEvent` | synchronous event callback |
| `MCPServers` | stdio MCP servers to spawn/discover |
| `Settings` | operational settings; zero fields take defaults |

## Turn control

### `Step(ctx, userInput) (StepResult, error)`

Runs one user turn through as many model/tool iterations as needed.

### `Run(ctx, InputFunc) error`

Repeatedly obtains `(line, ok)` from an input function and calls `Step`. Stops when `ok == false` or the parent context fails. `ErrInterrupted` from an individual step is swallowed so input can continue.

### `Interrupt() bool`

Cancels the active turn context and returns whether a turn cancel function was present. The cancel pointer is atomic; this is the control method designed to be called from another goroutine.

## Runtime mutation

### `SetModel(Model)`

Replaces the model used for future calls.

### `SetPrunerModel(Model)`

Replaces the pruner model, constructing a `Pruner` from the agent's current pruner settings when none exists.

### `SetPruneMode(PruneMode)`

Changes the pruner mode when the supplied value is valid. Invalid values are ignored.

## Workspace/session operations

### `Session() *Session`

Returns the live session pointer.

### `SessionPath() string`

Returns the current session's path, or empty string if no session exists.

### `Cd(target) (string, error)`

Changes session `Cwd`, saves the session, and returns the resolved absolute directory.

### `Undo() (string, bool)`

Reverts the latest recorded built-in file write when available.

### `Reset()`

Kills background shells, resets session state, rebuilds the system prompt, and rebinds built-in + custom tools.

### `Close() error`

Emits `session_end`, kills background shells, and kills MCP child processes. The implementation returns nil after cleanup.

## Concurrency

The agent/session mutation paths are not generally synchronized for concurrent `Step`, `Run`, `Reset`, `Cd`, `Undo`, or model-setting operations. `Interrupt` is specifically implemented with an atomic cancel pointer.

Treat one `Agent` as a single active turn machine unless you add an external synchronization layer.
