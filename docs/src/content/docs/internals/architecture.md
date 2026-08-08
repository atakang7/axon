---
title: Architecture
description: Implementation architecture for contributors and maintainers.
---

This section is deliberately different from the concepts manual. Concepts describe the system users should hold in their head; internals describe how the current Go implementation realizes that model.

## Construction boundary

`New(Config)` is the composition root.

At construction it resolves settings defaults, selects/loads a session, applies an optional workspace directory, allocates a per-agent background-shell registry, starts MCP servers, binds built-in capabilities, validates custom/discovered tools, seeds a fresh system message, constructs an optional pruner, stores resolved policy on the agent, and emits session start.

The important invariant is that lower layers do not repeatedly rediscover configuration from global state.

## Turn boundary

`Step` owns the user-turn state machine. It records input, optionally invokes the pruner, performs a model call, records the assistant response, executes returned tools in order, records those observations, and loops until no tool calls remain.

The model-call implementation is separated from the loop so retry/streaming behavior can evolve without putting provider transport logic in the state machine.

## Context projection boundary

The session stores messages; the primary model consumes a derived projection.

The projection code is responsible for recency collapse, persisted parking breadcrumbs, tool-call/result coherence, and current task injection. The pruner mutates parking metadata but is not the component that serializes provider messages.

## Tool boundary

Tools are runtime objects with executable functions. Before a model call, they are projected into `ToolSpec` values that have no function pointer.

Built-ins bind to capability interfaces/values rather than the entire agent. MCP tools are adapted into the same `Tool` shape during construction.

## Resource ownership

Background shell registries and MCP client lists live on the agent instance. They are lifecycle resources, not package globals.

`Reset` kills background shells and resets conversational/session state while keeping MCP clients attached. `Close` terminates both shell and MCP resources.

## Transport boundary

The shipped client is one implementation of `Model`. It owns OpenAI-compatible request serialization, SSE parsing, request/idle timeout behavior, API-error construction, and streamed callback dispatch.

A custom `Model` can replace this entire transport layer without changing the rest of the runtime.

Use [Source map](/axon/internals/source-map/) for file ownership and [Runtime invariants](/axon/internals/runtime-invariants/) for properties changes should preserve.
