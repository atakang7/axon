# Axon — Architecture

A Go runtime for building LLM agents. One library, one loop, pluggable everything that can vary.

```
github.com/atakang7/axon/agent  ← the runtime (library, this repo)
github.com/atakang7/bouton      ← reference coding agent built on the runtime (separate repo)
```

The runtime knows nothing about terminals, flags, signals, YAML, or `os.Exit`. All terminal-shaped concerns live in [bouton](https://github.com/atakang7/bouton).

## Layering

This is the single most important rule in the repository.

```
config  ←  llm  ←  session  ←  tools  ←  agent
```

**A package may import anything to its left, and nothing to its right.** The
arrows are dependencies, so `agent` sees everything and `config` sees nothing.

The rule is enforced by the Go compiler, not by review: each layer is its own
package, and an import in the wrong direction is a build error. Every layer
states its own boundary rule in its package comment.

| Layer | Package | Owns | May import |
|---|---|---|---|
| 0 | `internal/config` | XDG/`AXON_*` paths, `Limits` | nothing |
| 1 | `internal/llm` | `Model` port, `Provider`, `Msg`, `ToolCall`, `ToolSpec`, OpenAI `Client` | config |
| 2 | `internal/session` | append-only log, cwd, edit history, task plan | config, llm |
| 3 | `internal/tools` | the seven built-in tools, background shells | config, llm, session |
| 4 | `agent` | `Agent`, the turn loop, events, prompt, pruner | all of the above |

Everything below `agent` lives under `internal/`, so it is unreachable from
outside this module. That is deliberate: those packages are free to change
because nothing external can depend on them. The stable surface is `agent`.

Two boundaries are worth calling out, because they are what keep the graph a
DAG rather than a ball of mutual references:

- **`llm` does not know what a `Tool` is.** It takes `ToolSpec` — name,
  description, schema — and never sees a tool's `Fn`. `agent.toolSpecs`
  projects `Tool` down to `ToolSpec` at the one crossing point, as an
  allowlist. The model layer therefore cannot reach the execution layer.
- **`tools` does not receive a `*Session`.** Each tool takes the narrowest
  interface it needs, declared in `internal/tools/tools.go` on the consuming
  side and satisfied implicitly by `Session`:
  - `Workspace` — `Dir`, `ResolvePath`, `RecordEdit`
  - `Plan` — `RegisterTask`, `AdvanceTask`, `ReplanTask` (write-only)

  A tool cannot read conversation state, and a fake `Workspace` for a test is
  six lines.

## Layout

```
agent/
  api.go              Config, New, Step, Run, Reset, Undo, Cd, Close; re-exported types
  agent.go            Agent struct, chat/retry, runTool
  setup.go            New, Reset, Undo, Cd, Close — construction and lifecycle
  loop.go             Step and Run — the turn loop
  handler.go          Event, Kind, ToolEvent, PruneInfo, SessionInfo
  prompt.go           buildSystemPrompt (role + tool catalog + probes + orientation)
  probes.go           startup shell probes spliced into the system prompt
  pruner.go           secondary LLM that parks old blocks
  exports.go          DataDir, ConfigDir, ProvidersPath, … (path helpers embedders need)

internal/config/
  config.go           paths, Limits, LoadLimits

internal/llm/
  provider.go         Provider, LoadProviders, ResolveProvider
  client.go           Model, Request, Stream, Msg, ToolCall, ToolSpec; OpenAI Client

internal/session/
  session.go          Session, append-only log, edit history, undo, task plan
  memory.go           ContextMessages projection; Park

internal/tools/
  tools.go            Tool, Workspace, Plan, Catalog, schema helpers
  tools_helpers.go    WriteFileAtomic, formatters, binary refusal, output capping
  tool_read.go        ReadTool (skeleton/slice/full)
  tool_write.go       WriteTool (save/replace_string/replace_lines/insert_at_line)
  tool_search.go      SearchTool (literal/regex/trace)
  tool_exec.go        ExecTool, BashOutputTool, KillShellTool
  tool_task.go        TaskTool
  bg.go               BackgroundShells registry (servers, watchers)

examples/minimal/
  main.go             smallest possible embed of agent.New + agent.Step
```

## The turn loop

```
Step(ctx, input)
   │
   ▼
append user msg ─► session.Save
   │
   ▼
prune? ──► Pruner.Prune (parks old blocks)
   │
   ▼
chat() ──► Model.Complete(Request{toolSpecs(tools), Stream{...}})
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

```go
// Construction — built-ins are always present; cfg.Tools are appended.
func New(Config) (*Agent, error)

// Agent
func (a *Agent) Step(ctx, input) (StepResult, error)
func (a *Agent) Run(ctx, InputFunc) error
func (a *Agent) Interrupt() bool
func (a *Agent) Reset()
func (a *Agent) Undo() (path string, ok bool)
func (a *Agent) Cd(path) (string, error)
func (a *Agent) Session() *Session
func (a *Agent) SessionPath() string
func (a *Agent) Close() error

// Config — Model and SystemPrompt are required; the rest default to zero.
type Config struct {
    Model        Model     // agent.OpenAI(...), or your own implementation
    SystemPrompt string
    Tools        []Tool
    Pruner       *Pruner
    Cwd          string
    Session      *Session
    OnEvent      func(ctx, Event)
}

// The model is an interface, so the turn loop can be driven with no network.
type Model interface {
    Complete(ctx context.Context, req Request) (*Msg, error)
}

func OpenAI(OpenAIConfig) (Model, error)  // the implementation that ships

// Tools — the extension surface
type Tool struct {
    Name        string
    Description string
    Schema      map[string]any
    Fn          func(ctx, args) (string, error)
}

// Provider, Msg, ToolCall, Client, ToolSpec, Session, Task, TaskStep and Edit
// are type aliases for the internal packages that own them, so the types are
// identical across both names.

// Errors
var (
    ErrNoProvider, ErrNoSystemPrompt, ErrToolNotFound,
    ErrDuplicateTool, ErrInterrupted, ErrAmbiguousProvider
)
```

## Invariants

- **`Session.Messages` is append-only.** Parking sets projection metadata;
  `ContextMessages` derives the breadcrumb at emission time and the original
  content is never touched. Audit history survives pruning.
- **The pruner has exactly one verb: park.** A block is active or parked,
  nothing else. There is one source of truth for a parked block — the `Msg`
  itself — so no side-table can disagree with the log.
- **Built-ins are unconditional.** Every agent has the hands-and-legs tools
  (read, write, exec, search, task, bash_output, kill_shell). No knob removes
  them. Custom tool names cannot collide with them; enforced at `New`.
- **Nothing is process-global.** Every piece of mutable state an agent uses is
  created in `New` and released in `Close`. Two agents in one process share
  no shells, no limits and no session.
- **Limits are resolved once, at construction.** No tool reads the environment
  at call depth, so two agents can be tuned differently and a test can vary a
  cap without touching `os.Environ`.
- **Tool execution is turn-scoped.** `Interrupt` cancels the in-flight chat and
  kills the running tool's process group. Background shells outlive turns but
  die on `Close`.
- **Writes are atomic.** Every file mutation goes through `WriteFileAtomic`
  (tmp + rename). Formatters run after, never during, so `Undo` is byte-exact.
- **The runtime never writes to stdout.** All observability goes through
  `Config.OnEvent`.
- **The model is a port, not a dependency.** `Config.Model` is an interface, so
  the entire turn loop runs against a scripted fake with no network.

## Extending

- **New built-in tool** → add `internal/tools/tool_<name>.go` with a
  constructor taking the narrowest capability it needs; register it in
  `builtinTools` in `agent/setup.go`.
- **New custom tool kind (e.g. MCP)** → custom tools are `agent.Tool` values
  whose `Fn` does whatever; pass them via `Config.Tools`. No runtime change.
- **New observability sink** → pass a function as `Config.OnEvent`. Fan-out is
  a one-line closure.
- **New session store** → pass your own `*Session` via `Config.Session`.
- **New provider** → extend `LoadProviders` in `internal/llm/provider.go`; the
  streaming layer is OpenAI-compatible and already handles most.

## Testing

Tests run against real files and real processes in `t.TempDir()`, never mocks,
and never touch the network. The sharp edges are covered deliberately:

- `internal/session` — the `ContextMessages` projection: parked blocks take
  their orphaned tool results with them, the log is never mutated, the system
  prompt is unremovable, and `Reset` keeps a session where the embedder put it.
- `internal/tools` — registry independence, the `bash_output` delta contract,
  process-group kills, injected limits, and binary refusal.
- `agent` — the turn loop against a scripted `Model`: tools run until the model
  stops asking, results are fed back into the next request, only schemas cross
  to the model, and events bracket the turn. Plus the layering rule itself.

```
go test ./...
```

## Things intentionally NOT here

- **No subagents.** One LLM, full context every turn, aggressive parking is
  the cost lever.
- **No HTTP/API layer.** Build one on top with `Step`.
- **No agent registry / discovery / lifecycle.** That belongs to a higher layer.
- **No MCP client yet.** Reserved as a tool kind, not implemented.
- **No sandbox or per-tool permission prompt.** The model decides what is
  destructive; the embedder gates with `Interrupt` and `Undo`.
- **No `reason` field on tool calls.** Earlier builds required a justification
  string on every call. It was dropped: it cost latency and tokens on every
  call, and the model's own reasoning trace already serves as the audit log.
- **No YAML in the runtime.** YAML is a CLI concern. The contract is `Config`.
```
