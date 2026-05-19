# axon

**A Go runtime for building LLM agents.**

Axon is a small library that runs the agent loop — streaming a model API, dispatching tool calls, persisting an append-only session, pruning context under pressure, and emitting structured events at every step. Embedders supply a provider, optional tools, and a handler; the runtime drives the loop.

This repo is library-only. The terminal coding agent that previously lived at `cmd/axon` has moved to its own project: **[bouton](https://github.com/atakang7/bouton)**.

```
github.com/atakang7/axon/agent  ← the runtime (import this)
github.com/atakang7/bouton      ← terminal coding agent built on axon
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
    SystemPrompt: "You are a coding assistant.",
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

Event kinds: `KindSessionStart`, `KindUserInput`, `KindTurnStart`, `KindAPICall`, `KindToken`, `KindReasoning`, `KindAssistantEnd`, `KindToolArgDelta`, `KindToolCall`, `KindToolResult`, `KindToolError`, `KindTurnEnd`, `KindPruneStart`/`KindPruneEnd`, `KindInfo`, `KindError`, `KindSessionEnd`. See `agent/handler.go` for the authoritative list.

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

The reference terminal coding agent is now [bouton](https://github.com/atakang7/bouton). Install with:

```sh
go install github.com/atakang7/bouton/cmd/bouton@latest
bouton                                  # interactive
bouton --prompt "summarize TODOs"       # single-shot
bouton --agent reviewer                 # YAML personality
```

Provider config, YAML agent personalities, slash commands, and the interactive picker all live in bouton — see its [README](https://github.com/atakang7/bouton#readme).

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
