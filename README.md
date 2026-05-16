# axon

**A Go runtime for building LLM agents.**

Axon is a small library that runs the agent loop — streaming a model API, dispatching tool calls, persisting an append-only session, pruning context under pressure, and emitting structured events at every step. Embedders supply a provider, optional tools, and a handler; the runtime drives the loop.

The repo also ships a reference CLI (`cmd/axon`) that wires the runtime to a terminal: an interactive provider picker, a YAML loader for agent personalities, a colored TTY renderer, and slash commands. The CLI is one consumer of the library, not the product.

```
github.com/atakang7/axon/agent     ← the runtime (import this)
github.com/atakang7/axon/cmd/axon  ← the reference CLI
```

---

## The library

### Minimum viable embed

```go
import "github.com/atakang7/axon/agent"

ag, err := agent.New(agent.Config{
    Provider: agent.Provider{
        Name: "openai", Model: "gpt-4o", BaseURL: "https://api.openai.com",
        APIKey: os.Getenv("OPENAI_API_KEY"),
    },
})
if err != nil { return err }
defer ag.Close()

res, err := ag.Step(ctx, "list every TODO comment under cmd/")
```

That's the whole minimum. `New` constructs an agent with the runtime's built-in tools (read, write, exec, search, task, bash_output, kill_shell) attached. `Step` drives one user turn to completion and returns the assistant text + every tool call that happened.

### Adding your own tools

```go
deployTool := agent.Tool{
    Name:        "deploy",
    Description: "Deploy a service to staging.",
    Schema: map[string]any{
        "type": "object",
        "properties": map[string]any{"service": map[string]any{"type": "string"}},
        "required": []string{"service"},
    },
    Fn: func(ctx context.Context, args json.RawMessage) (string, error) {
        var p struct{ Service string }
        if err := json.Unmarshal(args, &p); err != nil { return "", err }
        return runDeploy(ctx, p.Service)
    },
}

ag, _ := agent.New(agent.Config{
    Provider:     myProvider,
    SystemPrompt: "You are a deployment assistant.",
    Tools:        []agent.Tool{deployTool},
})
```

`Tools` is appended to the built-ins. Names that collide with built-ins are rejected. Built-ins (read, write, exec, search, task, bash_output, kill_shell) are always present — there is no knob to remove them.

### Observability

`Config.OnEvent` is a plain function field. The runtime calls it at every meaningful moment with an `Event`:

```go
cfg.OnEvent = func(ctx context.Context, e agent.Event) {
    switch e.Kind {
    case agent.KindToken:
        httpResp.Write([]byte(e.Text)) // stream tokens to a client
    case agent.KindToolCall:
        log.Printf("tool %s: %s", e.Tool.Name, e.Tool.Args)
    }
}
```

Event kinds: `KindUserInput`, `KindToken`, `KindReasoning`, `KindAssistantEnd`, `KindToolCall`, `KindToolResult`, `KindToolError`, `KindTurnEnd`, `KindPruneStart`/`KindPruneEnd`, `KindError`. See `agent/handler.go` for the full set.

Fan-out is a one-liner — just call multiple sinks inside the closure. No `MultiHandler` ceremony needed.

### Owning vs. driving the loop

Two ways to use the agent:

```go
// You own the loop — good for HTTP handlers, orchestrators, tests.
res, err := ag.Step(ctx, userMessage)

// The runtime drives the loop — good for REPLs, batch runs.
err := ag.Run(ctx, func() (string, bool) { return readLine() })
```

Both are first-class. `Run` is just sugar over `Step` for the input-source-driven case.

### Operations exposed on `*Agent`

```go
ag.Step(ctx, input)         // one user turn
ag.Run(ctx, inputFn)        // loop until input exhausts
ag.Interrupt() bool         // cancel the in-flight turn
ag.Reset()                  // wipe session, rebuild system prompt
ag.Undo() (string, bool)    // revert the last file edit
ag.Cd(path) (string, error) // change cwd
ag.Session() *Session       // read live session state
ag.SessionPath() string     // on-disk path of the session file
ag.Close() error            // release background shells
```

### Pluggable session storage

`Config.Session` accepts a `*agent.Session`. Most embedders leave it nil and let the runtime load or create the default on-disk session at `agent.SessionPath()`. If you want sessions in Postgres / Redis / RAM, construct your own `*Session` and pass it in.

---

## The reference CLI

```sh
go build -o axon ./cmd/axon
./axon                                  # interactive
./axon --prompt "summarize TODOs"       # single-shot
./axon --agent reviewer                 # load ~/.config/axon/agents/reviewer.yaml
```

### Provider config

The CLI reads `${XDG_CONFIG_HOME:-~/.config}/agent/providers.json` (override with `AXON_PROVIDERS_PATH`):

```json
{
  "providers": [
    { "name": "ollama",   "base_url": "http://localhost:11434", "model": "llama3" },
    { "name": "openai",   "base_url": "https://api.openai.com", "model": "gpt-4o",   "api_key": "sk-..." },
    { "name": "claude",   "base_url": "https://api.anthropic.com", "model": "claude-3-opus", "api_key": "sk-ant-..." }
  ]
}
```

Or pure env:

```sh
LLM_MODEL=gpt-4o LLM_BASE_URL=https://api.openai.com LLM_API_KEY=sk-... ./axon
```

`LLM_PROVIDER` selects one when multiple are configured. The CLI offers an interactive picker otherwise.

### YAML agents (CLI-only)

Place YAML configs under `${AXON_AGENTS_DIR:-~/.config/axon/agents}/`:

```yaml
name: reviewer
system_prompt: ./reviewer.md
tools:
  - name: submit_review
    type: shell
    description: "Submit a GitHub PR review."
    schema:
      type: object
      properties:
        verdict: { type: string, enum: [approve, request_changes] }
        body:    { type: string }
      required: [verdict, body]
    command: gh pr review --{{.verdict}} --body {{.body | shellQuote}}
    timeout_seconds: 10
```

YAML is **a CLI concern**. The runtime knows nothing about it. Library users build `agent.Config` directly.

### Slash commands

- `/new` — wipe session, restart
- `/undo` — revert the last file edit
- `/cd <path>` — change cwd
- `/pwd` — show cwd
- `/session` — show session info

---

## Design

- **One LLM, no subagents.** The cost lever is aggressive forgetting via the secondary pruner LLM, not parallel agents.
- **Append-only session log.** Park/recall/forget are projections (`ContextMessages`), never mutations. `/undo` is byte-exact because edits are atomic (tmp + rename).
- **Turn-scoped cancellation.** One `context.Context` per turn covers the HTTP stream *and* every tool subprocess. `Interrupt()` fires that cancel.
- **Every tool call requires a `reason`.** The model must articulate intent before paying the call's token cost.
- **No global state.** Two agents in one process is supported by construction.
- **No sandbox or permission prompt today.** Ctrl-C and `/undo` are the guardrails. A permission layer can be added on top via a tool wrapper.

See `ARCHITECTURE.md` for the loop, the pruner contract, and the extension points.

---

## License

MIT. See `LICENSE`.
