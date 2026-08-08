# axon

**A Go runtime for building LLM agents.**

axon runs the agent loop — streaming a model API, dispatching tool calls, persisting an append-only session, pruning context under pressure, and emitting structured events at every step. You supply a model, a system prompt and optionally your own tools; the runtime drives the loop.

```go
settings, err := axon.Load()                                        // axon.yaml + .env
model, err := settings.NewClient("openrouter", "deepseek/deepseek-v3.2")

ag, err := axon.New(axon.Config{
    Model:        model,
    SystemPrompt: "You are a coding assistant.",
    Settings:     settings,
})
defer ag.Close()

res, err := ag.Step(ctx, "list every TODO comment under cmd/")
```

`New` attaches the built-in tools (read, write, exec, search, task, bash_output, kill_shell). `Step` drives one user turn to completion and returns the assistant text plus every tool call that happened.

---

## Configuration

axon runs on **two files**. Both are required.

| file | holds | commit it? |
| --- | --- | --- |
| `~/.config/axon/axon.yaml` | every setting | **yes** — no secrets in it |
| `~/.config/axon/.env` | every credential | **no** |

The split is the point: settings and credentials have different lifecycles, different rotation and different blast radius. Keeping them apart means the file you actually read and diff never has to be redacted.

Override either location with `AXON_CONFIG` and `AXON_ENV`.

### Quick start

```sh
mkdir -p ~/.config/axon
curl -o ~/.config/axon/axon.yaml \
  https://raw.githubusercontent.com/atakang7/axon/main/axon.example.yaml

printf 'OPENROUTER_API_KEY=sk-or-...\n' > ~/.config/axon/.env
chmod 600 ~/.config/axon/.env
```

The smallest config that works:

```yaml
providers:
  openrouter:
    base_url: https://openrouter.ai/api
    api_key: ${OPENROUTER_API_KEY}
    models:
      deepseek/deepseek-v3.2:
```

Everything else has a default. A config that sets three things is a better config than one that restates thirty. [`axon.example.yaml`](axon.example.yaml) documents every field at its default value.

### Providers

axon speaks exactly one protocol: **OpenAI-style streaming chat completions**, `POST {base_url}/v1/chat/completions` with a bearer token. There is no per-provider code anywhere in the codebase.

That means it works with OpenRouter, OpenAI, Groq, DeepSeek direct, Together, Fireworks, vLLM, llama.cpp and Ollama's `/v1` endpoint. It is developed and tested against **OpenRouter**.

It does **not** work with Anthropic, Gemini, Bedrock or Azure OpenAI — those use different endpoints, auth headers and message shapes. Reaching one means implementing the `Model` interface, not editing configuration. Open an issue if you need one.

`providers` is a list of endpoints and the models you may ask them for. **axon does not choose between them** — your application pins one or offers the user a choice:

```yaml
providers:
  openrouter:
    base_url: https://openrouter.ai/api
    api_key: ${OPENROUTER_API_KEY}
    models:
      qwen/qwen3.6-flash:                 # no options needed
      deepseek/deepseek-v3.2:
        route: Baidu Qianfan              # OpenRouter routing hint
```

```go
for _, name := range settings.Providers["openrouter"].ModelNames() { … }
```

### Credentials

`${VAR}` in an `api_key` is resolved from the `.env`, falling back to the process environment so CI can inject secrets without writing a file. The `.env` is the conventional format — `KEY=value`, `#` comments, optional `export`, optional quotes. It is not a shell: no substitution, no interpolation, nothing executable.

An unresolved variable is an **error at load**, not an empty key. An empty key reaches the provider as a missing header and comes back as a 401 several seconds later, which is a much worse way to learn you misspelled a name.

### What you can set

| section | what it bounds |
| --- | --- |
| `providers` | endpoints, models, credentials |
| `session` | where conversation state is written |
| `model` | max tokens, request and idle timeouts, reasoning |
| `retry` | attempts, backoff ceiling, which HTTP statuses are retried |
| `tools` | per-tool caps on output size and wall time |
| `pruner` | thresholds for the secondary context-pruning model |

Durations read as `30s`, `10m`, `1h30m`, or a plain number of seconds. Sizes read as `12KB`, `2MB` (decimal) or `32KiB`, `2MiB` (binary), or a plain byte count.

A misspelled field is a **load error**, not a setting that silently does nothing.

### What you cannot set

Prompts. The tool descriptions, the system-prompt assembly and the pruner's instructions are axon's own — they change in lockstep with the formats they describe, and an override would break the next time one changed. So are correctness details: file permissions, buffer sizes, the binary-content heuristic, the MCP protocol version.

The rule: **a value is configuration if changing it is an operational decision** — cost, time, context, where state lives. It stays in the code if changing it is a correctness decision.

### Environment variables

Four remain, and they all say *where something is*, never *what it contains*:

| | |
| --- | --- |
| `AXON_CONFIG` | path to `axon.yaml` |
| `AXON_ENV` | path to the `.env` |
| `AXON_DATA_DIR` | where state is written — overrides the config |
| `AXON_SESSION_PATH` | pins one session file — overrides the config |

The last two override rather than default, because their purpose is redirecting state without editing a file: a container mounting a volume, or a test that must not touch your real session.

### Embedding without files

`Config.Settings`'s zero value is usable — every unset field falls back to `DefaultSettings()` — so an embedder that does not want files can build a `Settings` in code and never call `Load`. Nothing in axon reads a file unless you ask it to, which is what keeps two agents in one process able to differ, and what lets tests assert on behaviour without arranging a filesystem.

---

## The library

### Adding your own tools

```go
deployTool := axon.Tool{
    Name:        "deploy",
    Description: "Deploy a service to staging.",
    Schema: map[string]any{
        "type": "object",
        "properties": map[string]any{"service": map[string]any{"type": "string"}},
        "required": []string{"service"},
    },
    Fn: func(ctx context.Context, args json.RawMessage) (string, error) { … },
}

ag, _ := axon.New(axon.Config{Model: m, SystemPrompt: p, Tools: []axon.Tool{deployTool}})
```

`Tools` is appended to the built-ins; a name that collides is rejected at `New`. Remove a built-in with `ExcludeBuiltins`, which frees its name.

### Observability

`Config.OnEvent` is a plain function field, called at every meaningful moment:

```go
cfg.OnEvent = func(ctx context.Context, e axon.Event) {
    switch e.Kind {
    case axon.KindToken:
        io.WriteString(w, e.Text)
    case axon.KindToolCall:
        log.Printf("tool %s: %s", e.Tool.Name, e.Tool.Args)
    }
}
```

Kinds: `KindSessionStart`, `KindUserInput`, `KindTurnStart`, `KindAPICall`, `KindToken`, `KindReasoning`, `KindAssistantEnd`, `KindToolArgDelta`, `KindToolCall`, `KindToolResult`, `KindToolError`, `KindTurnEnd`, `KindPruneStart`/`KindPruneEnd`, `KindInfo`, `KindError`, `KindSessionEnd`. `handler.go` is authoritative.

### Operations

```go
ag.Step(ctx, input)         // one user turn
ag.Run(ctx, inputFn)        // loop until input exhausts
ag.Interrupt() bool         // cancel the in-flight turn
ag.Reset()                  // wipe session, rebuild system prompt
ag.Undo() (string, bool)    // revert the last file edit, byte-exact
ag.Cd(path) (string, error)
ag.Session() *Session
ag.Close() error
```

### Errors

```go
var apiErr *axon.APIError
if errors.As(err, &apiErr) && apiErr.Status == 429 { … }
```

`APIError` carries the HTTP status as a number and the response body. Retry decisions branch on the code, never on message text.

Sentinels: `ErrNoModel`, `ErrNoSystemPrompt`, `ErrToolNotFound`, `ErrDuplicateTool`, `ErrInvalidTool`, `ErrInterrupted`, `ErrMissingConfig`, `ErrMissingEnv`, `ErrInvalidConfig`, `ErrUnknownProvider`, `ErrUnknownModel`.

### Pluggable model

```go
type Model interface {
    Complete(ctx context.Context, req Request) (*Msg, error)
}
```

One method. Implement it to reach a provider that is not OpenAI-compatible, to route through a gateway, or to hand the loop a deterministic fake so it can be driven in tests with no network and no API key — which is how axon's own suite works.

---

## Design

- **One LLM, no subagents.** The cost lever is a secondary pruner model parking stale context, not parallel agents.
- **Append-only session log.** Parking is a projection, never a mutation, so `/undo` is byte-exact and the full history survives for audit.
- **Turn-scoped cancellation.** One `context.Context` per turn covers the HTTP stream *and* every tool subprocess.
- **Tools take capabilities, not state.** A tool receives the narrowest interface it needs (`Workspace`, `Plan`, `Limits`) and never the `Session` or the credentials. It cannot read the conversation, and a fake for tests is six lines.
- **No ambient state.** Configuration is resolved once, at construction, and passed down. Nothing re-reads settings at call depth, so two agents in one process can be tuned differently.
- **No sandbox or permission prompt.** `Interrupt()` and `Undo()` are the guardrails. A permission layer belongs on top, as a tool wrapper.

## License

MIT. See `LICENSE`.
