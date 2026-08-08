---
title: Telemetry & Events
description: Subscribing to the runtime execution frames.
---

Axon is fundamentally a background orchestrator. To surface its behavior to a UI, a log aggregator, or a metrics pipeline, you must implement the `OnEvent` hook.

```mermaid
graph LR
    classDef event fill:#8B5CF6,stroke:#5B21B6,stroke-width:2px,color:#fff;
    classDef sink fill:#3B82F6,stroke:#1D4ED8,stroke-width:2px,color:#fff;

    Stream[Internal Loop] -->|Dispatches Event| Handler{Config.OnEvent}:::event
    
    Handler -->|KindToken| UI[UI Chat Bubble]:::sink
    Handler -->|KindToolCall| Spin[Loading Spinner]:::sink
    Handler -->|KindPruneStart| Warn[Context Warning]:::sink
    Handler -->|KindError| Log[Log Aggregator]:::sink
```

## The Event Loop

`Config.OnEvent` receives a synchronous callback for every state transition in the `ag.Step` loop. 

**Critical Requirement:** The `OnEvent` callback blocks the primary execution thread of the agent. Do not perform heavy synchronous I/O (like writing to a slow database) directly inside this closure. If you need to write to a slow sink, dispatch the event to a buffered channel.

### Example: Stdout Telemetry

```go
func LoggingCallback(ctx context.Context, e axon.Event) {
	switch e.Kind {
	case axon.KindToken:
		// Stream the assistant's standard text output.
		fmt.Print(e.Text)
		
	case axon.KindToolCall:
		// Log the intent to execute a tool.
		log.Printf("[DISPATCH] %s: %s\n", e.Tool.Name, e.Tool.Args)
		
	case axon.KindPruneStart:
		// Emit telemetry that the context window hit capacity.
		log.Println("[WARN] Token pressure critical. Invoking secondary pruner model.")
		
	case axon.KindError:
		// Catch HTTP stream failures or tool panics.
		log.Printf("[FATAL] %v\n", e.Error)
	}
}

// Attach during initialization:
config := axon.Config{
	Model:   model,
	OnEvent: LoggingCallback,
}
```

For the complete enumeration of event types, refer to the `Kind*` constants in `handler.go`.
