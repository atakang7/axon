---
title: The runtime model
description: The execution model that predicts how turns, failures, tools, state, and telemetry behave.
---

Axon is easiest to reason about as a **stateful observation loop**:

```text
user intent
    │
    ▼
  model ── proposes action ──▶ tool/runtime
    ▲                            │
    └──── receives observation ──┘
```

The model decides what should happen next. The runtime decides what actually happens on the machine, records the result, and turns that result into the model's next observation.

That single loop explains most of Axon's behavior.

## A turn is not a model request

One user input creates one **turn**. A turn may contain many model requests:

```text
user
  ↓
model request #1
  ↓ tool call
execution
  ↓ tool result
model request #2
  ↓ tool call
execution
  ↓ tool result
model request #3
  ↓
final assistant text
```

This distinction matters operationally. Measure user latency, cancellation, tracing, and task success at the **turn** level; measure tokens, provider failures, and retries at the **model-request** level.

If you treat them as the same thing, agent telemetry becomes misleading very quickly.

## There are three planes

A useful way to separate responsibilities is:

### Decision plane — `Model`

The model receives the current context plus tool contracts and returns text and/or structured tool calls. It has no executable Go function and no direct filesystem/process handle.

### Execution plane — tools

Tools hold real authority: files, shells, services, MCP processes, or application APIs. A tool call is only a proposal until this plane executes it.

### State plane — `Session`

The session carries observations between model requests and across process restarts. Axon projects model-visible context from that durable state instead of replaying the entire history blindly.

This separation is why you can swap a model without rewriting tools, replace a tool without changing the model transport, and reduce old context without deleting the historical record.

## Most tool failures are useful observations

A missing file, failed command, or rejected edit is usually information about the world, not a reason to abort the entire turn.

Axon therefore converts ordinary tool errors into tool-result observations and lets the model recover.

The practical consequence for tool authors is important: **error quality affects agent quality**. `"replacement matched 3 locations"` gives the next model call something actionable; `"operation failed"` does not.

Errors that mean the loop itself cannot continue—construction failure, unrecoverable model failure, parent cancellation, or the initial session-save failure—escape as Go errors instead.

## Events observe execution; they do not define it

Axon emits runtime events around the loop so UIs and telemetry do not need to infer execution from assistant text.

Keep two categories separate:

- **streaming events** (`token`, `reasoning`, `tool_arg_delta`) describe work in progress;
- **resolved events** (`tool_call`, `tool_result`, `assistant_end`, `turn_end`) describe completed runtime boundaries.

Use streaming events for presentation. Use resolved events for audit/tracing decisions.

The callback is synchronous, so an observer that blocks can slow the runtime. Persistence is separate: events are a live observation surface, not the session's source of truth.

For implementation patterns, see [Build observability](/axon/guides/observability/) and [Events reference](/axon/reference/events/).

## Runtime policy is fixed at construction

`Agent.New` receives already-selected objects plus settings. It does not continuously reload configuration, auto-select a provider, or mutate an arbitrary `Model` later.

That gives an agent instance a stable operational policy for its lifetime and allows multiple agents in one process to use different models, limits, context policy, and sessions.

## What this model lets you predict

Once you internalize the loop, several design decisions become straightforward:

- Want provider-independent execution? Put provider logic behind `Model`.
- Want the model to perform a new action? Add a capability/tool, not a branch inside the loop.
- Want failures the model can recover from? Return precise tool observations.
- Want durable progress? Put it in session state, not only transient events.
- Want to reduce context cost? Change the projection of durable history rather than deleting history.
- Want safe machine access? Restrict the execution plane with OS/container authority; the model plane is not a sandbox.

That is the core Axon mental model. The rest of the documentation should refine one of those decisions, not invent another architecture.
