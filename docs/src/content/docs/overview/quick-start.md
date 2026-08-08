---
title: Initializing the Agent
description: Code implementation for the core loop.
---

Include `github.com/atakang7/axon` in your `go.mod`.

## Bootstrap the Runtime

1. Load the immutable configuration from the filesystem.
2. Allocate the LLM provider client.
3. Construct the agent instance.

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/atakang7/axon"
)

func main() {
    ctx := context.Background()

    // Load configuration. Reads ~/.config/axon/axon.yaml and .env.
    settings, err := axon.Load()
    if err != nil {
        log.Fatalf("config load failed: %v", err)
    }
    
    // Allocate provider. The model string must match a configured model in your YAML.
    model, err := settings.NewClient("openrouter", "deepseek/deepseek-v3.2")
    if err != nil {
        log.Fatalf("provider allocation failed: %v", err)
    }

    // Construct the agent. Attaches default tools automatically.
    ag, err := axon.New(axon.Config{
        Model:        model,
        SystemPrompt: "You are a backend orchestrator. Use tools strictly when required.",
        Settings:     settings,
    })
    if err != nil {
        log.Fatalf("agent construction failed: %v", err)
    }
    defer ag.Close()

    // Execute one turn.
    response, err := ag.Step(ctx, "List the open ports on this machine.")
    if err != nil {
        log.Fatalf("step failed: %v", err)
    }
    
    fmt.Println(response)
}
```

Do not share the `ag` instance across concurrent `Step` calls for different users. One `Agent` represents one session.
