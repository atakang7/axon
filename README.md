<p align="center">
  <img src="docs/public/axon-hero.svg" alt="Axon" width="880">
</p>

<h3 align="center">A Go runtime for tool-using LLM agents</h3>

<p align="center">
  Axon owns turns, tools, sessions, context pressure, retries, streaming, background processes, MCP, and events. Your application owns the product.
</p>

<p align="center">
  <a href="https://atakang7.github.io/axon/">Docs</a> ·
  <a href="#quickstart">Quickstart</a> ·
  <a href="#example">Example</a> ·
  <a href="LICENSE">MIT License</a>
</p>

---

## Quickstart

```bash
go get github.com/atakang7/axon/v2
```

That's it. Axon has zero third-party runtime dependencies — only `gopkg.in/yaml.v3`, pulled in lazily when you load a `axon.yaml`.

Point it at any OpenAI-compatible endpoint and drive a turn:

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
    defer agent.Close() // kills background shells, closes MCP

    result, err := agent.Step(context.Background(), "List the files in this directory.")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(result.Assistant)
}
```

Set an API key and run:

```bash
export OPENROUTER_API_KEY=sk-...
go run .
```

## Example

The snippet above is the full agent loop — one client, one agent, one `Step`. Axon layers the rest on top when you need it:

- **Tools** — file I/O, search, shell exec, background processes, all built in
- **Sessions** — persistent, resumable, with automatic context pruning under pressure
- **MCP** — connect external tool servers over stdio or HTTP
- **Events** — a structured stream you can render into any UI

---

## Documentation

Full documentation lives at **[atakang7.github.io/axon](https://atakang7.github.io/axon/)**:

**Getting Started**
- [What is Axon?](https://atakang7.github.io/axon/start/overview/) — What Axon is and why it exists.
- [Quickstart](https://atakang7.github.io/axon/start/quickstart/) — Get an agent running in under a minute.

**Core Concepts**
- [The Turn Loop](https://atakang7.github.io/axon/concepts/turn-loop/) — How `Step()` drives the agent through model calls and tool execution.
- [Context Management](https://atakang7.github.io/axon/concepts/context/) — How Axon keeps the model's context window bounded.
- [Sessions & Memory](https://atakang7.github.io/axon/concepts/sessions/) — Persistence, listing, switching, and undo.
- [Events & UI](https://atakang7.github.io/axon/concepts/events/) — The structured event stream for building UIs.

**Tools**
- [Built-in Tools](https://atakang7.github.io/axon/tools/builtins/) — The seven tools that ship with every agent.
- [Custom Tools](https://atakang7.github.io/axon/tools/custom/) — How to add your own tools to an agent.
- [MCP Servers](https://atakang7.github.io/axon/tools/mcp/) — Integrate Model Context Protocol tool servers.

**Configuration**
- [Agent Setup](https://atakang7.github.io/axon/configuration/setup/) — The two configuration surfaces and why they are separate.
- [Runtime Policies](https://atakang7.github.io/axon/configuration/yaml/) — Every field in the configuration file.
- [File Locations](https://atakang7.github.io/axon/configuration/locations/) — Where Axon reads and writes files.

**Under the Hood**
- [Architecture](https://atakang7.github.io/axon/internals/architecture/) — Internal design reference for contributors.
- [Security Boundaries](https://atakang7.github.io/axon/internals/security/) — How Axon protects credentials and configuration from tools.
- [Retry Logic](https://atakang7.github.io/axon/internals/retries/) — How Axon handles transient failures.


---

## License

[MIT](LICENSE)
