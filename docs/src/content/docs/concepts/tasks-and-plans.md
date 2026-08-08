---
title: Tasks & plans
description: How Axon makes multi-step intent durable without turning the runtime into a workflow engine.
---

The built-in task capability gives an agent a small piece of **explicit plan state**.

It exists for work where “remember what step I am on” should not depend entirely on the model reconstructing a plan from conversation history.

## A task has one goal and ordered steps

The stored task contains:

- a goal;
- an ordered list of step descriptions;
- completion state per step;
- an index pointing at the current step.

The tool can register a plan, advance it, or replace it when reality no longer matches the plan.

## The plan is state, not control flow

Axon does not execute task steps itself. Advancing a plan does not invoke a function associated with that step, schedule a job, or guarantee the model follows the plan.

Instead, the current task is rendered back into model context. The model still decides which tool call or answer should happen next.

That makes the task system closer to a durable checklist than a workflow engine.

## Why the tool is write-oriented

The task capability exposed to the built-in tool is intentionally focused on mutations: register, advance, replan. Rendering the full task into the model prompt stays the runtime's responsibility.

This avoids having two competing code paths deciding how task state should be presented.

## Plans survive the same way sessions do

Task state is part of the session JSON, so it survives process restarts and participates in reset with the rest of the session.

## Use it when state helps

A one-shot question does not need a plan. A repository migration, investigation, or multi-stage coding task can benefit from one because the current objective remains explicit even when ordinary context is collapsed.

The implementation recommends a small number of short steps, but that is guidance rather than a hard workflow constraint.
