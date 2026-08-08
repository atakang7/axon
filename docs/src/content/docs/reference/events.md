---
title: Events
description: Event kinds, payload ownership, timestamps, and turn metadata.
---

## Kinds

```text
unknown
session_start
user_input
turn_start
api_call
token
reasoning
assistant_end
tool_arg_delta
tool_call
tool_result
tool_error
turn_end
prune_start
prune_end
info
error
session_end
```

`Kind.String()` returns these names. An out-of-range value renders as `kind(N)`.

## Event structure

```go
type Event struct {
    Kind    Kind
    Turn    int
    Time    time.Time
    Text    string
    Tool    *ToolEvent
    Prune   *PruneInfo
    Err     error
    Session *SessionInfo
}
```

Before invoking the callback, Axon fills zero `Time` with `time.Now()` and zero `Turn` from the current session when available.

## Payload map

| Kind | Relevant payload |
| --- | --- |
| `session_start`, `session_end` | `Session` |
| `user_input` | `Text` |
| `turn_start`, `turn_end`, `api_call` | base fields |
| `token`, `reasoning`, `assistant_end`, `info` | `Text` |
| `tool_arg_delta`, `tool_call`, `tool_result` | `Tool` |
| `tool_error` | `Tool`, `Err` |
| `prune_start`, `prune_end` | `Prune` |
| `error` | `Err`, optionally `Text` |

## `ToolEvent`

```go
type ToolEvent struct {
    ID        string
    Name      string
    Args      json.RawMessage
    ArgsDelta string
    Result    string
    BlockID   string
}
```

`ArgsDelta` is only meaningful during streamed argument deltas. Full `Args` appears at resolved call time. `Result`/`BlockID` appear on successful tool-result emission.

## `PruneInfo`

```go
type PruneInfo struct {
    Before   int
    After    int
    Rejected []string
}
```

`Before` and `After` are approximate projected token counts from the pruner's character/4 estimator.

`Rejected` is populated on successful `prune_end` events with block IDs the curator named but Axon could not park—for example protected, already-parked, or invented IDs. Rejected IDs are telemetry, not a failure of the pass; valid IDs from the same pass may already have been parked.

The pruner accumulates rejected IDs until `Rejected()` is read for the event, then clears that report.

## `SessionInfo`

Contains session ID, Cwd, path, provider/model strings, and whether pruning is on.

Current agent construction does not populate provider/model because the generic `Model` interface exposes no common metadata method.

## Delivery

`OnEvent` is invoked synchronously by `emit` on the executing goroutine. Unknown future `Kind` values should be treated as no-ops by consumers according to the public contract.
