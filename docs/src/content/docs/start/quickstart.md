---
title: Quickstart
description: Get an agent running in under a minute.
---

## Install

```bash
go get github.com/atakang7/axon/v2
```

## Minimal agent (no config file)

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/atakang7/axon/v2"
)

func main() {
    model, err := axon.OpenAI(axon.ClientConfig{
        Provider: axon.Provider{
            BaseURL: "https://openrouter.ai/api",
            Model:   "deepseek/deepseek-v3.2",
            APIKey:  os.Getenv("OPENROUTER_API_KEY"),
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

    result, err := agent.Step(context.Background(), "List the files in this directory.")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(result.Assistant)
}
```

**Required:** `Config.Model` and `Config.SystemPrompt`. Everything else has defaults.

## Multi-turn loop

```go
agent.Run(ctx, func() (string, bool) {
    fmt.Print("> ")
    var line string
    _, err := fmt.Scanln(&line)
    return line, err == nil
})
```

`Run` calls `Step` for each line until the input function returns `false` or `ctx` is cancelled. Interruptions (`ErrInterrupted`) are handled — the loop continues.

## With config file

```go
settings, err := axon.Load() // reads ~/.config/axon/axon.yaml + .env
if err != nil {
    log.Fatal(err)
}

model, err := settings.NewClient("openrouter", "deepseek/deepseek-v3.2")
if err != nil {
    log.Fatal(err)
}

agent, err := axon.New(axon.Config{
    Model:        model,
    SystemPrompt: "You are a careful coding agent.",
    Settings:     settings,
})
if err != nil {
    log.Fatal(err)
}
defer agent.Close()
```

`Settings.NewClient(endpoint, model)` resolves the provider from config, applies model settings, and returns a working `Model` in one call.

## With pruner

```go
prunerModel, _ := axon.OpenAI(axon.ClientConfig{
    Provider: axon.Provider{
        BaseURL: "https://openrouter.ai/api",
        Model:   "qwen/qwen3.6-flash",    // cheap, fast
        APIKey:  os.Getenv("OPENROUTER_API_KEY"),
    },
})

agent, _ := axon.New(axon.Config{
    Model:        mainModel,
    Pruner:       prunerModel,
    SystemPrompt: "...",
})
```

The pruner is a secondary model that parks stale context. Use a cheap flash-tier model. See [Context Management](/axon/concepts/context/) for details.

## With events

```go
agent, _ := axon.New(axon.Config{
    Model:        model,
    SystemPrompt: "...",
    OnEvent: func(ctx context.Context, e axon.Event) {
        switch e.Kind {
        case axon.KindToken:
            fmt.Print(e.Text)
        case axon.KindToolCall:
            fmt.Printf("→ %s\n", e.Tool.Name)
        case axon.KindError:
            fmt.Fprintf(os.Stderr, "err: %v\n", e.Err)
        }
    },
})
```

See [Events](/axon/concepts/events/) for the full kind reference.

## Best practices

1. **Always `defer agent.Close()`** — kills background shells, closes MCP subprocesses.
2. **Use `Settings.NewClient()`** when loading from config.
3. **Keep system prompts instructional** — Axon appends tool schemas automatically.
4. **Enable the pruner for long sessions** — context grows unbounded without it.
5. **Background all network commands** — if it *might* hang, set `run_in_background: true`.
6. **Concurrency:** `Step`/`Run`/`Reset` are not concurrent-safe. `Interrupt()` is the only goroutine-safe method.
7. **Tool schema is always required** — even for no-arg tools use `map[string]any{"type": "object"}`.
