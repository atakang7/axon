---
title: Reference Implementation
description: Showcase of the Cortex TUI Agent.
---

To illustrate how Axon integrates into a production architecture, consider **Cortex**, a Terminal User Interface (TUI) agent built on top of the Axon runtime.

## What is Cortex?

Cortex is a persistent, local terminal assistant. It uses a Go-based TUI framework for its frontend while relying entirely on Axon as its backend execution engine. 

### Why this architecture?

By strictly decoupling the terminal rendering from the agent execution loop, Cortex achieves:
1. **Zero-Overhead Orchestration:** Axon runs in a background goroutine, managing the LLM stream and executing bash/file tools.
2. **Event-Driven UI:** Cortex subscribes to Axon's `OnEvent` callback. As Axon emits `KindToken` or `KindToolCall` events, Cortex routes them through channels to update the UI reactively.
3. **Session Persistence:** Because Axon handles the append-only session projection natively, Cortex can seamlessly restart or undo actions (`ag.Undo()`) directly from the terminal without manual state synchronization.

## Integration Blueprint

Here is the concrete bridging logic mapping Axon's event stream into a UI state machine.

```go
// 1. Initialize Axon with an event channel.
eventCh := make(chan axon.Event, 100)

config := axon.Config{
    Model:        model,
    SystemPrompt: "You are Cortex, a senior terminal assistant.",
    Settings:     settings,
    OnEvent: func(ctx context.Context, e axon.Event) {
        // Non-blocking dispatch to the UI thread
        select {
        case eventCh <- e:
        default:
        }
    },
}
ag, _ := axon.New(config)

// 2. Start the Axon loop in a background Goroutine.
go func() {
    // userPromptSupplier is a callback returning terminal input
    ag.Run(context.Background(), userPromptSupplier) 
}()

// 3. UI Thread: Consume events and render.
for e := range eventCh {
    switch e.Kind {
    case axon.KindToken:
        tui.AppendToMessageBubble(e.Text)
    case axon.KindToolCall:
        tui.RenderSpinner("Running tool: " + e.Tool.Name)
    case axon.KindPruneStart:
        tui.RenderWarning("Context limit reached. Pruning session...")
    }
}
```

This clear separation of concerns—Axon owns the network, tools, and LLM state; Cortex owns the terminal drawing—is exactly what the Axon runtime is designed to facilitate.
