---
title: What is Axon?
description: The scope and design of the Axon runtime.
---

Axon is the runtime between **an application that wants an agent** and **a model that can call tools**.

It is intentionally not a complete agent product. There is no CLI entrypoint, UI, provider picker, project workflow, or opinionated user experience in the package. An embedder owns those choices.

## What Axon owns

Axon implements the parts that become repetitive and correctness-sensitive in every tool-using agent:

- iterative model → tool → model execution;
- OpenAI-compatible streamed chat completions;
- retries for configured HTTP statuses and transient transport failures;
- built-in read/write/search/exec/background/task tools;
- custom tools through a small `Tool` contract;
- MCP tool discovery over a spawned stdio server;
- on-disk sessions and edit history;
- working-directory changes and undo;
- free recency-window context collapse plus optional model-assisted pruning;
- structured synchronous runtime events;
- interruption and cleanup of process resources.

## What the embedder owns

The application still decides:

- which `Model` implementation to use;
- the system prompt;
- which provider/model is selected from configuration;
- whether a pruner model is enabled;
- which custom or MCP tools exist;
- which built-ins are excluded;
- what the event stream is used for;
- the user interaction model around `Step`, `Run`, `Reset`, `Undo`, and `Cd`.

## Library-first by design

Configuration loading is **opt-in**. `New` does not read `axon.yaml` or `.env`. It receives a resolved `Config`, applies defaults to `Config.Settings`, and freezes those settings into that agent instance.

That has two useful consequences:

1. two agents in one process can use different settings;
2. tests or embedders can construct everything directly without arranging global files.

`Load()` is a convenience path when you do want Axon's YAML + credentials-file convention.

## One protocol ships, one interface is required

Axon ships a client for OpenAI-style streamed chat completions. Anything that does not speak that protocol can still be used by implementing the single-method `Model` interface:

```go
type Model interface {
    Complete(ctx context.Context, req Request) (*Msg, error)
}
```

That boundary is the core of the package: the runtime knows how to drive a model, not how every model provider in existence works.
