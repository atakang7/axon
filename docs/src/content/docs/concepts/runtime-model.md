---
title: The runtime model
description: The conceptual model behind Axon's turn loop and ownership boundaries.
---

An Axon agent is best understood as a **stateful observation loop**.

The model never directly edits a file, starts a server, or searches a repository. It emits a structured request for a named capability. The runtime executes that capability, records what happened, turns the result into another observation, and asks the model what to do next.

```text
intent ──▶ model ──▶ proposed action ──▶ runtime/tool ──▶ observation
  ▲                                                       │
  └───────────────────────────────────────────────────────┘
```

The loop ends when the model produces assistant output without another tool request.

## A turn is larger than a model request

This is the first distinction to internalize.

A **model request** is one inference call.

A **turn** starts with one user input and may contain a chain of model requests and tool executions before it settles on a final answer.

That difference affects everything around Axon: UI progress, cancellation, cost accounting, tracing, persistence, and testing should usually be organized around the turn rather than assuming one user message equals one API call.

## The model is a decision engine, not the executor

The model sees descriptions of tools. It does not receive their Go functions. This creates a clean separation:

- the **model plane** decides what action would move the task forward;
- the **execution plane** decides what actually happens on the machine;
- the **session** carries observations between them.

A custom model can therefore be swapped without rewriting tools, and a custom tool can be added without teaching the model transport layer how to execute it.

## State is part of the runtime

Agentic work is not just chat history. During a coding task the runtime also needs a working directory, a task plan, edit history, block identifiers, and live background-process ownership.

Axon groups the durable part of that working state into a session. The next model request is then built as a **projection** of session history rather than a blind replay of every byte ever observed.

That projection boundary is what makes context management possible without treating deletion as memory management.

## Failure is usually another observation

Ordinary tool failures are fed back to the model as tool results. This is intentional: “file not found”, a failing test, or a rejected replacement is often information the agent can use to recover.

Failures of the runtime boundary—construction errors, unrecoverable model errors, session-save failure at the beginning of a turn, parent cancellation—are different. Those escape the loop as Go errors.

## Runtime policy is frozen into an agent

Operational settings are resolved when an agent is constructed. Tools receive their limits; the agent receives retry/context policy; the session location is resolved. Axon does not repeatedly reload `axon.yaml` while a turn is executing.

That means one process can host differently configured agents without a mutable global configuration plane.

## A useful design test

When deciding whether something belongs in Axon, ask: **is this execution machinery common to many agent products, or product policy specific to one application?**

The former belongs in the runtime. The latter belongs in the embedder.
