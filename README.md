# Axon

**Axon is a small Go runtime for building tool-using LLM agents.** It owns the turn loop, tool dispatch, session persistence, context projection/pruning, background processes, MCP tool discovery, retries, streaming, and structured runtime events.

Axon is a library, not a CLI or an opinionated application shell. You provide the model and system prompt; Axon provides the runtime.

```bash
go get github.com/atakang7/axon/v2
```

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
if err != nil {
    log.Fatal(err)
}
defer agent.Close()

result, err := agent.Step(context.Background(), "Explain this repository.")
if err != nil {
    log.Fatal(err)
}
fmt.Println(result.Assistant)
```

## Documentation

**[Read the Axon documentation →](https://atakang7.github.io/axon/)**

The documentation is organized around the runtime as implemented: architecture, turn lifecycle, configuration, models/providers, sessions, context pruning, built-in tools, MCP, events, security boundaries, and a source-level code map.

## License

[MIT](LICENSE)
