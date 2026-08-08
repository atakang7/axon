---
title: Sessions & workspaces
description: Durable agent state, project affinity, edit history, and the workspace boundary.
---

A session is Axon's **durable working record for one agent thread**. It is more than a chat transcript.

A session carries:

- conversation messages;
- the current working directory;
- a turn counter;
- stable block identifiers used by context management;
- a task plan when one exists;
- pre-edit file contents used by undo.

This lets an agent process stop and resume without reducing its state to “whatever messages fit in memory.”

## Project affinity comes from the process directory

When no session path is pinned, Axon derives a stable filename from the process current working directory. Two different projects therefore get different default session files, including two same-named directories at different absolute paths.

There is a subtle distinction between **session identity** and **tool working directory**: `Config.Cwd` changes where tools operate after the session has already been selected. It does not change which default session file was chosen.

If you need exact storage placement, pin the session path instead of relying on this derivation.

## The workspace is a base directory, not a jail

Relative tool paths are resolved against the session working directory. Absolute paths remain absolute, and traversal components are not rejected.

So the workspace gives tools a shared notion of “here”; it does not confine them to “here.” Security isolation must be enforced around the process.

## History and working context are different

Session messages are the durable record from which Axon derives the next model-visible context. Old content can be collapsed in that projection without deleting the original recorded content.

That separation is fundamental: persistence answers “what happened?” while context answers “what does the model need in front of it now?”

## Edits create a lightweight recovery path

Built-in writes record the previous file contents before mutation. `Undo` uses the most recent record to restore those bytes and tells the model the revert happened.

This is intentionally a small local edit ledger, not version control. It does not replace Git, transactions across multiple files, or semantic rollback.

A newly created file records an empty prior value, so undoing its creation currently writes an empty file rather than deleting the path.

## Session ownership is explicit

You may supply your own `*Session` to `Config`. Axon reuses that same pointer rather than copying it. This is the hook for applications that want to own session construction or integrate other storage/lifecycle logic around the runtime.
