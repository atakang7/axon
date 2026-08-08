---
title: Events & Observability
description: Subscribing to the agent loop.
---

Axon emits typed events for every stage of the execution loop via `Config.OnEvent`.

## Handling Events

Assign a callback to intercept execution states, tool calls, and payload telemetry.

```go
cfg.OnEvent = func(ctx context.Context, e axon.Event) {
    switch e.Kind {
    case axon.KindToken:
        // Stream text output
        fmt.Print(e.Text)
    case axon.KindToolCall:
        // Log tool invocation
        log.Printf("Tool Executed: %s with args %s", e.Tool.Name, e.Tool.Args)
    case axon.KindError:
        // Log terminal errors
        log.Printf("Error: %v", e.Error)
    }
}
```

## Available Event Kinds

Refer to `handler.go` for the canonical list. Key events include:
- `KindTurnStart`, `KindTurnEnd`
- `KindAPICall`
- `KindToken`, `KindReasoning`, `KindAssistantEnd`
- `KindToolCall`, `KindToolResult`, `KindToolError`
- `KindPruneStart`, `KindPruneEnd`
