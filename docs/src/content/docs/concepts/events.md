---
title: Events
description: Agent lifecycle and streaming callbacks.
---

Pass `Config.OnEvent` to receive `Event` values during a turn.

```go
OnEvent: func(_ context.Context, e axon.Event) {
    if e.Kind == axon.KindToken {
        fmt.Print(e.Text)
    }
},
```

| Event kinds | Payload |
|-------------|---------|
| `KindToken`, `KindReasoning`, `KindAssistantEnd` | `Text` |
| `KindToolCall`, `KindToolResult`, `KindToolError` | `Tool` |
| `KindPruneStart`, `KindPruneEnd` | `Prune` |
| `KindUsage` | `Usage` |
| `KindError` | `Err` |
| `KindSessionStart`, `KindSessionEnd` | `Session` |
| `KindTurnStart`, `KindTurnEnd`, `KindAPICall` | `Kind` |

## Limits

- Check `Kind` before reading an optional payload.
- Ignore unknown kinds to remain compatible with newer releases.
