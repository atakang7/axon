---
title: Security model
description: What Axon's internal capability boundaries do—and do not—protect.
---

The correct security model for Axon is: **the agent has the operating-system authority of the process you run it in**.

Axon contains useful internal capability boundaries and cost controls, but it is not a filesystem sandbox, shell sandbox, network sandbox, or secret-filtering system.

## Working directory is routing, not confinement

Built-in file/search/exec operations share a working directory so relative paths have consistent meaning.

Absolute paths are still accepted and traversal is not blocked. The working directory therefore answers “where is relative?” rather than “where is the model allowed to go?”

## Shell execution is real execution

The exec capability runs commands with `sh -lc` under the Axon process identity. Stdin is detached from an interactive terminal and process groups are used for cleanup, but the command otherwise has the machine/container permissions you granted the process.

If a model must not read a path, connect to a network, or invoke a binary, remove that authority outside Axon.

## MCP extends the trust boundary

An MCP server is a spawned subprocess. It inherits the process environment plus any configured entries and can expose arbitrary capabilities through its own tools.

Connecting an MCP server is therefore equivalent to adding executable application code to the agent's capability set, not merely adding documentation.

## Internal least-authority still matters

Axon's built-in tools are constructed with narrower values/interfaces instead of the entire agent or settings tree. In particular, operational tool limits do not carry provider credentials into built-in implementations.

This reduces accidental coupling and secret exposure inside the runtime, even though it does not isolate the process from the host.

Custom tool functions are your code: they may capture any value you choose to give them.

## Limits protect resources, not confidentiality

Byte, timeout, tail, and match settings help prevent one action from consuming unbounded latency/memory/context. The binary-read heuristic avoids loading obviously useless binary content into model context.

None of those mechanisms redact secrets or classify sensitive data.

## Practical deployment rule

Run Axon inside the smallest host/container identity that is acceptable for the agent. Scope filesystem mounts, credentials, network access, and process privileges there. Then use Axon's tool selection and limits as a second layer for runtime ergonomics and cost control.
