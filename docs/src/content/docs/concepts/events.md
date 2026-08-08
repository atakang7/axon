---
title: Events
description: The structured event stream for building UIs.
---

Every runtime action emits an `Event` through `Config.OnEvent`. Build any UI — TUI, web, logs — by switching on `Event.Kind`.

## Handler

```go
agent, _ := axon.New(axon.Config{
    // ...
    OnEvent: func(ctx context.Context, e axon.Event) {
        switch e.Kind {
        case axon.KindToken:
            fmt.Print(e.Text)
        case axon.KindReasoning:
            // reasoning tokens (thinking)
        case axon.KindToolCall:
            fmt.Printf("→ %s(%s)\n", e.Tool.Name, string(e.Tool.Args))
        case axon.KindToolResult:
            fmt.Printf("  result [%s]: %s\n", e.Tool.BlockID, e.Tool.Result[:80])
        case axon.KindUsage:
            fmt.Printf("tokens: %d in, %d out\n",
                e.Usage.PromptTokens, e.Usage.CompletionTokens)
        case axon.KindPruneEnd:
            fmt.Printf("pruned: %d → %d tokens\n", e.Prune.Before, e.Prune.After)
        case axon.KindError:
            fmt.Fprintf(os.Stderr, "err: %v\n", e.Err)
        }
    },
})
```

## Event struct

```go
type Event struct {
    Kind    Kind
    Turn    int
    Time    time.Time
    Text    string       // Token, Reasoning, AssistantEnd, UserInput, Info, Error
    Tool    *ToolEvent   // ToolArgDelta, ToolCall, ToolResult, ToolError
    Prune   *PruneInfo   // PruneStart, PruneEnd
    Usage   *UsageInfo   // Usage
    Err     error        // Error, ToolError
    Session *SessionInfo // SessionStart, SessionEnd
}
```

Only the fields relevant to `Kind` are populated. Switch on `Kind` and read the matching payload.

## Kind reference

### Session lifecycle

| Kind | Payload | When |
|------|---------|------|
| `KindSessionStart` | `Session` | Agent constructed |
| `KindSessionEnd` | `Session` | Agent closed |

### Turn lifecycle

| Kind | Payload | When |
|------|---------|------|
| `KindTurnStart` | — | Turn counter incremented |
| `KindUserInput` | `Text` | User message appended |
| `KindAPICall` | — | Model request begins |
| `KindTurnEnd` | — | Turn complete |

### Streaming

| Kind | Payload | When |
|------|---------|------|
| `KindToken` | `Text` | Content token streamed |
| `KindReasoning` | `Text` | Reasoning/thinking token streamed |
| `KindAssistantEnd` | `Text` | Final assistant text |
| `KindToolArgDelta` | `Tool` | Streaming partial tool-call args |

### Tools

| Kind | Payload | When |
|------|---------|------|
| `KindToolCall` | `Tool` | Tool call resolved with full args |
| `KindToolResult` | `Tool` | Tool returned |
| `KindToolError` | `Tool`, `Err` | Tool errored |

### Context

| Kind | Payload | When |
|------|---------|------|
| `KindUsage` | `Usage` | Token accounting for a completed API call |
| `KindPruneStart` | `Prune` | Pruner pass begins |
| `KindPruneEnd` | `Prune` | Pruner pass complete (with before/after/rejected) |

### Informational

| Kind | Payload | When |
|------|---------|------|
| `KindInfo` | `Text` | Informational (e.g. retry backoff) |
| `KindError` | `Err` | Non-fatal error |

## Payload structs

```go
type ToolEvent struct {
    ID        string
    Name      string
    Args      json.RawMessage
    ArgsDelta string           // only for KindToolArgDelta
    Result    string           // only for KindToolResult
    BlockID   string           // only for KindToolResult
}

type PruneInfo struct {
    Before   int
    After    int
    Rejected []string          // block IDs the curator named but couldn't park
}

type UsageInfo struct {
    PromptTokens     int
    CompletionTokens int
}

type SessionInfo struct {
    ID       string
    Cwd      string
    Provider string
    Model    string
    Path     string
    PrunerOn bool
}
```

## Forward compatibility

New kinds may be added in minor versions. Treat unknown kinds as no-ops.
