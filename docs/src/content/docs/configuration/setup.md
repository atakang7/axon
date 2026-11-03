---
title: Config and Settings
description: Agent construction and optional file loading.
---

`Config` wires an `Agent`; `Settings` holds timeouts, providers, tool limits and session paths.

```text
+-----------+  Load()  +----------+  New()  +-------+
| axon.yaml | -------> | Settings | ------> | Agent |
+-----------+          +----------+         +-------+
```

`Load()` also reads `.env` for provider credentials, while `New()` never reads either file.

```go
settings, err := axon.Load()
if err != nil {
    log.Fatal(err)
}
model, err := settings.NewClient("openrouter", "deepseek/deepseek-v3.2")
if err != nil {
    log.Fatal(err)
}
agent, err := axon.New(axon.Config{
    Model: model, SystemPrompt: "You are a coding assistant.",
    Settings: settings,
})
if err != nil {
    log.Fatal(err)
}
defer agent.Close()
```

## Limits

- `Config.Model` and `Config.SystemPrompt` are required.
- Unset settings use `DefaultSettings()`; loaded settings are not re-read after construction.
