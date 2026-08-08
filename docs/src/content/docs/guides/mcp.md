---
title: Connect an MCP server
description: Spawn a stdio MCP server and expose its discovered tools through Axon's normal tool loop.
---

## Add a server at construction

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

Axon starts MCP servers during `New`. If startup, initialization, or tool discovery fails, agent construction fails and already-started MCP clients are closed.

## Current protocol sequence

Axon currently uses stdio, newline-delimited JSON-RPC and performs:

```text
initialize (protocolVersion 2024-11-05)
notifications/initialized
tools/list
```

Each discovered MCP tool becomes an ordinary Axon `Tool` with its name, description, `inputSchema`, and an adapter function.

## Calling a tool

The adapter decodes model arguments to an object and calls MCP `tools/call` with:

```json
{
  "name": "tool-name",
  "arguments": {}
}
```

Text content entries are concatenated. Non-text content types are currently ignored. `isError: true` becomes an Axon tool error and is fed back to the model.

## Current scope is tools only

Axon's MCP integration does not currently expose MCP resources, prompts, roots, sampling, or other capability families.

## Environment and trust

The subprocess inherits the parent process environment. `MCPServer.Env` entries are appended to it.

Treat the server as trusted executable code with the process permissions it receives.

## Lifecycle

`Agent.Close` kills MCP child processes.

`Agent.Reset` does **not** restart or rediscover them; existing MCP clients/tools remain attached while the session/background-shell state is reset.

## Timeout caveat

The current MCP `call` path waits for a matching response channel without selecting on the tool context or applying a per-call timeout. A server that stays alive but never responds can therefore block that call indefinitely.

If this failure mode matters, put timeout/process supervision around the MCP server or Axon process until the runtime adds bounded MCP request waits.
