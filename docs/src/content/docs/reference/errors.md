---
title: Errors
description: Public sentinel errors, provider errors, retry classification, and notable non-sentinel failure behavior.
---

## Agent sentinels

```text
ErrNoModel
ErrNoSystemPrompt
ErrToolNotFound
ErrDuplicateTool
ErrInvalidTool
ErrInterrupted
```

### Current usage notes

- `ErrNoModel` — `New` with nil model.
- `ErrNoSystemPrompt` — `New` with empty prompt.
- `ErrInvalidTool` — custom/MCP tool missing required `Name`, `Schema`, or `Fn`.
- `ErrDuplicateTool` — name collision in final toolset.
- `ErrInterrupted` — active turn canceled through turn context.
- `ErrToolNotFound` is exported, but current dispatch of a model-requested unknown tool returns a normal `role=tool` message containing `tool not found` rather than returning this sentinel from `Step`.

## Configuration sentinels

```text
ErrMissingConfig
ErrMissingEnv
ErrInvalidConfig
ErrUnknownProvider
ErrUnknownModel
```

Loader/resolver errors wrap these where appropriate, so use `errors.Is` rather than matching strings.

## `APIError`

Non-2xx responses from Axon's shipped client become:

```go
type APIError struct {
    Status int
    Body   string
}
```

The body is capped at 4096 bytes. The agent uses `Status` for configured retry classification.

## Tool errors

A `Tool.Fn` error is normally **not returned from `Step`**. The runtime emits `tool_error` and appends a `role=tool` message whose content is `err.Error()`, then the model gets another chance to reason.

## Session persistence errors

At the start of a turn, failure to save the appended user message aborts `Step`.

After successful assistant/tool appends, save failure is emitted as an `error` event with text `session not persisted` and the turn continues.

## Pruner errors

Pruner failures are emitted with text `prune skipped`; the primary turn continues.

## Empty model response

A `nil`/empty-content/no-tool-call-style empty reply path is treated as `empty response from model`. It is given one retry before failing.
