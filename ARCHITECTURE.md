# Axon — Architecture

A Go runtime for building LLM agents. One library, one loop, pluggable everything that can vary.

```
github.com/atakang7/axon/agent     ← the runtime (library)
github.com/atakang7/axon/cmd/axon  ← reference CLI built on the runtime
```

The runtime knows nothing about terminals, flags, signals, YAML, or `os.Exit`. Everything terminal-shaped lives in `cmd/axon`.

## Layout

```
agent/
  api.go              Config, New, Step, Run, Reset, Undo, Cd, Session, Close
  agent.go            Agent struct, chat/retry, runTool
  handler.go          Event, Kind, ToolEvent, PruneInfo (emitted via Config.OnEvent)
  exports.go          DataDir, ProvidersPath, EnvString, ... (helpers CLIs need)
  session.go          Session struct, append-only log, edit history, undo
  memory.go           park/recall/forget projections; TaskTool lives here
  prompt.go           buildSystemPrompt (role + catalog + probes + orientation)
  pruner.go           secondary LLM that drops/parks old blocks
  providers.go        Provider type + LoadProviders
  config.go           env/XDG path resolution
  llm.go              OpenAI-compatible streaming chat client
  tools.go            Tool type, schema helpers, tool-name constants
  tools_helpers.go    atomic writes, formatters, binary refusal
  tool_{read,write,search,exec}.go   built-in tool implementations
  bg.go               background shell registry (servers, watchers)
  probes.go           language/build detection injected into the system prompt

cmd/axon/
  main.go             entry point: flags → Config → agent.New → REPL
  picker.go           interactive provider picker, lastChoice persistence
  yamlcfg.go          YAML agent personality loader (AgentConfig, ToolConfig)
  customtool.go       YAML ToolConfig → agent.Tool adapter (shell only today)
  tty_handler.go      terminal renderer + ttyHandler that consumes agent.Event
  commands.go         slash-command dispatch (/new, /undo, /cd, /pwd, /session)
  input.go            paste-aware stdin reader, single-shot input
```

## The turn loop

```
Step(ctx, input)
   │
   ▼
append user msg ─► session.Save
   │
   ▼
prune? ──► Pruner.Prune (parks/forgets old blocks)
   │
   ▼
chat() ──► Client.ChatStream
   │           │       │      │
   │       tokens   reasoning  tool-arg deltas
   │           └──► Config.OnEvent(ctx, Event{...})
   │
   ▼
tool_calls?
   │     │
   no   yes
   │     │
   │     ▼
   │   for each tc: runTool → append result → emit ToolResult
   │     │
   │     └────►(loop)
   │
   ▼
emit AssistantEnd, TurnEnd, return StepResult
```

`Run(ctx, inputFn)` is sugar over `Step` for the input-source-driven case.

## Public API surface

The whole API of `package agent`:

```go
// Construction — built-ins are always present; cfg.Tools are appended.
func New(Config) (*Agent, error)

// Agent
type Agent struct{ /* opaque */ }
func (a *Agent) Step(ctx, input) (StepResult, error)
func (a *Agent) Run(ctx, InputFunc) error
func (a *Agent) Interrupt() bool
func (a *Agent) Reset()
func (a *Agent) Undo() (path string, ok bool)
func (a *Agent) Cd(path) (string, error)
func (a *Agent) Session() *Session
func (a *Agent) Close() error

// Config — Provider and SystemPrompt are required.
type Config struct {
    Provider     Provider
    SystemPrompt string
    Tools        []Tool
    Pruner       *Pruner
    OnEvent      func(ctx, Event)
    Cwd          string
    Session      *Session
}

// Tools — the extension surface
type Tool struct {
    Name        string
    Description string
    Schema      map[string]any
    Fn          func(ctx, args) (string, error)
}

// Observability — plain function field on Config. Fan-out is a
// one-line closure that calls multiple sinks.

// Errors
var (
    ErrNoProvider, ErrNoSystemPrompt, ErrToolNotFound,
    ErrDuplicateTool, ErrInterrupted
)
```

## Invariants

- **`Session.Messages` is append-only.** Park / recall / forget are projections built in `ContextMessages`, never mutations. Audit history survives pruning.
- **Built-ins are unconditional.** Every agent has the hands-and-legs tools (read, write, exec, search, task, bash_output, kill_shell). No knob to remove them.
- **Tool execution is turn-scoped.** Ctrl-C / `Interrupt` cancels the in-flight chat AND kills the running tool's process group. Background shells outlive turns but die on `Close` or process exit.
- **Custom tool names cannot collide with built-ins.** Enforced at `New` time.
- **Writes are atomic.** Every file mutation goes through `writeBytesRaw` (tmp + rename). Formatters run after, never during, so `Undo` is byte-exact.
- **Reason field required on every tool call.** The model must articulate intent before paying the call's token cost.
- **The runtime never writes to stdout.** All observability goes through `Config.OnEvent`. The CLI's `ttyHandler` is the only thing that prints.

## How `cmd/axon` consumes the runtime

```
flag.Parse
   │
   ▼
LoadAgentConfig(--agent name)   ← reads YAML
   │
   ▼
agent.LoadProviders()           ← reads ~/.config/agent/providers.json
   │
   ▼
pickProvider (interactive)
   │
   ▼
tty := newTTYHandler()
   │
   ▼
ag, _ := agent.New(agent.Config{
    Provider, SystemPrompt, Tools (from YAML),
    Pruner, OnEvent: tty.Handle,
})
   │
   ▼
for line := range stdin:
   if slash:  handleSlash(ag, line)
   else:      ag.Step(ctx, line)
```

## Extending

- **New built-in tool** → add `tool_<name>.go` with a `<Name>Tool(s *Session) Tool` constructor; register in `builtinTools` in `api.go`.
- **New custom tool kind** (e.g. MCP) → add the arm in `buildCustomTool` (`cmd/axon/customtool.go`). The runtime needs no changes — MCP would be `Tool` values whose `Fn` happens to do an RPC.
- **New slash command** → add a case in `cmd/axon/commands.go`. Add the underlying operation as a method on `*Agent` if needed.
- **New observability sink** → write a function and pass it as `Config.OnEvent`. Fan-out is a one-line closure.
- **New session store** → embedders pass their own `*Session` to `Config.Session`. The runtime works with whatever it gets.
- **New provider** → extend `LoadProviders` in `providers.go`; the streaming layer is OpenAI-compatible and already handles most.

## Things intentionally NOT here

- **No subagents.** One LLM, full context every turn, aggressive forgetting is the cost lever.
- **No HTTP/API layer.** Build one on top with `Step`.
- **No agent registry / discovery / lifecycle.** That belongs to a higher layer (the "docker for agents" surface this runtime was extracted to support).
- **No MCP client yet.** Reserved as a tool kind, not implemented.
- **No sandbox or per-tool permission prompt.** The model decides what's destructive; the embedder gates with `Interrupt` and `Undo`.
- **No YAML in the runtime.** YAML is a CLI concern. The runtime's contract is `Config`.
