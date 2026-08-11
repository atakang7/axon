---
title: Quickstart
description: Get an agent running in under a minute.
---

## Create a Go module

Axon is a Go library, not a standalone CLI. `go get` must run inside a Go module:

```bash
mkdir axon-quickstart
cd axon-quickstart
go mod init example.com/axon-quickstart
go get github.com/atakang7/axon/v2@latest
```

## Create the agent

Save this as `main.go`:

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

## Compile before using an API key

This verifies the documented program against the Axon version selected by Go:

```bash
go build .
```

## Run

```bash
export OPENROUTER_API_KEY=sk-or-...
go run .
```

The API call requires a valid OpenRouter key; the preceding `go build .` check does not.

## Multi-turn loop

Once you have the agent above, you can replace the single `Step` with:

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

## Best practices

1. **Always `defer agent.Close()`** — kills background shells, closes MCP subprocesses.
2. **Use `Settings.NewClient()`** when loading from config.
3. **Keep system prompts instructional** — Axon appends tool schemas automatically.
4. **Enable the pruner for long sessions** — context grows unbounded without it.
5. **Background network commands that may hang.**
6. **Concurrency:** `Step`/`Run`/`Reset` are not concurrent-safe. `Interrupt()` is the only goroutine-safe method.
7. **Tool schema is always required** — even for no-arg tools use `map[string]any{"type": "object"}`.
