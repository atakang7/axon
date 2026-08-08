---
title: MCP Servers
description: Integrate Model Context Protocol tool servers.
---

Axon spawns MCP servers as subprocesses, performs the JSON-RPC handshake, discovers tools, and maps them to `axon.Tool`. Killed automatically on `agent.Close()`.

## Usage

```go
agent, err := axon.New(axon.Config{
    Model:        model,
    SystemPrompt: "...",
    MCPServers: []axon.MCPServer{{
        Command: "npx",
        Args:    []string{"-y", "@modelcontextprotocol/server-filesystem", "/path/to/dir"},
    }},
})
if err != nil {
    log.Fatal(err) // handshake failure is fatal
}
defer agent.Close() // kills the subprocess
```

MCP tools appear alongside built-in and custom tools. No special handling needed by the model or the embedder.

## MCPServer config

```go
type MCPServer struct {
    Command string
    Args    []string
    Env     []string // optional KEY=VALUE environment variables
}
```

## Protocol

```
axon                           MCP subprocess
  │                                  │
  ├─── initialize ──────────────────►│
  │◄── result ──────────────────────┤
  ├─── notifications/initialized ──►│
  ├─── tools/list ─────────────────►│
  │◄── result {tools: [...]} ──────┤
  │                                  │
  │  (agent is constructed)          │
  │                                  │
  ├─── tools/call {name, args} ────►│  (during tool execution)
  │◄── result {content: [...]} ────┤
  │                                  │
  ├─── [process killed] ──────────►│  (on agent.Close())
```

Communication is JSON-RPC 2.0 over stdin/stdout. The subprocess's stderr is not captured.

## Multiple servers

```go
MCPServers: []axon.MCPServer{
    {Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-filesystem", "."}},
    {Command: "./my-custom-mcp-server", Args: []string{"--port", "0"}},
},
```

Each server is its own subprocess. Tool names must not collide across servers or with built-in/custom tools.

## Error handling

- Handshake failure (`initialize` or `tools/list` fails) → `axon.New()` returns an error. Already-started servers are cleaned up.
- Tool call failure → error is returned as the tool result and fed back to the model.
- Subprocess death during a session → tool calls to that server's tools will fail.
