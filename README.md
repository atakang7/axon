# axon

Axon is a Go library for embedding tool-using language model agents with persistent sessions and bounded model context.

## Repository map

The runtime is one `package axon`; the documentation site is a separate project.

| Path | Owns |
| --- | --- |
| [`api.go`](api.go), [`setup.go`](setup.go), [`loop.go`](loop.go), [`agent.go`](agent.go) | Public agent API, construction, turns, retries |
| [`client.go`](client.go) | Streaming OpenAI-compatible HTTP client |
| [`session.go`](session.go), [`context_budget.go`](context_budget.go), [`checkpoint.go`](checkpoint.go) | Persistent history and the model's working view |
| [`std_tools.go`](std_tools.go), [`mcp.go`](mcp.go) | Local tools and MCP stdio subprocesses |
| [`load.go`](load.go), [`settings.go`](settings.go), [`axon.example.yaml`](axon.example.yaml) | Optional file configuration, defaults and example |
| [`docs/`](docs/README.md) | Astro Starlight documentation site |
| [`RUNBOOK.md`](RUNBOOK.md), [`DELIVERY.md`](DELIVERY.md), [`docs/weaknesses.md`](docs/weaknesses.md) | Run and verification commands, release path, known limitations |
| [`AGENTS.md`](AGENTS.md), [`CONTRIBUTING.md`](CONTRIBUTING.md) | Change boundaries and contributor checks |

## Contents

1. [Agent request path](#1-agent-request-path)
2. [Tool execution path](#2-tool-execution-path)
3. [Session and context path](#3-session-and-context-path)
4. [Configuration and trust path](#4-configuration-and-trust-path)
5. [Event path](#5-event-path)
6. [Delivery path](#6-delivery-path)
7. [Design decisions](#7-design-decisions)
8. [Host requirements](#8-host-requirements)

## 1. Agent request path

```text
┌───────────────┐     ┌──────────────┐     ┌──────────────────┐
│ embedding app │ ──▶ │ Agent.Step   │ ──▶ │ Model.Complete   │
└───────────────┘     │ loop.go      │     │ client.go        │
                      └──────┬───────┘     └────────┬─────────┘
                             ▲                      │ HTTP/SSE
                             └────── reply ─────────┤
                                                    ▼
                                              provider API
```

The app supplies a `Model`, system prompt and optional tools to `New(Config)` (`api.go`, `setup.go`). `Step` records user input, asks the model, then repeats if the reply requests tools (`loop.go`). `OpenAI` supplies the included streaming client; other `Model` implementations can be injected (`api.go`, `client.go`).

## 2. Tool execution path

```text
model tool_calls ──▶ loop.toolGroups ──▶ Tool.Fn
                                            │
                                   ┌────────┴────────┐
                                   │ local built-ins │
                                   │ custom Go tools │
                                   │ MCP stdio call  │
                                   └────────┬────────┘
                                            ▼
                              tool result → Session → next model call
```

`New` registers built-ins, app-provided tools and tools discovered at MCP startup (`setup.go`, `mcp.go`). Consecutive read-only tool calls may run concurrently; results are saved and returned to the model (`loop.go`, `tool_exec.go`). See the [tool reference](docs/src/content/docs/reference/tools.md).

## 3. Session and context path

```text
Step ──▶ Session.Messages ──▶ Save() ──▶ JSON session file
                  │
                  └──▶ ContextMessages ──▶ budget/folding ──▶ model
```

A session is loaded or created when the agent is constructed and saved during a turn (`setup.go`, `session.go`). `ContextMessages` derives a shorter model view from that history; folding and checkpoints do not delete the original message record (`context_budget.go`, `checkpoint.go`). The default session path is derived from the working directory; `AXON_SESSION_PATH` overrides it (`config.go`). See [context behavior](docs/src/content/docs/explanation/context.md).

## 4. Configuration and trust path

```text
axon.yaml + .env ──▶ Load / LoadFrom ──▶ Settings.NewClient ──▶ HTTP bearer key
embedding app ──▶ New(Config) ──▶ shell tools / optional MCP subprocesses
```

`New(Config)` does not read configuration files; an embedder can opt into `Load()` or supply its own settings (`load.go`, `setup.go`). Built-in shell tools run with the embedding process's permissions. MCP servers are child processes and inherit the host environment unless overridden (`std_tools.go`, `mcp.go`). See [settings](docs/src/content/docs/reference/settings.md) and [tool boundaries](docs/src/content/docs/explanation/security.md).

## 5. Event path

```text
turn / provider / tool / usage ──▶ Agent.emit ──▶ Config.OnEvent ──▶ app
```

`handler.go` defines turn, tool, token and usage events. Axon calls the supplied `OnEvent` callback; collection, logging and display belong to the embedding application.

## 6. Delivery path

```text
pull request ──▶ CI checks
main push ─────▶ CI gate ──▶ semantic-release ──▶ GitHub release
main push ─────▶ Astro build ──▶ GitHub Pages
```

The release workflow invokes the CI workflow as a prerequisite; the Pages workflow deploys separately (`.github/workflows/`). See [DELIVERY.md](DELIVERY.md).

## 7. Design decisions

### One Go package

`api.go`, `loop.go` and `tools.go` share a single package so embedders can construct and run an agent without separate internal modules. The website lives under `docs/`.

### Full history, shorter working view

`Session.Messages` persists the record; context folding controls what the model receives on each call (`session.go`, `context_budget.go`). This separates recovery from prompt budget.

### Tool capabilities without provider settings

`setup.go` passes workspace and execution limits to built-in tools rather than the provider settings object (`config.go`, `setup.go`). This does not sandbox shell commands.

## 8. Host requirements

- **Library:** Go 1.26.2 (`go.mod`), an embedding Go application, and a model implementing `Model`.
- **Included client:** an OpenAI-compatible streaming chat endpoint and its credential (`client.go`).
- **Documentation site:** Node.js 22.12.0 in CI (`docs/package.json`, `.github/workflows/ci.yml`).
- **Concurrency:** `Step`, `Run` and `Reset` must not overlap; `Interrupt` is safe across goroutines (`api.go`).

Start with [Run an agent](docs/src/content/docs/how-to/first-agent.md). Review [known weaknesses](docs/weaknesses.md) before embedding untrusted tools.
