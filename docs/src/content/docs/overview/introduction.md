---
title: Introduction & Design
description: Core mental models and design philosophy of the Axon runtime.
---

Axon is a deterministic Go runtime for orchestrating Large Language Model (LLM) agents. 

Unlike Python-based frameworks that optimize for rapid prototyping via dynamic typing and unbounded agent-to-agent communication, Axon is designed as a foundational backend library. It provides strict concurrency guarantees, explicit state management, and robust observability for production environments.

## The Mental Model

At its core, an LLM agent is a while-loop executing a non-deterministic state machine. The model observes state, plans an action, executes a tool, and observes the result.

Axon manages this loop on your behalf. You provide the model client, the system prompt, and the tool definitions. Axon handles the execution loop, token stream multiplexing, tool dispatching, and context window pruning.

## Core Guarantees

If you are building atop Axon, you can rely on the following invariants:

1. **Deterministic State Mutation (Append-Only):**
   Axon's session memory is strictly append-only. The conversation state is a projection of a linear event log. This guarantees that operations like `/undo` are byte-exact rollbacks, and every user session yields a perfect audit trail.

2. **Turn-Scoped Concurrency:**
   A single user turn (`ag.Step`) runs under a single `context.Context`. If the user cancels the request or a timeout is reached, the context cancellation propagates synchronously to the HTTP stream and all concurrent tool execution subprocesses.

3. **Stateless Configuration:**
   Agent settings are loaded once at instantiation and never mutated. This allows you to safely multiplex hundreds of structurally divergent agents (e.g., varying temperatures, varying tools) within a single OS process without race conditions or shared state bleed.

4. **Monolithic Orchestration:**
   Axon intentionally omits multi-agent delegation (where sub-agents chat with sub-agents). Multi-agent patterns historically suffer from cascading latency and catastrophic error loops. Instead, Axon manages context scale using a secondary *pruner model* that asynchronously parks stale memory.
