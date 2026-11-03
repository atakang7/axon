---
title: Run an agent
description: Create a Go agent that calls an OpenAI-compatible endpoint.
---

Set `OPENROUTER_API_KEY` in your shell and create a Go module:

```sh
mkdir axon-demo && cd axon-demo
go mod init example.com/axon-demo
go get github.com/atakang7/axon/v2
```

Save the following as `main.go`:

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
    model, err := axon.OpenAI(axon.ClientConfig{Provider: axon.Provider{
        BaseURL: "https://openrouter.ai/api",
        Model: "deepseek/deepseek-v3.2",
        APIKey: os.Getenv("OPENROUTER_API_KEY"),
    }})
    if err != nil { log.Fatal(err) }

    agent, err := axon.New(axon.Config{
        Model: model,
        SystemPrompt: "You are a coding assistant.",
    })
    if err != nil { log.Fatal(err) }
    defer agent.Close()

    result, err := agent.Step(context.Background(), "List the files in this directory.")
    if err != nil { log.Fatal(err) }
    fmt.Println(result.Assistant)
}
```

Run it:

```sh
go run .
```

A provider must accept streaming OpenAI-style chat completions.
