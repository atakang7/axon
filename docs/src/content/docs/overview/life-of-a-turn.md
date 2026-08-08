---
title: Life of a turn
description: Exact Step execution order and failure behavior.
---

`Agent.Step(ctx, input)` is the central state machine.

## 1. Record the user turn

Axon rejects an empty input, increments `Session.Turn`, emits `turn_start`, appends the user message, and saves the session.

That first save is strict: if it fails, `Step` returns an error before calling the model.

It then emits `user_input`.

## 2. Project and optionally prune context

Before **every** model call in the loop, Axon checks `Pruner.ShouldFire` when a pruner exists.

If pruning is due:

- emit `prune_start` with the estimated projected token count;
- run the curator;
- on success emit `prune_end`;
- on failure emit `error` with `"prune skipped"` and continue the turn.

Pruning is an optimization, not a prerequisite for a successful turn.

## 3. Call the model

Axon creates a cancelable turn context, exposes it through `Interrupt`, then calls the model with:

- `Session.ContextMessages(settings.Pruner.WindowBlocks)`;
- the current tool specs;
- streaming callbacks for text, reasoning, and partial tool arguments.

The call is retried according to `RetryConfig` for configured HTTP statuses and for supported transient transport failures such as timeout, EOF/truncated stream, connection reset/refused, and DNS errors.

`context.Canceled` and `context.DeadlineExceeded` are never retried.

## 4. Record the assistant message

A successful model message is appended and Axon attempts to save the session.

Unlike the initial user save, a later persistence failure does not abort the turn; it emits an `error` event with `"session not persisted"`.

If the message contains text, Axon remembers the latest text as `StepResult.Assistant` and emits `assistant_end`.

## 5A. No tool calls → finish

When the message has no tool calls, Axon clears the interrupt handle, emits `turn_end`, and returns:

```go
type StepResult struct {
    Assistant string
    ToolCalls []ToolCall
    Turn      int
}
```

## 5B. Tool calls → execute and continue

For each returned tool call, in order:

1. emit `tool_call`;
2. find the matching tool by name;
3. call its `Fn` with the same turn context;
4. turn success **or failure** into a `role=tool` message;
5. append/save the tool result;
6. prefix the stored tool content with its block ID when one exists;
7. emit `tool_result` on success or `tool_error` from `runTool` on tool failure.

Then the loop goes back to pruning/model invocation with the new tool results in context.

:::note
The shipped OpenAI-compatible client asks providers for `parallel_tool_calls: true`, but Axon's runtime executes returned tool calls sequentially in slice order.
:::

## Interruption

`Interrupt()` atomically cancels the currently active model/tool turn context. If cancellation reaches `Step`, it returns `ErrInterrupted` plus the turn number and any tool calls accumulated so far.

`Run` treats `ErrInterrupted` specially: it continues to the next input instead of failing the whole run.

## Empty model responses

A model response with neither text nor tool calls is treated as an error. Axon gives that condition one retry; a second empty response fails the call even when the general retry policy allows more attempts.
