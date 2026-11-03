---
title: Events
description: Event kinds and payload fields from handler.go.
---

Register `Config.OnEvent func(context.Context, Event)` to receive runtime events.

| Kinds | Payload |
| --- | --- |
| `KindSessionStart`, `KindSessionEnd` | `Session` |
| `KindUserInput`, `KindToken`, `KindReasoning`, `KindAssistantEnd` | `Text` |
| `KindTurnStart`, `KindTurnEnd`, `KindAPICall` | Turn metadata |
| `KindToolArgDelta`, `KindToolCall`, `KindToolResult`, `KindToolError` | `Tool` |
| `KindToolProgress` | `Tool`, `Text` |
| `KindUsage` | `Usage` |
| `KindPruneStart`, `KindPruneEnd` | `Prune` |
| `KindInfo`, `KindError` | `Text` or `Err` |

`UsageInfo` records prompt, completion and cached tokens plus a cost when supplied by the provider. Unrecognized event kinds can be ignored.
