---
title: Connect an MCP server
description: Start a stdio MCP server from the agent.
---

Add the server to the configuration passed to `axon.New`:

```go
cfg.MCPServers = []axon.MCPServer{{
    Command: "npx",
    Args: []string{"-y", "@modelcontextprotocol/server-filesystem", "."},
}}
agent, err := axon.New(cfg)
if err != nil { log.Fatal(err) }
defer agent.Close()
```

The command must be installed and reachable on `PATH`. `axon.New` starts the child process, initializes the protocol and loads its tools. `agent.Close()` terminates the child.
