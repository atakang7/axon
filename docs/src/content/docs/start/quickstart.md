---
title: Quickstart
description: Build the smallest useful Axon agent without hiding the construction boundaries.
---

This quickstart uses direct Go construction first because it exposes the runtime cleanly. File-based configuration is optional and comes afterward.

## 1. Install

```bash
go get github.com/atakang7/axon/v2
```

The module currently declares Go `1.26.2`.

## 2. Build a model

Axon ships an OpenAI-compatible streaming client:

```go
model, err := axon.OpenAI(axon.ClientConfig{
    Provider: axon.Provider{
        Name:    "my-provider",
        BaseURL: "https://example.com/v1",
        Model:   "<model-id>",
        APIKey:  os.Getenv("MODEL_API_KEY"),
    },
})
if err != nil {
    log.Fatal(err)
}
```

`BaseURL` may end in `/v1`; if it does not, the client appends `/v1` before calling `/chat/completions`.

## 3. Construct an agent

```go
agent, err := axon.New(axon.Config{
    Model:        model,
    SystemPrompt: "You are a careful coding agent.",
})
if err != nil {
    log.Fatal(err)
}
defer agent.Close()
```

`Model` and `SystemPrompt` are the only required constructor fields. Zero-valued operational settings are filled from `axon.DefaultSettings()`.

By default Axon also attaches seven built-in tools: `read`, `write`, `exec`, `bash_output`, `kill_shell`, `search`, and `task`.

## 4. Run one turn

```go
result, err := agent.Step(context.Background(), "Summarize this project.")
if err != nil {
    log.Fatal(err)
}

fmt.Println(result.Assistant)
```

One `Step` can contain many model calls and many tool calls. It returns only when the model finally produces a response with no further tool calls, or the turn fails/interruption occurs.

## 5. Close what you own

`Close` cleans up live background shells and MCP subprocesses owned by the agent. Treat it like closing a database client: once you construct an agent, arrange to close it.

## Optional: load operational settings from files

When you want provider definitions and operational policy outside your binary:

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

That sequence is intentionally explicit: **load settings → choose a provider/model → build a model → wire the agent**.

Read [Configuration model](/axon/configuration/) before treating `axon.yaml` as a global magic file; different sections are consumed at different boundaries.
