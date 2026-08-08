---
title: Getting started
description: Create a model, construct an agent, and run a turn.
---

## Install

```bash
go get github.com/atakang7/axon/v2
```

Axon requires Go `1.26.2` according to the module declaration.

## Option A: construct everything directly

This path requires no Axon config files.

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    axon "github.com/atakang7/axon/v2"
)

func main() {
    model, err := axon.OpenAI(axon.ClientConfig{
        Provider: axon.Provider{
            Name:    "provider",
            BaseURL: "https://example.com/v1",
            Model:   "<model-id>",
            APIKey:  os.Getenv("MODEL_API_KEY"),
        },
    })
    if err != nil {
        log.Fatal(err)
    }

    agent, err := axon.New(axon.Config{
        Model:        model,
        SystemPrompt: "You are a careful coding agent.",
    })
    if err != nil {
        log.Fatal(err)
    }
    defer agent.Close()

    result, err := agent.Step(context.Background(), "List the files here.")
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println(result.Assistant)
}
```

With zero-valued `Settings`, `New` applies `DefaultSettings()`.

## Option B: load Axon's configuration

`Load()` reads the standard `axon.yaml` and `.env` locations, resolves secrets, applies defaults, validates the result, and returns ready-to-use `Settings`.

```go
settings, err := axon.Load()
if err != nil {
    log.Fatal(err)
}

model, err := settings.NewClient("openrouter", "<model-id>")
if err != nil {
    log.Fatal(err)
}

agent, err := axon.New(axon.Config{
    Model:        model,
    SystemPrompt: "You are a careful coding agent.",
    Settings:     settings,
})
```

A minimal configuration looks like this:

```yaml
providers:
  openrouter:
    base_url: https://openrouter.ai/api/v1
    api_key: ${OPENROUTER_API_KEY}
    models:
      <model-id>: {}
```

And the credentials file contains:

```dotenv
OPENROUTER_API_KEY=...
```

:::caution
`Load()` currently requires the credentials file to exist because it reads `.env` before the YAML file, even if your configuration ultimately uses no secret. Construct `Settings` directly when you do not want that file contract.
:::

## Add a pruner only when you want one

Context windowing works without a secondary model. Model-assisted parking is enabled only when `Config.Pruner` is non-nil.

```go
agent, err := axon.New(axon.Config{
    Model:        model,
    Pruner:       cheapModel,
    SystemPrompt: "You are a coding agent.",
    Settings:     settings,
})
```

## Close the agent

Call `Close()` when the owning application is finished. It emits `session_end`, kills live background shells, and kills spawned MCP processes.

## Next

Read [Life of a turn](/axon/overview/life-of-a-turn/) before building UI around `Step`: it explains exactly when persistence, pruning, events, tools, retries, and interruption happen.
