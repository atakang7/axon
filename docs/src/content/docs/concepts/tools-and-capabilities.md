---
title: Tools as capabilities
description: How Axon models actions, authority, failures, and extension points.
---

A tool is a **capability the model may request**, not a plugin with control over the agent loop.

Each tool has two faces:

```text
model-visible contract                runtime-only implementation
name + description + JSON Schema  ──▶ Go function with real authority
```

The model sees the left side. Axon holds the right side.

## Why the split matters

The schema tells a model how to ask. The function decides what the request means on the machine.

Keeping those concerns separate gives Axon a strong composition property: any tool—built-in, application-defined, or MCP-discovered—enters the same loop and returns the same kind of observation.

The model transport never needs a reference to executable tool code.

## Three sources, one toolset

An agent's final toolset can contain:

- **built-ins** for file, search, shell, background-process, and task operations;
- **custom Go tools** supplied by the application;
- **MCP tools** discovered from configured child processes.

Names must be unique. Excluding a built-in frees that name so an application can deliberately replace the capability.

## Authority comes from construction

Axon's built-ins are created with narrow runtime capabilities. File/search/exec operations receive a workspace; the task tool receives plan mutation; process tools receive the shell registry; bounded tools receive limits.

That is an internal least-authority pattern, not an OS sandbox. A tool that can execute shell commands still has the process's real shell authority.

Custom tools deserve the same design discipline: close over the smallest service or interface they need.

## Tool failure is not turn failure

A tool can fail because the world disagrees with the model: a path is wrong, a test fails, an exact string is not unique. Axon turns that failure into a tool observation and lets the model adapt.

This is why tools should return useful errors. A precise domain error is not only diagnostics for a developer; it is input to the next model decision.

## Multiple calls are ordered observations

The shipped model client advertises parallel-tool-call support to compatible providers, but the runtime currently executes returned calls sequentially in provider order.

Do not design two tool calls as if they are guaranteed to overlap in time. If concurrency is part of your capability's semantics, implement it inside that capability or outside the agent loop deliberately.

## Limits are not all the same kind

Some tool settings are **hard ceilings**; others are only **defaults** the model can override per call. For example, exec timeout and tail size have configured maxima, while read slice length and background `max_bytes` are not hard-capped by their similarly named defaults.

The [Tool limits configuration](/axon/configuration/tools/) page classifies each setting explicitly.
