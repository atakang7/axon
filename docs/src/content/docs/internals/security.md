---
title: Security Boundaries
description: How Axon protects credentials and configuration from tools.
---

## The Boundary

Tools execute code and modify state on behalf of the LLM. To limit blast radius, Axon enforces strict isolation between tools and the agent's core infrastructure.

```
┌──────────────────────────────────────────────────────────────────┐
│                         Agent                                    │
│                                                                  │
│  ┌────────────────────┐  ┌─────────────────────────────────┐    │
│  │    Settings         │  │       Tools                     │    │
│  │  (has API keys,     │  │  (receive only Limits —         │    │
│  │   provider config)  │  │   cannot reach API keys,        │    │
│  │                     │  │   provider config, or session)  │    │
│  └────────────────────┘  └─────────────────────────────────┘    │
│           │                          │                           │
│           │ Limits = Settings.       │ Workspace interface       │
│           │   Tools.limits()         │   Dir()                   │
│           │                          │   ResolvePath()           │
│           ▼                          │   RecordEdit()            │
│  ┌────────────────────┐              │                           │
│  │     Limits          │◄─────────────┘                          │
│  │  (flat caps only,   │                                         │
│  │   no credentials)   │  Plan interface (task tool)             │
│  └────────────────────┘    RegisterTask()                        │
│                            AdvanceTask()                         │
│                            ReplanTask()                          │
└──────────────────────────────────────────────────────────────────┘
```

## What Tools Cannot Do

A tool's function signature is `func(ctx context.Context, args json.RawMessage) (string, error)`. Because of this signature, tools **cannot**:

1. **Access API keys** — The `Settings` struct is completely inaccessible to tools.
2. **Read conversation history** — Tools cannot see past interactions or the session log.
3. **Modify configuration** — Tools cannot change the model, system prompt, or pruner settings.
4. **Call other tools directly** — Tools have no reference to the agent's tool registry.

## What Tools Receive

Tools are constructed by `setup.go` before being passed to the agent. During construction, built-in tools are given exactly what they need:

1. **Limits:** A flat struct containing caps (e.g., `ReadMaxBytes`, `ExecTimeout`). No credentials.
2. **Workspace:** An interface for resolving paths safely and recording edits for `agent.Undo()`.
3. **Plan:** An interface specifically for the `task` tool to interact with the current plan.
4. **BackgroundShells:** A registry for the `exec`, `bash_output`, and `kill_shell` tools to manage long-running processes.

Custom tools follow the same rule: you provide the closure, and the tool can only access what you explicitly capture in that closure.

## MCP Subprocesses

MCP servers run as separate OS processes. They receive their environment variables (`MCPServer.Env`) at startup, but they cannot read the agent's environment or access the agent's memory space.
