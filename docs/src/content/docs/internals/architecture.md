---
title: Architecture
description: Source layout and data flow.
---

Axon is one Go package with `Agent` coordinating model calls, tools and sessions.

```text
+--------+  settings  +-------+  writes  +---------+
| Config | ---------> | Agent | -------> | Session |
+--------+            +-------+          +---------+
```

| File | Responsibility |
|------|----------------|
| `setup.go` | `New`, tool binding, shutdown |
| `loop.go` | `Step` and `Run` |
| `agent.go` | Model retries and tool dispatch |
| `client.go` | Streaming HTTP and SSE |
| `session.go` | Saved messages and edit history |
| `checkpoint.go` | Context checkpoints |
| `std_tools.go` | Model-visible tool schemas |
| `mcp.go` | MCP subprocess client |

The `Client` implements `Model.Complete`; `Agent` invokes `Tool.Fn` on returned tool calls.

## Limits

- `Step` is not safe to call concurrently on one agent.
- Settings are resolved at construction, not reloaded during a turn.
