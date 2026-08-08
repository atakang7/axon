---
title: Sessions & workspaces
description: How Axon separates durable history, working state, project location, and recovery state.
---

A session is best understood as an **agent checkpoint**, not a chat transcript.

It stores the minimum durable state Axon needs to resume the same line of work:

```text
conversation history
workspace location
turn + block identity
current task plan
edit recovery data
```

Those pieces solve different problems, and keeping them together gives a restarted agent enough continuity to continue rather than merely remember what was said.

## Session identity and workspace location are different

When no path is pinned, Axon chooses a stable session file from the **process working directory at construction time**.

`Config.Cwd` is applied later and controls where relative tool paths and shell commands operate.

So these are separate questions:

- **Which conversation/checkpoint am I resuming?** → session path.
- **Where should tools operate right now?** → session `Cwd`.

This matters for embedders that open one stored session while intentionally pointing tools at another directory, and for tests that need deterministic state placement.

If session identity must be explicit, pin the path rather than relying on cwd-derived naming.

## A workspace gives location, not confinement

Relative file/search/exec paths resolve against the session working directory. Absolute paths stay absolute and traversal is not blocked.

That makes `Cwd` a routing primitive: it establishes what `./foo` means. It is **not** an authorization primitive defining what the model may access.

If the agent must be confined, enforce that at the process/container/VM boundary.

## Durable history is not model working memory

The session retains historical message content. The model sees a **projection** built from that history.

That distinction lets Axon collapse or park old blocks to reduce request context without making persistence itself lossy.

Use the two layers for different questions:

- Session history: *what happened?*
- Context projection: *what must the model see now to continue?*

See [Context lifecycle](/axon/concepts/context-lifecycle/) for the projection model.

## Task state is a durable hint, not a workflow engine

The session may hold one goal plus ordered task steps. That state is rendered back into model context so a multi-step objective remains explicit even as ordinary history ages out.

Axon does not execute steps, schedule them, or guarantee the model follows them. Registering or advancing a task only changes durable guidance presented to the model.

The useful rule is simple: use task state when **losing the current objective would make recovery expensive**. A one-shot question does not need it; a repository migration or multi-stage investigation often does.

This is why task state belongs conceptually with the session rather than deserving a separate workflow abstraction.

## Edit history is recovery state, not version control

Built-in writes record previous file contents. `Undo` restores the most recent recorded bytes and appends a note so the model's understanding follows the filesystem change.

That gives the agent a cheap one-step recovery mechanism for its own edits, but it is intentionally weaker than Git or a transaction system:

- recovery is local and ordered;
- the ledger has a size budget;
- there is no multi-file atomic rollback;
- undoing creation of a new file currently leaves an empty file rather than deleting it.

Use Git for project history. Treat Axon's edit ledger as runtime recovery from the immediately preceding mutation.

## Applications can own session lifecycle

Supplying `Config.Session` gives Axon the exact `*Session` pointer rather than a copy. That lets an embedder decide when and how a session object is created, selected, or wrapped by higher-level application lifecycle logic.

The practical boundary is: **Axon owns mutations to the active runtime session; the embedder may own how that session is chosen and exposed.**
