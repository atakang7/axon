---
title: Context & pruning
description: How Axon reduces model-visible context without deleting recorded content.
---

Axon has two independent context-reduction mechanisms:

1. a **free recency window** applied every request;
2. an optional **model-assisted pruner** that permanently marks older blocks as parked.

## Historical content is preserved

Parking changes metadata (`Parked`, `ParkSummary`, `ParkReason`) on a `Msg`, but it does not erase that message's original `Content` from the session file.

The model sees a derived projection built by `Session.ContextMessages`, not the stored slice directly.

## Free recency window

`PrunerSettings.WindowBlocks` controls how many recent active message blocks remain expanded.

For active blocks outside that window, Axon generates a breadcrumb at request time:

```text
[#m12 parked | reason: outside recency window | gist: tool:read: ...]
```

This is **not persisted parking**. If a block later falls back inside the active window calculation, its full stored content can be projected again.

Already-parked blocks do not consume window budget.

## Tool-call coherence

If an assistant message containing tool calls is collapsed by the window or by persisted parking, Axon records those tool-call IDs and omits the corresponding `role=tool` messages from the projection.

That prevents the provider from receiving a tool result whose initiating assistant tool call is no longer present.

## When the pruner runs

A configured `Pruner` estimates projected tokens as approximately `characters / 4`.

It fires only when:

- projected tokens are at least `floor_tokens`; and
- either it has never fired, or at least `growth_tokens` have accumulated since the previous attempt.

The estimate already includes recency windowing, so the paid curator is not triggered by history that the free projection has already collapsed away.

## What the curator sees

The pruner model receives:

- a system prompt describing its mode and output contract;
- the current task block;
- only active message blocks that are outside the recency window;
- at most 2,000 characters of each candidate block.

It returns JSON containing numeric block IDs:

```json
{"park":[3,7,9]}
```

The runtime converts those to `m3`, `m7`, and `m9` and calls `Session.Park`.

## Hard guards vs prompt guidance

This distinction is important.

**Enforced by code:**

- a system message cannot be parked by `Session.Park`;
- the most recent active user message is rejected by the pruner path;
- the most recent active assistant message is rejected by the pruner path.

**Requested from the curator prompt, but not independently enforced for every historical block:**

- do not park user messages;
- do not park unresolved errors/failing tests;
- do not park edited-file context;
- do not park content needed for quoting.

The code-grounded safety boundary is therefore narrower than the natural-language curator policy.

## Failure behavior

Pruning never fails the user turn. Network errors, timeouts, malformed curator output, unusable IDs, or save failures are returned from `Prune`; `Step` emits an error and continues.

A failed attempt still updates the pruner's `lastFire` baseline to the pre-prune context size, preventing a broken curator from being retried on every tool-loop iteration.

## Modes

`low`, `moderate`, and `extreme` change the natural-language threshold given to the curator. They do not change the hard protected-ID logic or the recency-window algorithm.
