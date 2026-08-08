---
title: Architecture
description: The executable layers and boundaries inside Axon.
---

Axon is a single Go package, but the implementation has clear runtime layers.

```text
Application / embedder
  │
  ├── Config + Settings
  ├── Model
  ├── system prompt
  ├── custom tools / MCP servers
  └── OnEvent
        │
        ▼
┌──────────────── Agent ────────────────┐
│ setup.go      construct capabilities  │
│ loop.go       drive each turn         │
│ agent.go      call model + retry      │
│ handler.go    emit events             │
└───────────────┬───────────────────────┘
                │
       ┌────────┴─────────┐
       ▼                  ▼
 Session/context        Tool layer
 session.go             tools.go
 memory.go              tool_*.go
 pruner.go              bg.go
                        mcp.go
       │                  │
       └────────┬─────────┘
                ▼
              Model
       interface: axon.go
       shipped client: client.go
```

## Construction is the dependency-injection boundary

`New(Config)` is where ambient decisions become concrete capabilities:

1. require `Model` and `SystemPrompt`;
2. apply `Settings.WithDefaults()` once;
3. load/create a session at the resolved session path unless one is supplied;
4. optionally change the session working directory;
5. create a per-agent background-shell registry;
6. start MCP servers and convert discovered MCP tools into Axon `Tool`s;
7. bind built-ins to narrow capabilities (`Workspace`, `Plan`, shell registry, `Limits`);
8. validate custom/MCP tools and reject duplicate names;
9. build the initial system message if the session has no messages;
10. optionally construct a `Pruner`;
11. emit `session_start`.

Nothing below this boundary reloads configuration.

## Capability-scoped tools

Built-in tools do not receive the whole `Agent` or the whole `Settings` tree.

- file/search/exec tools receive a `Workspace`;
- task receives a write-oriented `Plan`;
- process tools receive the background-shell registry;
- bounded tools receive the flattened `Limits` value.

This is an important architecture property: the built-ins cannot reach provider credentials simply because they were handed runtime settings.

## Historical state vs model-visible state

`Session.Messages` is the durable history. The model does not receive that slice directly.

`Session.ContextMessages(windowBlocks)` derives a projection for each model call:

- recent active blocks keep their content;
- active blocks outside the window become generated breadcrumbs;
- parked blocks become persisted parking breadcrumbs;
- tool results whose assistant tool-call block was collapsed are omitted to preserve tool-call protocol coherence;
- the active task plan is appended as a system message.

This separation is why Axon can reduce prompt size without deleting the recorded content.

## Model boundary

The runtime depends on one interface: `Model.Complete`.

The shipped `Client` implements it with streamed OpenAI-compatible chat completions. Tool implementation functions never cross that boundary: the model receives only `ToolSpec` values containing name, description, and JSON Schema.

## Process ownership

Background shells and MCP servers are **per-agent resources**. `Reset` kills background shells; `Close` kills background shells and MCP processes. One agent's registry is not shared with another.
