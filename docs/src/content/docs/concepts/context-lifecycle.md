---
title: Context lifecycle
description: Axon's working-set model: durable history, recency collapse, and optional semantic parking.
---

The central idea of Axon's context system is: **history is not the same thing as working memory**.

A long-running agent may need a durable record of everything it observed, while the model only needs a small subset in full detail to continue the current task. Axon treats those as separate layers.

## Layer 1: durable history

The session keeps message content and metadata on disk. This is the record of what happened.

Context management does not begin by deleting that record.

## Layer 2: the active projection

Before a primary-model request, Axon builds a fresh projection of session messages.

Recent active blocks remain expanded. Older active blocks outside the configured recency window are replaced in that request by compact breadcrumbs. This reduction is cheap: it requires no secondary model call and does not permanently mark the block as forgotten.

Think of it as keeping the newest working set on the desk while older material stays in the archive.

## Layer 3: semantic parking

When a pruner model is configured, Axon can ask it to judge old blocks that are already outside the recency window. Blocks it selects are marked as parked and continue to project as breadcrumbs on future requests.

This is qualitatively different from the recency window:

- **windowing** says “old enough not to expand right now”;
- **parking** says “judged unnecessary to continue this task.”

Parking is one-way in the current runtime. The original content remains stored, but there is no public unpark operation that returns it to active model context.

## Why use both layers?

Recency is free and predictable but semantically blind. A pruner can distinguish stale exploration from old-but-important context, but it costs latency/tokens and can make mistakes.

Putting recency first means the secondary model only reasons about already-old material instead of spending tokens re-evaluating the recent working set.

## Protocol coherence matters

Tool calls and tool results form protocol pairs. If the assistant message that initiated a tool call is collapsed, Axon also removes the corresponding tool-result message from the model projection rather than leaving an orphaned result.

This is a useful example of the difference between “shorten context” and “preserve a valid model conversation.” Context management has to do both.

## The task plan is injected separately

When a session has an active task plan, its rendered task block is appended to the projected context as a system message. That gives the current plan a stable place in working memory even as ordinary historical blocks age out.

## A prune pass can succeed with rejected IDs

The curator is a model, so it may name a protected, already-parked, or invented block ID. Axon rejects IDs it cannot legitimately park while still parking the valid IDs from the same response.

Those rejects do **not** turn an otherwise useful prune pass into an error. They are surfaced on the successful `prune_end` event as `PruneInfo.Rejected`, then cleared from the pruner's pending report.

Actual curator failures—request errors, timeout, malformed output, or session-save failure—still produce the pruning-error path and the primary turn continues.

## Protection is partly code, partly policy

The runtime itself protects the newest active user and assistant blocks from the pruner path and refuses to park system messages. The pruner prompt asks for additional semantic safety rules and forbids naming IDs it was not shown, but those remain model instructions rather than equivalent hard checks for every historical block.

For operational tuning, see [Context & pruner configuration](/axon/configuration/pruner/).
