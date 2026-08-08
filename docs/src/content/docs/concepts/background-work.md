---
title: Background work
description: The foreground/background execution model for commands whose lifetime does not fit a turn call.
---

Shell commands have two very different shapes:

- **bounded work**: format, build, test, inspect a file;
- **open-ended work**: servers, watchers, long network clients, anything waiting on sockets or external state.

Axon gives those shapes different lifecycles instead of forcing every command through one timeout path.

## Foreground means “this should terminate”

A foreground exec belongs to the current tool call. Axon waits for it, captures bounded output, applies a timeout, and returns an exit summary to the model.

This is appropriate when completion itself is the observation the model needs.

## Background means “start ownership, observe later”

A background exec returns a shell handle immediately. Output goes to a log file owned by that agent's shell registry. The model later asks for new output or explicitly stops the process.

The important semantic shift is that the tool call no longer represents the process lifetime. It represents **acquiring a handle to a live resource**.

## Polling returns deltas

`bash_output` advances a per-shell read offset and returns only newly appended log data. That avoids paying model-context cost for the same startup logs every time a server is polled.

If a backlog is larger than the selected byte budget, Axon keeps the newest portion and advances past dropped bytes: the next poll continues from “now,” not from the discarded backlog.

## Processes belong to one agent

Each agent has its own shell registry. Closing or resetting one agent does not intentionally reach into another agent's registry.

`Reset` kills live background shells because a “fresh” session should not inherit servers from the conversation it just wiped. `Close` also kills them as part of resource cleanup.

## Process groups make cleanup practical

Commands run through a shell and may spawn children. Axon puts the command in a process group so termination can target the group instead of only the shell wrapper.

This improves cleanup but does not turn arbitrary process trees into a perfect sandbox; children that deliberately escape the process group can still create edge cases.

For concrete usage, see [Run background processes](/axon/guides/background-processes/).
