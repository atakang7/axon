---
title: Events & observability
description: Axon's runtime event stream as a UI and tracing surface.
---

Axon emits structured events so an application can observe a turn without parsing assistant text or tool output heuristically.

The event stream covers the major boundaries of execution:

- session start/end;
- turn start/end;
- user input;
- model API calls;
- streamed text and reasoning;
- streamed tool arguments;
- resolved tool calls and results/errors;
- pruning start/end;
- retry/info/error signals.

## Events are a live trace, not the source of truth

Session state is persisted separately. Events tell an observer **what the runtime is doing now**; they are not the storage mechanism from which Axon reconstructs the session later.

This makes the same event surface useful for several consumers:

- a terminal or web UI showing streaming progress;
- structured logs;
- tracing spans and metrics;
- debugging/recording test harnesses.

## Streaming and resolved events serve different jobs

Token/reasoning/tool-argument deltas are incremental and suited to user feedback.

A resolved tool-call event carries the complete argument payload that is about to execute. Treat that as the stable boundary for auditing actions; a partial argument stream is only presentation/progress data.

## Delivery is synchronous

The callback runs directly in Axon's execution path. There is no internal telemetry queue.

That is simple and deterministic, but it gives the handler backpressure over the agent. A handler that blocks on a remote log sink can slow token streaming and turn progress.

Production observers should usually hand events off to their own bounded queue or fast in-process recorder.

## Events inherit turn context

When Axon emits an event without an explicit timestamp or turn number, it fills those fields from the current time/session before invoking the callback.

See [Events reference](/axon/reference/events/) for the complete event/payload map and [Build observability](/axon/guides/observability/) for integration patterns.
