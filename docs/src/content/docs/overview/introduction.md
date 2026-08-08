---
title: Introduction
description: What is Axon and why does it exist?
---

Axon is a Go runtime for building LLM agents. 

It drives the autonomous agent loop: streaming a model API, dispatching tool calls, persisting an append-only session, pruning context under token pressure, and emitting structured events at every step.

## Design Principles

- **One LLM, no subagents.** Cost is controlled by a secondary pruner model that parks stale context, not by running parallel agent fleets.
- **Append-only session log.** Memory is a projection, never a mutation. It enables byte-exact undos and preserves full history for audit.
- **Turn-scoped lifecycles.** A single `context.Context` spans one user turn, covering the HTTP stream and all concurrent tool subprocesses.
- **Capability-based tools.** Tools receive narrow interfaces (e.g., `Workspace`, `Plan`). They cannot access ambient state or credentials.
- **Stateless configuration.** Settings resolve at construction. The runtime never re-reads state, allowing multiple diverse agents within a single process.
