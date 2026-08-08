---
title: Models & providers
description: The distinction between a Model runtime contract and provider configuration.
---

Axon deliberately separates **the thing the runtime can ask for a completion** from **the address book used to construct one**.

## Model is the runtime abstraction

A `Model` is anything that can accept a request containing messages and tool descriptions and return an assistant message.

This is the abstraction the agent loop depends on. It can represent:

- Axon's shipped OpenAI-compatible HTTP client;
- a gateway inside your company;
- an adapter to a non-OpenAI protocol;
- a deterministic fake used by tests.

The runtime does not need to know which category it is talking to.

## Providers are construction data

The `providers` section in `Settings` is not consulted on every agent turn. It is data used by `Settings.Provider` / `Settings.NewClient` to construct the shipped client.

That means providers do not select themselves and `axon.New` never says “use the first configured model.” Your application owns selection.

This is useful in applications with a model picker, routing policy, feature-specific models, or separate primary/pruner models: configuration lists what is available; application logic decides what becomes a `Model`.

## One protocol ships

Axon's built-in client speaks streamed OpenAI-style chat completions. It serializes messages and function tools, accepts text/reasoning/tool-call deltas, and assembles the final assistant message.

If an endpoint speaks that wire format, it can generally be represented with `Provider` data. If it does not, adapt it behind the `Model` interface rather than adding provider-specific branches to the turn loop.

## Primary model and pruner model are separate roles

The primary `Config.Model` performs the user's task.

`Config.Pruner`, when supplied, is a second `Model` used only to decide which old context blocks can be permanently parked. It can therefore be cheaper or faster than the primary model.

The pruner is optional. Recency-window context reduction still happens without one.

## Settings do not magically reconfigure an existing Model

This is an important operational boundary. `Settings.Model` fields are applied by `Settings.NewClient` when that helper constructs Axon's shipped client. Passing a separately constructed `Model` to `axon.New` does not cause Axon to reach inside that object and apply request timeout, reasoning, or token settings.

Think of `Settings.NewClient` as the bridge between provider/model settings and one concrete `Model` object.
