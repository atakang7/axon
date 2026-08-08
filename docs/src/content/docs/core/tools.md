---
title: Tools
description: Extending Axon capabilities.
---

Axon exposes a standard `Tool` interface. Custom tools execute within the turn's `context.Context`.

## Implementing a Tool

Define the JSON Schema for the arguments, and provide a closure for execution.

```go
import (
    "context"
    "encoding/json"
    "github.com/atakang7/axon"
)

var DeployTool = axon.Tool{
    Name:        "deploy",
    Description: "Deploy a service to staging.",
    Schema: map[string]any{
        "type": "object",
        "properties": map[string]any{
            "service": map[string]any{"type": "string"},
        },
        "required": []string{"service"},
    },
    Fn: func(ctx context.Context, args json.RawMessage) (string, error) { 
        var input struct {
            Service string `json:"service"`
        }
        if err := json.Unmarshal(args, &input); err != nil {
            return "", err
        }
        
        // Execute deployment...
        return "Success: " + input.Service, nil
    },
}
```

## Registration

Pass your tool during agent initialization. It appends to the built-in toolset. Colliding tool names trigger an error during `New`.

```go
ag, err := axon.New(axon.Config{
    Model:        model, 
    SystemPrompt: "...", 
    Tools:        []axon.Tool{DeployTool},
})
```
