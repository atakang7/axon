---
title: Architecture
description: Internal design reference for contributors.
---

## Package layout

Single flat package. No `internal/`, no sub-packages. Everything is `package axon`.

```
axon/
├── axon.go              ← types: Model, Request, Msg, Tool, Provider, Stream
├── api.go               ← public surface: OpenAI(), Config, errors
├── setup.go             ← New(), Close(), Reset(), Undo()
├── loop.go              ← Step(), Run() — the turn loop
├── agent.go             ← Agent struct, chat() retries, tool dispatch
│
├── session.go           ← Session: persist, append, edit tracking, tasks
├── sessions_list.go     ← ListSessions(), SwitchSession()
├── memory.go            ← ContextMessages() — what the model sees
│
├── pruner.go            ← context curator: park stale blocks
├── prompt.go            ← system prompt builder
│
├── client.go            ← OpenAI-compatible SSE streaming HTTP client
│
├── settings.go          ← Settings tree, defaults, provider resolution
├── load.go              ← YAML + .env loading, secret expansion
├── config.go            ← file locations, Limits
├── scalar.go            ← Duration, Bytes YAML types
│
├── handler.go           ← Event, Kind, payload structs
├── bg.go                ← BackgroundShells registry
├── mcp.go               ← MCP JSON-RPC subprocess client
│
├── tools.go             ← Workspace/Plan interfaces, tool names, schema helpers
├── tools_helpers.go     ← WriteFileAtomic, binary detection, limitBuf
├── tool_read.go         ← read tool
├── tool_write.go        ← write tool
├── tool_exec.go         ← exec + bash_output + kill_shell tools
├── tool_search.go       ← search tool (ripgrep)
└── tool_task.go         ← task tool
```

## Type relationships

```
axon.Config ──────────┐
  .Model ─────────────┼──► Model interface { Complete(ctx, Request) (*Msg, error) }
  .Pruner ────────────┤                         │
  .SystemPrompt       │                         ▼
  .Tools []Tool ──────┤               ┌──────────────────┐
  .OnEvent ───────────┤               │    Client         │
  .Session ───────────┤               │  (SSE streaming)  │
  .MCPServers ────────┤               └──────────────────┘
  .Settings ──────────┤
  .ExcludeBuiltins    │
  .Cwd                │
                      ▼
               ┌─────────────┐
               │    Agent     │
               ├─────────────┤
               │ model        │──► Model
               │ tools []Tool │──► read, write, exec, search, task, + custom
               │ session      │──► Session (persistence + edit log + task)
               │ pruner       │──► Pruner (nil if no pruner model)
               │ shells       │──► BackgroundShells (per-agent registry)
               │ mcpClients   │──► []mcpClient (subprocess lifecycle)
               │ onEvent      │──► func(ctx, Event)
               │ limits       │──► Limits (flat caps from Settings.Tools)
               │ settings     │──► Settings (frozen at construction)
               └─────────────┘
```

## Settings lifecycle

```
axon.yaml ──► Load() ──► resolveSecrets(.env) ──► WithDefaults() ──► validate()
                                                       │
                                                       ▼
                                                  frozen in Agent
```

Settings are frozen at agent construction. Nothing re-reads config after `New()`. Two agents in one process can differ.

## SSE stream processing

```
Client.Complete()
│
├─ POST {baseURL}/v1/chat/completions (stream: true)
├─ Set idle timer (IdleTimeout)
│
└─ for each SSE line:
     ├─ Reset idle timer
     ├─ Parse: data: {"choices":[{"delta":{...}}]}
     │
     ├─ delta.content → accumulate + Stream.Token()
     ├─ delta.reasoning_content → accumulate + Stream.Reasoning()
     ├─ delta.tool_calls[i] → accumulate by index + Stream.ToolArgs()
     ├─ usage → store for final report
     │
     ├─ data: [DONE] → stop
     └─ idle timer fires → cancel → "stream stalled" error
```

Tool calls arrive as fragments keyed by index. Reassembled in stable order after the stream ends.

## Background shells

```
exec(command, run_in_background=true)
│
├─ sh -lc "command" (Setpgid for process group)
├─ stdout/stderr → logfile (per-shell)
├─ stdin → /dev/null
└─ return shell_id

bash_output(shell_id) → readNew(maxBytes)
  ├─ Delta only (offset tracked per-shell)
  ├─ File identity tracked (dev+inode) for replacement detection
  └─ Caps output, advances offset past dropped bytes

kill_shell(shell_id) → SIGTERM to process group → wait → SIGKILL

agent.Close() / Reset() → KillAll()
```
