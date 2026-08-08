---
title: Events & errors
description: Event kinds, payload fields, API errors, and public sentinel errors.
---

## Event kinds

`Event.Kind` can be:

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

`Kind.String()` returns those names. Out-of-range values render as `kind(<number>)`.

## Event payloads

| Event field | Used for |
| --- | --- |
| `Text` | token, reasoning, assistant_end, user_input, info, error |
| `Tool` | tool_arg_delta, tool_call, tool_result, tool_error |
| `Prune` | prune_start, prune_end |
| `Err` | error, tool_error |
| `Session` | session_start, session_end |

`ToolEvent` carries ID, name, full args, arg delta, result, and resulting message block ID depending on event kind.

`PruneInfo` reports approximate projected token counts before/after a curator run.

## Provider errors

The shipped HTTP client returns `*APIError` for non-2xx responses:

```go
type APIError struct {
    Status int
    Body   string
}
```

`Body` is capped at 4096 bytes.

## Agent sentinel errors

Public sentinels declared by the runtime include:

```text
ErrNoModel
ErrNoSystemPrompt
ErrToolNotFound
ErrDuplicateTool
ErrInvalidTool
ErrInterrupted
```

`ErrNoModel`, `ErrNoSystemPrompt`, `ErrDuplicateTool`, `ErrInvalidTool`, and `ErrInterrupted` are used by current construction/turn paths. `ErrToolNotFound` is exported but the current missing-tool dispatch path returns a normal tool message containing `"tool not found"` rather than returning that sentinel.

## Configuration sentinel errors

The loader/provider resolver declares:

```text
ErrMissingConfig
ErrMissingEnv
ErrInvalidConfig
ErrUnknownProvider
ErrUnknownModel
```

Errors are wrapped with `%w` in the loader/resolver paths, so callers can use `errors.Is` for classification while still retaining the human-readable path/context message.

## Retry classification

The agent retries:

- API status codes present in `RetryConfig.OnStatus`;
- `io.EOF` / `io.ErrUnexpectedEOF`;
- network timeout errors;
- connection refused/reset;
- DNS errors.

It does not retry explicit cancellation/deadline errors. Unknown error types are returned immediately.
