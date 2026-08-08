---
title: Config vs Settings
description: The two configuration surfaces and why they are separate.
---

Axon has two configuration surfaces. They are deliberately separate because they serve different purposes and have different lifetimes.

```
┌──────────────────────────────────────────────────┐
│  axon.Config (Go code)                           │
│                                                  │
│  Wires one agent instance:                       │
│  Model, SystemPrompt, Tools, Pruner, Session,    │
│  Cwd, MCPServers, OnEvent, ExcludeBuiltins,      │
│  Settings                                        │
│                                                  │
│  → Passed to axon.New()                          │
│  → Different per agent in same process           │
└──────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────┐
│  axon.Settings / axon.yaml (file)                │
│                                                  │
│  Operational policy:                             │
│  Providers, Model params, Retry, Tool caps,      │
│  Pruner thresholds, Session location             │
│                                                  │
│  → Loaded by axon.Load() or axon.LoadFrom()      │
│  → Never loaded automatically by axon.New()      │
│  → Safe to commit (secrets live in .env)         │
└──────────────────────────────────────────────────┘
```

## `axon.Config`

```go
type Config struct {
    Model           Model                           // required
    SystemPrompt    string                          // required
    Tools           []Tool                          // custom tools (appended to built-ins)
    ExcludeBuiltins []string                        // remove built-in tools by name
    Pruner          Model                           // secondary model for context management
    Cwd             string                          // working directory override
    Session         *Session                        // pre-loaded session (nil = auto)
    OnEvent         func(ctx context.Context, e Event) // event callback
    MCPServers      []MCPServer                     // MCP subprocess servers
    Settings        Settings                        // operational policy
}
```

This is the contract for `axon.New()`. Each field controls what the agent *is*: its model, its role, its tools.

## `axon.Settings`

This is the parsed contents of `axon.yaml`. It controls how the agent *operates*: request timeouts, retry policy, tool caps, pruner thresholds.

The zero value is usable — every unset field falls back to `DefaultSettings()` via `WithDefaults()`.

```go
settings, err := axon.Load()          // reads axon.yaml + .env
settings = settings.WithDefaults()    // already called by Load()
```

### Key rule

`axon.New()` never loads `axon.yaml` automatically. Loading is the embedder's decision. This means:
- Two agents in one process can use different settings
- Tests don't need filesystem setup
- The runtime has no implicit dependency on file layout
