---
title: Writing Telemetry Callbacks
description: Intercept state mutations and output streams.
---

Write an `OnEvent` callback in `axon.Config` to capture the internal state machine transitions, tool dispatches, and token streams.

## Event Dispatcher Implementation

Define a switch block matching `axon.Kind*` constants. Do not execute blocking operations inside this callback; it will stall the generation loop.

```go
import (
    "context"
    "fmt"
    "log"
    "github.com/atakang7/axon"
)

func BuildEventLogger() func(context.Context, axon.Event) {
    return func(ctx context.Context, e axon.Event) {
        switch e.Kind {
        
        // Print raw model text output to stdout directly.
        case axon.KindToken:
            fmt.Print(e.Text)
            
        // Print internal reasoning (e.g. <think> blocks in DeepSeek).
        case axon.KindReasoning:
            // Often rendered in dim or distinct styling.
            fmt.Print(e.Text)
            
        // Log tool arguments when the model decides to dispatch one.
        case axon.KindToolCall:
            log.Printf("Executing tool [%s] with args: %s", e.Tool.Name, e.Tool.Args)
            
        // Log the return payload from a completed tool execution.
        case axon.KindToolResult:
            log.Printf("Tool [%s] completed. Output length: %d bytes", e.Tool.Name, len(e.Text))
            
        // Log runtime-level faults.
        case axon.KindError:
            log.Printf("Runtime failure: %v", e.Error)
        }
    }
}
```

## Registration

Assign the callback prior to calling `axon.New`.

```go
config := axon.Config{
    Model:        model,
    SystemPrompt: prompt,
    Settings:     settings,
    OnEvent:      BuildEventLogger(),
}

ag, err := axon.New(config)
```
