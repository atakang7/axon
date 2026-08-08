---
title: Context Management
description: How Axon keeps the model's context window bounded.
---

Context grows without bound during long sessions. Axon has two lines of defense.

```
Session.Messages (immutable historical log)
┌────┐┌────┐┌────┐┌────┐┌────┐┌────┐┌────┐┌────┐
│ m1 ││ m2 ││ m3 ││ m4 ││ m5 ││ m6 ││ m7 ││ m8 │
└─┬──┘└─┬──┘└─┬──┘└─┬──┘└─┬──┘└─┬──┘└─┬──┘└─┬──┘
  │     │     │     │     │     │     │     │
  ▼     ▼     ▼     ▼     ▼     ▼     ▼     ▼
ContextMessages() derives what the model sees:

OUTSIDE WINDOW          │      INSIDE WINDOW
(breadcrumbs)           │      (full content)
┌───────────┐           │     ┌────┐┌────┐┌────┐
│[#m1 gist] │           │     │ m6 ││ m7 ││ m8 │
│[#m2 gist] │           │     │full││full││full│
│[#m3 park] │ ◄─parked  │     └────┘└────┘└────┘
│[#m4 gist] │           │
│[#m5 gist] │           │
└───────────┘           │
```

## Key insight

**Parking is a projection, not a mutation.** `Session.Messages` is an immutable log. `ContextMessages()` is a view computed on every model call. The historical record is never altered.

## Line 1: Recency Window (free)

The `window_blocks` most recent active blocks are shown in full. Everything older is collapsed to a one-line breadcrumb:

```
[#m1 parked | reason: outside recency window | gist: user: List the files]
```

No model call. No cost. Blocks that age out are automatically back to full content the moment they re-enter the window (e.g., the pruner parks enough that the window recedes).

**Configuration:** `pruner.window_blocks` (default: 30)

## Line 2: Pruner (paid)

A secondary model reviews blocks outside the window and decides which to **park**. Parking replaces the full block with a breadcrumb in the model's view. The full content stays in the session log.

### When does the pruner fire?

```
context_tokens >= floor_tokens
AND (first fire OR context_tokens - last_fire >= growth_tokens)
```

| Setting | Default | Purpose |
|---------|---------|---------|
| `floor_tokens` | 10,000 | Below this, not worth a round trip |
| `growth_tokens` | 5,000 | Accumulate before re-prune |

### Modes

| Mode | Behavior |
|------|----------|
| `low` | Park only unambiguous dead weight. `{"park":[]}` is the expected outcome. |
| `moderate` | Park what is clearly no longer in play. **(default)** |
| `extreme` | Park everything not carrying current direction forward. Agent can re-read files. |

All modes share the same safety rules. A stricter mode moves the threshold for "still needed" — not the protection list.

### Never parked (any mode)

- User messages
- Most recent assistant message
- Blocks with unresolved errors
- Blocks naming files the agent has edited
- Blocks the agent needs to quote

### Enabling the pruner

```go
prunerModel, _ := axon.OpenAI(axon.ClientConfig{
    Provider: axon.Provider{
        BaseURL: "https://openrouter.ai/api",
        Model:   "qwen/qwen3.6-flash",    // cheap, fast, long-context
        APIKey:  os.Getenv("OPENROUTER_API_KEY"),
    },
})

agent, _ := axon.New(axon.Config{
    Model:        mainModel,
    Pruner:       prunerModel,
    SystemPrompt: "...",
})
```

### Runtime mode switching

```go
agent.SetPruneMode(axon.PruneExtreme)
agent.SetPrunerModel(differentModel)
```

### Pruner events

| Event | Info |
|-------|------|
| `KindPruneStart` | `PruneInfo.Before` — token count before |
| `KindPruneEnd` | `PruneInfo.After` — token count after. `PruneInfo.Rejected` — block IDs the curator named but couldn't park |

A steady stream of `Rejected` IDs means the curator is naming blocks it was never shown. Not a failure — but worth monitoring.

### How parking works internally

1. The pruner model receives the session log (outside the window only) and a task summary
2. It returns `{"park": [3, 7, 9]}` — block IDs to park
3. For each ID: if not protected and not already parked, `session.Park(id, gist, reason)` sets `Parked=true`
4. `ContextMessages()` sees `Parked=true` and emits a breadcrumb instead of the full content
5. Tool calls on parked assistant messages are dropped, along with their orphaned tool results
