---
title: MCP Servers
description: External tools over subprocess JSON-RPC.
---

`MCPServer` launches a subprocess and exposes its discovered tools to `Agent`.

```text
+-------+  tool call  +-----------+  JSON-RPC  +------------+
| Agent | ----------> | mcpClient | ---------> | MCP server |
+-------+             +-----------+            +------------+
```

```go
agent, err := axon.New(axon.Config{
    Model: model,
    SystemPrompt: "You are a coding assistant.",
    MCPServers: []axon.MCPServer{{
        Command: "npx",
        Args: []string{"-y", "@modelcontextprotocol/server-filesystem", "/path/to/dir"},
    }},
})
if err != nil {
    log.Fatal(err)
}
defer agent.Close()
```

## Config

| Field | Default | What |
|-------|---------|------|
| `Command` | required | Server executable |
| `Args` | empty | Subprocess arguments |
| `Env` | inherited environment | Extra `KEY=VALUE` entries |

## Limits

- Startup or tool-discovery errors fail `New`.
- MCP tool names must not collide with existing tools.
- `Close()` stops the subprocesses.
