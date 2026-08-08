---
title: Runtime invariants
description: Design properties contributors should preserve when changing Axon.
---

These are not style preferences; they are properties encoded by the current runtime structure.

## Settings are resolved at construction

`New` applies defaults once and stores the resolved `Settings`, `Limits`, exclusion list, and toolset on the agent. Built-ins do not re-read operational environment variables per call.

Preserve this if multiple differently configured agents in one process should remain possible.

## Model code cannot reach tool implementations through Request

A `Tool` is projected to `ToolSpec` before `Model.Complete`. Provider/client implementations receive schema/description/name, not `Fn`.

That keeps model transport separate from execution capability.

## Built-ins receive least runtime capability

Tools are built against `Workspace`, `Plan`, shell registry, and `Limits`, not `*Agent` or provider settings.

Adding a convenient dependency to a tool can accidentally widen its authority. Prefer a smaller interface/value.

## Stored content and projected context are different things

Do not prune by deleting message content just to make the next request smaller. The current design preserves original content and changes the model projection in `ContextMessages`.

Persisted parking is metadata; recency collapse is a transient view.

## Tool-call protocol pairs must stay coherent

When an assistant tool-call message is collapsed, matching tool-result messages are omitted from the projected request. A context optimization must not leave orphaned protocol messages.

## Pruning is best-effort

The user turn must continue when the curator is unavailable or produces unusable output. `Step` treats pruning failures as emitted errors, not turn failures.

## Tool failure is model-visible

A tool operation error becomes a `role=tool` message so the model can recover. Reserve `Step` errors for failures of the turn/runtime boundary rather than ordinary tool-domain failures.

## Process resources belong to one Agent

Background shell registries and MCP client lists are instance state. Cleanup should not reach into another agent's resources.

## Session history is saved incrementally

The user message is persisted before the first model call. Assistant/tool messages are saved as they are appended. That limits how much state a process crash can lose.

## The working directory is not a sandbox

Path resolution is convenience routing, not isolation. Do not document or refactor it as a security boundary without adding enforcement to the executable paths.

## Public operational errors should stay classifiable

Loader/construction errors wrap exported sentinels where the code currently promises `errors.Is` classification. Preserve that boundary when adding context to error messages.
