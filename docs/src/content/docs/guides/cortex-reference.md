---
title: Cortex (Reference TUI)
description: Architectural case study of a terminal UI integration.
---

To understand how Axon is designed to be consumed, examine **Cortex**—a Terminal User Interface (TUI) agent built atop the Axon runtime.

```mermaid
graph TD
    classDef axon fill:#4F46E5,stroke:#312E81,stroke-width:2px,color:#fff;
    classDef tui fill:#059669,stroke:#064E3B,stroke-width:2px,color:#fff;
    classDef channel fill:#F59E0B,stroke:#B45309,stroke-width:2px,color:#000;

    subgraph "Goroutine 1: Axon Orchestrator"
        A[ag.Run()]:::axon --> B(OnEvent Hook):::axon
    end

    B == "chan axon.Event (Buffered)" ===> C{UI Event Loop}:::channel

    subgraph "Goroutine 2: Main UI Thread (Bubble Tea)"
        C --> D[Update State]:::tui
        D --> E[Render Terminal ANSI]:::tui
    end
```

## Architectural Decoupling

Cortex is fundamentally a standard Bubble Tea (or similar Go TUI) application. It handles keystrokes, renders layout boxes, and draws ANSI colors. 

It offloads 100% of the LLM complexity to Axon.

### The Integration Boundary

Cortex instantiates Axon and binds an asynchronous channel to the `OnEvent` telemetry hook.

```go
// Create a buffered channel to bridge the Axon thread and the UI thread.
eventStream := make(chan axon.Event, 256)

ag, _ := axon.New(axon.Config{
	Model: model,
	OnEvent: func(ctx context.Context, e axon.Event) {
		// Non-blocking send. Drops events if UI hangs.
		select {
		case eventStream <- e:
		default:
		}
	},
})

// Axon blocks while resolving the turn, so we execute it in a background goroutine.
go func() {
	ag.Run(context.Background(), getTerminalInput)
}()
```

### The UI Thread

The main thread of Cortex simply ranges over the `eventStream`. When a `KindToken` arrives, it appends a string to the chat bubble component. When a `KindToolCall` arrives, it renders a loading spinner component. 

By maintaining this strict boundary, Cortex's codebase remains entirely focused on terminal rendering, while Axon securely isolates the state machine, the HTTP streams, and the subprocess execution.
