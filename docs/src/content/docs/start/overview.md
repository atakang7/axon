---
title: Overview
description: What Axon is, where it sits, and what it deliberately leaves to your application.
---

Axon is an **agent execution runtime**. It is not a chatbot framework, CLI, workflow product, or provider SDK collection.

A tool-using model by itself can only produce two things: text and requests to call tools. A useful agent also needs a place to keep state, a disciplined way to execute those requests, a way to feed observations back to the model, limits around expensive operations, and lifecycle control when work runs for minutes instead of milliseconds. Axon is that layer.

## The boundary

Your application decides **what the agent is for**:

- which model to use;
- what system prompt defines its role;
- which tools it should have;
- how users interact with it;
- which provider/model is selected;
- what telemetry should be recorded;
- what host/container permissions the process receives.

Axon decides **how one agent instance runs**:

- how turns repeat model → tool → model;
- how the conversation and workspace state persist;
- how old context is projected under pressure;
- how built-in tools read, write, search, and execute;
- how long-running commands are owned and polled;
- how retries, interruption, and cleanup behave;
- which runtime events are emitted.

This division is why Axon is useful as a library: the application remains opinionated; the runtime remains reusable.

## The three contracts

You can understand nearly the whole public surface as three contracts:

**Model** — takes messages and tool descriptions, returns assistant text and/or tool calls.

**Tool** — exposes a name, description, JSON Schema, and executable function.

**Agent** — owns the loop that repeatedly passes observations between the model and those tools while maintaining a session.

Everything else supports those contracts: settings control operational policy, sessions carry durable state, events expose execution, and the pruner manages context pressure.

## Configuration is not one thing

Axon deliberately separates two concerns that are often collapsed into a single config object:

- **Agent wiring (`axon.Config`)** answers “what objects make up this agent instance?”
- **Operational settings (`axon.Settings`, optionally loaded from `axon.yaml`)** answer “what policies and limits should those objects use?”

`axon.New` consumes both, but it does **not** load files or choose a provider for you. This distinction is central to the configuration manual.

## What Axon does not promise

Axon's working directory is not a filesystem sandbox. Shell execution is real shell execution. MCP servers are real subprocesses. Tool output limits are cost/latency controls, not data-loss-prevention filters.

If the model must be isolated from the machine, enforce that with the OS, container, VM, credentials, and network boundary around the Axon process.

## Where to go next

- Want to run something now? [Quickstart](/axon/start/quickstart/).
- Want the mental model first? [The runtime model](/axon/concepts/runtime-model/).
- Want to edit configuration? [Configuration model](/axon/configuration/).
