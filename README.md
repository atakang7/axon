# Axon

A Go runtime for tool-using LLM agents. Axon owns turns, tools, sessions, context pressure, retries, streaming, background processes, MCP, and events. Your application owns the product.

```bash
go get github.com/atakang7/axon/v2
```

---

## Documentation

Full documentation is available at **[atakang7.github.io/axon](https://atakang7.github.io/axon/)**.

It covers:
- **[Quickstart](https://atakang7.github.io/axon/start/quickstart/)** — Get an agent running in under a minute
- **[Configuration](https://atakang7.github.io/axon/configuration/yaml/)** — Complete `axon.yaml` reference
- **[Built-in Tools](https://atakang7.github.io/axon/tools/builtins/)** — File I/O, search, and process management
- **[Context Management](https://atakang7.github.io/axon/runtime/context/)** — How the pruner keeps long sessions stable
- **[Events](https://atakang7.github.io/axon/runtime/events/)** — Build any UI with the structured event stream
- **[Architecture](https://atakang7.github.io/axon/internals/architecture/)** — Internal mechanics and security boundaries

---

## Quick Example

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
    // 1. Initialize the model client
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

    // 2. Construct the agent
    agent, err := axon.New(axon.Config{
        Model:        model,
        SystemPrompt: "You are a careful coding agent.",
    })
    if err != nil {
        log.Fatal(err)
    }
    defer agent.Close() // kills background shells, closes MCP

    // 3. Drive the turn loop
    result, err := agent.Step(context.Background(), "List the files in this directory.")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(result.Assistant)
}
```

---

## License

[MIT](LICENSE)
