---
title: MCP servers
description: Attach stdio Model Context Protocol tools to an agent.
---

Axon can spawn out-of-process MCP servers and expose their discovered **tools** as ordinary Axon tools.

## Configure a server

```go
agent, err := axon.New(axon.Config{
    Model:        model,
    SystemPrompt: "...",
    MCPServers: []axon.MCPServer{
        {
            Command: "my-mcp-server",
            Args:    []string{"--stdio"},
            Env:     []string{"MODE=read-only"},
        },
    },
})
```

## Startup sequence

For each server, `New`:

1. spawns `Command` with `Args`;
2. opens stdin/stdout pipes;
3. starts a line-oriented JSON-RPC response loop;
4. sends `initialize` using protocol version `2024-11-05` and client name `axon`;
5. sends `notifications/initialized`;
6. calls `tools/list`;
7. maps each discovered MCP tool to an Axon `Tool`.

If any server fails startup/handshake/discovery, `New` closes MCP clients it already started and returns an error.

## Tool calls

An MCP tool invocation becomes:

```text
tools/call
  name: <tool name>
  arguments: <decoded JSON object>
```

Axon concatenates text entries from the MCP result's `content` array. Non-text content types are currently ignored. An MCP result with `isError: true` becomes a tool error.

## Current scope: tools only

The implementation does not expose MCP resources, prompts, roots, sampling, or other MCP capability families. `tools/list` / `tools/call` is the supported surface.

## Process and environment

The child receives the current process environment. `MCPServer.Env` entries are appended when supplied.

`Agent.Close()` kills each MCP child process. `Agent.Reset()` does not restart or rediscover MCP servers.

## Important blocking behavior

The current MCP request path waits on an internal response channel without a context-aware select or per-call timeout. If a server stays alive but never responds to a JSON-RPC request, that call can block indefinitely.

Treat MCP servers as trusted runtime dependencies and add timeout/isolation behavior inside the server or around the Axon process when that failure mode matters.

## Name collisions

MCP tools are appended to the custom-tool list before duplicate validation. A discovered MCP name that collides with a remaining built-in or another discovered/custom tool causes `New` to fail with `ErrDuplicateTool`.
