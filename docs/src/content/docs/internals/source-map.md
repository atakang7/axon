---
title: Source map
description: File ownership across the Axon implementation.
---

## Public contracts and configuration

| File | Owns |
| --- | --- |
| `axon.go` | `Model`, request/message/tool contracts |
| `api.go` | agent constructor config, public sentinels, `StepResult`, convenience API |
| `settings.go` | `Settings` schema, defaults, provider/model resolution, client construction |
| `load.go` | YAML + credentials loading, strict fields, variable expansion, validation |
| `config.go` | state-path resolution and flattened tool limits |
| `scalar.go` | YAML duration and byte-size types |

## Agent runtime

| File | Owns |
| --- | --- |
| `setup.go` | composition root, built-in/MCP binding, reset/undo/cd/close |
| `loop.go` | `Step` / `Run` turn state machine |
| `agent.go` | model call/retry path, interrupt, mutable model/pruner controls, tool dispatch |
| `prompt.go` | initial system-prompt + tool-contract composition |
| `handler.go` | event kinds and synchronous emission |

## Session and context

| File | Owns |
| --- | --- |
| `session.go` | persistence, workspace directory, edit ledger, task state |
| `memory.go` | primary-model context projection and breadcrumbs |
| `pruner.go` | pruning thresholds, curator prompt/call, parking decisions |

## Models and protocols

| File | Owns |
| --- | --- |
| `client.go` | OpenAI-compatible HTTP/SSE `Model` implementation |
| `mcp.go` | stdio MCP JSON-RPC client, handshake, tool discovery/calls |

## Built-in capabilities

| File | Owns |
| --- | --- |
| `tools.go` | `Workspace` / `Plan` interfaces, tool names/modes, schema helpers |
| `tool_read.go` | file/directory reads |
| `tool_write.go` | file mutation modes |
| `tool_exec.go` | foreground exec and shell-control tools |
| `tool_search.go` | ripgrep integration |
| `tool_task.go` | plan mutation tool |
| `bg.go` | background process registry and log-delta reads |
| `tools_helpers.go` | atomic writes, binary heuristic, capped output buffer |

## Tests

Tests sit beside implementation. For behavioral documentation, prefer the executable path plus its tests over comments or older prose. Especially high-signal suites include `loop_test.go`, `setup_test.go`, `load_test.go`, `config_test.go`, `client_test.go`, `memory_test.go`, `pruner_test.go`, and `bg_test.go`.
