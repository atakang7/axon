---
title: Source map
description: File-by-file map of the Axon codebase.
---

This page maps the implementation by responsibility. It is useful when a behavior in the higher-level docs needs to be traced to code.

## Public contracts and construction

| File | Responsibility |
| --- | --- |
| `axon.go` | `Model`, `Request`, `Stream`, `Provider`, `Msg`, `ToolCall`, `Tool`, `ToolSpec` contracts |
| `api.go` | `OpenAI`, public sentinels, `Config`, `InputFunc`, `StepResult`, session accessors |
| `setup.go` | `New`, built-in binding, MCP startup integration, tool validation, reset/undo/cd/close |
| `settings.go` | complete Settings tree, defaults, provider/model resolution, client construction, tool-limit projection |
| `config.go` | path resolution, per-cwd session key, state directory, flattened `Limits` |
| `load.go` | YAML + credentials-file loading, strict decode, secret expansion, validation |
| `scalar.go` | YAML duration and byte-size scalar types |

## Turn and model execution

| File | Responsibility |
| --- | --- |
| `loop.go` | `Step` and `Run` state machine |
| `agent.go` | agent state, model-call retries, interrupt, model/pruner mutation, tool dispatch |
| `client.go` | OpenAI-compatible HTTP/SSE model implementation and `APIError` |
| `prompt.go` | compose embedder system prompt with model-visible tool documentation |
| `handler.go` | event kinds/payloads and synchronous event emission |

## Session and context

| File | Responsibility |
| --- | --- |
| `session.go` | persistent session, working directory, edit ledger, task state |
| `memory.go` | model-visible context projection, recency-window collapse, parking breadcrumbs |
| `pruner.go` | pruning thresholds, curator request/response, hard protected IDs, mode prompts |

## Tools and processes

| File | Responsibility |
| --- | --- |
| `tools.go` | `Workspace`/`Plan` capability interfaces, built-in names/modes, JSON Schema helpers |
| `tool_read.go` | file slice/full read and directory listing |
| `tool_write.go` | save/replace/insert mutations and undo recording |
| `tool_exec.go` | foreground shell execution + `bash_output` + `kill_shell` tool definitions |
| `tool_search.go` | ripgrep-backed literal/regex search |
| `tool_task.go` | register/advance/replan task tool |
| `tools_helpers.go` | atomic writer, binary-file heuristic, byte-capped concurrent output buffer |
| `bg.go` | background shell registry, log-delta reader, process-group lifecycle |
| `mcp.go` | MCP stdio JSON-RPC subprocess, handshake, tool discovery and invocation |

## Tests

The repository mirrors most runtime areas with `_test.go` files:

```text
bg_test.go
client_test.go
config_test.go
handler_test.go
load_test.go
loop_test.go
memory_test.go
pruner_test.go
session_test.go
setup_test.go
tool_read_test.go
tool_search_test.go
tool_task_test.go
tool_write_test.go
tools_helpers_test.go
tools_test.go
```

There is no separate package hierarchy hiding the engine: the runtime is deliberately small enough that these files form the complete implementation surface.

## CI and docs

- `.github/workflows/ci.yml` runs build, vet, gofmt check, race tests, commitlint, and now the Astro docs build on pull requests.
- `.github/workflows/deploy-docs.yml` builds `docs/` and deploys `docs/dist` to GitHub Pages on `main`.
- `docs/` is an Astro + Starlight application.
