---
title: Overview
description: What Axon is and why it exists.
---

Every agent needs the same plumbing: call a model → parse tool calls → execute tools → feed results back → repeat until done. Then add retries, streaming, context management, session persistence, background processes, and suddenly the "thin wrapper" is 3000 lines of infrastructure.

Axon is that infrastructure, extracted into one Go package with one rule: **the runtime never decides what to build — only how to run it.**

```
┌──────────────────────────────────────────┐
│              Your Application            │
│  (system prompt, product logic, UI/TUI)  │
└──────────────────┬───────────────────────┘
                   │ axon.New(Config{...})
                   ▼
┌──────────────────────────────────────────┐
│                  Axon                    │
│  turn loop · tools · sessions · pruner  │
│  retries · streaming · events · MCP     │
└──────────────────┬───────────────────────┘
                   │ Model.Complete()
                   ▼
┌──────────────────────────────────────────┐
│         OpenAI-compatible API            │
│   (OpenRouter, Ollama, any /v1 endpoint) │
└──────────────────────────────────────────┘
```

## What Axon owns

| Concern | Axon handles it |
|---------|----------------|
| Turn loop | Call model → execute tools → feed back → repeat |
| Built-in tools | read, write, exec, search, task, bash_output, kill_shell |
| Streaming | SSE parsing, idle timeout, token-by-token delivery |
| Retries | Exponential backoff, status-code policy, transport auto-retry |
| Sessions | Per-directory persistence, edit tracking, undo |
| Context pressure | Recency window + pruner (cheap secondary model) |
| Background processes | Spawn, poll, kill with process-group management |
| MCP | Subprocess JSON-RPC lifecycle + tool discovery |
| Events | Structured event stream for any UI |

## What you own

- System prompt (the agent's role)
- Which model to use
- Product logic around the agent
- UI / TUI / API layer
- Custom tools

## The `Model` interface

One method. That's the entire contract.

```go
type Model interface {
    Complete(ctx context.Context, req Request) (*Msg, error)
}
```

Implement it to reach a provider that isn't OpenAI-compatible, to route through your own gateway, or to supply a deterministic fake for tests.

`axon.OpenAI()` returns the implementation that ships — an SSE streaming client for any OpenAI-compatible endpoint.
