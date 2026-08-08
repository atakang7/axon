---
title: Quick Start
description: Run your first Axon agent.
---

## Installation

Axon is a standard Go module.

```bash
go get github.com/atakang7/axon
```

## Basic Loop

Initialize configuration, instantiate a model provider, and start the agent.

```go
package main

import (
    "context"
    "fmt"
    "github.com/atakang7/axon"
)

func main() {
    ctx := context.Background()

    // 1. Load settings from axon.yaml and .env
    settings, err := axon.Load()
    if err != nil {
        panic(err)
    }
    
    // 2. Initialize provider client
    model, err := settings.NewClient("openrouter", "deepseek/deepseek-v3.2")
    if err != nil {
        panic(err)
    }

    // 3. Create the agent
    ag, err := axon.New(axon.Config{
        Model:        model,
        SystemPrompt: "You are an expert coding assistant.",
        Settings:     settings,
    })
    if err != nil {
        panic(err)
    }
    defer ag.Close()

    // 4. Execute a single turn
    res, err := ag.Step(ctx, "list every TODO comment under cmd/")
    if err != nil {
        panic(err)
    }
    fmt.Println(res)
}
```

Axon automatically attaches its built-in tools (read, write, exec, search, task) by default. `Step` runs one turn to completion and returns the final assistant text alongside a record of executed tools.
