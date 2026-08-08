---
title: Architecture Model
description: System boundaries and invariants.
---

Implement Axon as an orchestrator for a single model API connection. It manages tool dispatches, memory persistence, and context pruning.

## Invariants

1. **Monolithic Model Allocation.** You supply one primary model for inference and one secondary model for context pruning. Axon does not manage parallel sub-agents.
2. **Turn-Scoped Execution.** Wrap every `Step` call in a finite `context.Context`. This context bounds the HTTP request and all tool subprocesses spawned during the turn.
3. **Stateless Settings.** Load `axon.Config` exactly once at startup. Settings are immutable during runtime.
4. **Append-Only Memory.** Session state is an immutable event log. Implement `/undo` or state truncations strictly via projection, not mutation.

## API Surface

Interact with the runtime exclusively through the `axon.Agent` interface:

```go
// Block until a turn completes, returning the assistant response.
ag.Step(ctx context.Context, input string) (string, error)

// Block and loop continuously against an input provider.
ag.Run(ctx context.Context, inputFn func() (string, error))

// Halt the active Step or Run loop immediately.
ag.Interrupt() bool

// Flush the session and rebuild the root system prompt.
ag.Reset()

// Return the current immutable state projection.
ag.Session() *axon.Session
```
