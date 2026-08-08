---
title: Events & observability
description: Observe sessions, turns, streaming, tools, pruning, retries, and errors.
---

Pass `OnEvent` in `Config` to receive the runtime event stream.

```go
agent, err := axon.New(axon.Config{
    Model:        model,
    SystemPrompt: "...",
    OnEvent: func(ctx context.Context, e axon.Event) {
        log.Printf("turn=%d kind=%s", e.Turn, e.Kind)
    },
})
```

## The callback is synchronous

`emit` calls `OnEvent` directly on the runtime goroutine. There is no internal event queue.

That means a slow handler adds latency to model streaming, tool execution, and turn progress. If your telemetry sink can block, hand the event off to your own buffered channel/worker.

## Automatic fields

Before delivery, `emit` fills:

- `Time` with `time.Now()` when zero;
- `Turn` from the active session when zero.

Only fields relevant to a particular `Kind` are populated.

## Typical timeline

A tool-using turn usually looks like:

```text
turn_start
user_input
[prune_start → prune_end]
api_call
reasoning / token / tool_arg_delta ...
tool_call
tool_result | tool_error
api_call
...
assistant_end
turn_end
```

Retries emit `info` before the next attempt and `error` for the failed request.

## Session events

`New` emits `session_start` after constructing the agent. `Close` emits `session_end` before resource cleanup.

The current `New` implementation leaves `SessionInfo.Provider` and `SessionInfo.Model` blank because the generic `Model` interface has no standard metadata method. An embedder that knows those values should attach them in its own telemetry context.

## Streaming tool arguments

`tool_arg_delta` carries fragments while the provider is still streaming a function call. `tool_call` later carries the complete raw JSON arguments.

This separation is useful for UI: deltas can animate progress, while only the resolved call should be treated as executable input.
