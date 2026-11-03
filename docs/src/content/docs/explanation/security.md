---
title: Tool boundaries
description: What runs inside the embedding process.
---

Tools receive an execution context and JSON arguments. Built-in filesystem tools use a workspace interface; shell tools execute local commands with the permissions of the application process.

Provider credentials are held in settings passed to the client. Built-in tools receive limits instead of that settings object. This separation does not sandbox shell commands or custom tools: a command may still read files that its process account can access.

MCP servers are child processes. They inherit the host environment by default, and `MCPServer.Env` adds or overrides entries. Only start MCP servers and shell commands that the application intends to trust.
