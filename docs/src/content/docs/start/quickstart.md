---
title: Quickstart
description: Run the Go library in a new module.
---

## Install

Use Go 1.26.2 or later.

```sh
mkdir axon-demo
cd axon-demo
go mod init example.com/axon-demo
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
            BaseURL: os.Getenv("AXON_BASE_URL"),
            Model:   os.Getenv("AXON_MODEL"),
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

Save the Go snippet as `main.go`, then set your endpoint credentials and run it:

```sh
export AXON_BASE_URL=https://openrouter.ai/api
export AXON_MODEL=deepseek/deepseek-v3.2
export OPENROUTER_API_KEY='your-key'
go run .
```

`Config.Model` and `Config.SystemPrompt` are required. No config file is needed for this example.

## Multi-turn loop

```go
if err := agent.Run(ctx, func() (string, bool) {
    fmt.Print("> ")
    var line string
    _, err := fmt.Scanln(&line)
    return line, err == nil
}); err != nil {
    log.Fatal(err)
}
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

## With events

```go
agent, err := axon.New(axon.Config{
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
if err != nil {
    log.Fatal(err)
}
defer agent.Close()
```

See [Events](/axon/concepts/events/) for the full kind reference.

## Operational notes

Call `Close()` when finished so background shells and MCP subprocesses exit. `Step`, `Run` and `Reset` are not safe to call concurrently on a single agent; `Interrupt()` can be called from another goroutine. Unattended agents should set `MaxIterations`.
