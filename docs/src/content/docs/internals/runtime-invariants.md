---
title: Runtime invariants
description: Properties contributors should preserve unless intentionally redesigning the runtime contract.
---

## Construction resolves operational policy

An agent should not depend on repeated ambient config-file reads during execution. `New` resolves defaults and projects limits once so two agents in one process can differ.

## Model transport does not own execution authority

`Model.Complete` receives `ToolSpec`, not executable `Tool.Fn` values. Preserve that separation between model I/O and machine capability.

## Built-ins receive narrow dependencies

A tool should receive the capability it needs rather than `*Agent` or the full settings tree. Widening those constructor dependencies widens authority and coupling.

## Durable history and active context remain separate

Context pressure should be handled in projection/parking policy, not by casually deleting recorded message content. The durable record and the model working set serve different purposes.

## Tool protocol pairs remain coherent

If an assistant tool-call message is removed/collapsed from a provider request, matching tool-result messages must not be left orphaned in that request.

## Pruning stays an optimization

A curator outage or malformed decision must not make the user's primary task unavailable. Pruning failure is emitted and the turn continues.

## Ordinary tool failure remains model-visible

Domain failures should normally become tool observations so the model can recover. Do not turn every failed command/read/edit into a fatal `Step` error.

## Per-agent resources remain per-agent

Background shells and MCP processes belong to one agent instance. Lifecycle operations on one agent must not terminate another agent's resources.

## Session state is persisted incrementally

The user message is saved before the first model call; subsequent assistant/tool observations are saved as they are appended. Preserve the crash-recovery value of incremental persistence.

## Workspace is not documented as a sandbox without enforcement

The current path/execution layer does not confine operations to `Cwd`. Documentation and future refactors must not imply a security guarantee that executable paths do not enforce.

## Error classification remains stable where exported

Construction/configuration paths wrap exported sentinel errors for `errors.Is` classification. Add human context without breaking that public boundary unless intentionally changing the API.
