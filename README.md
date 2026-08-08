# Axon

**A Go runtime for tool-using LLM agents.** Axon owns the execution machinery—turns, tools, sessions, context pressure, retries, streaming, background processes, MCP tool discovery, and runtime events—while your application owns the product around it.

```bash
go get github.com/atakang7/axon/v2
```

**[Documentation](https://atakang7.github.io/axon/)** · [Configuration](https://atakang7.github.io/axon/configuration/) · [API reference](https://atakang7.github.io/axon/reference/agent/)

## Minimal shape

```go
model, err := axon.OpenAI(axon.ClientConfig{
    Provider: axon.Provider{
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

result, err := agent.Step(context.Background(), "Explain this repository.")
if err != nil {
    log.Fatal(err)
}
fmt.Println(result.Assistant)
```

## Configuration

Axon has two different configuration surfaces:

- **`axon.Config`** wires an agent instance: model, system prompt, tools, pruner model, session, working directory, MCP servers, and event callback.
- **`axon.Settings` / `axon.yaml`** holds operational policy: providers, request behavior, retries, tool limits, context policy, and state locations.

They are deliberately separate. `axon.New` never loads `axon.yaml` automatically.

See the **[configuration manual](https://atakang7.github.io/axon/configuration/)** for precedence, defaults, every field, and a complete example.

## License

[MIT](LICENSE)
