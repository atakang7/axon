# Axon — Architecture

Core agent runtime. One binary, many agents.

The runtime ships a fixed set of built-in tools (the "hands and legs": read,
write, exec, search, task, bash_output, kill_shell). Agents are YAML files
that combine a role prompt with optional custom tools. The same loop runs
every agent — there is no agent-specific code in the runtime.

## Layout

```
cmd/axon/main.go              entry point — calls agent.Main()
internal/agent/               package agent — all runtime logic
  run.go                      Main(): flags, providers, agent config, REPL wire-up
  agent.go                    Agent struct + Run loop (chat → tool calls → repeat)
  commands.go                 slash commands (/reset, /undo, ...)
  prompt.go                   role prompt + tool catalog + project orientation
  session.go                  Session struct, persistence, undo log, tasks
  memory.go                   park/recall/forget projections; TaskTool lives here
  llm.go / providers.go       provider-agnostic chat client + provider resolution
  pruner.go                   secondary LLM that drops/parks old messages
  agentconfig.go              YAML loader for agent personalities
  customtool.go               adapter: ToolConfig → Tool (today: shell only)
  tools.go                    Tool type, BuildTools, schema helpers
  tools_helpers.go            atomic writes, formatters, binary refusal
  tool_{read,write,search,exec}.go  built-in tool implementations
  bg.go                       background shell registry (servers, watchers)
  picker.go / config.go       provider picker + env tunables
  ui.go / probes.go / input.go / jsonl_logger.go   terminal I/O + observability
examples/agents/              reference agent configs
```

## The turn loop

```
user input
    │
    ▼
Agent.Run ──► chat() ──► Client.ChatStream (provider streaming)
    ▲             │
    │             ▼
    │       tool_calls?
    │         │     │
    │        no    yes
    │         │     │
    │         │     ▼
    │         │   runTool() → Tool.Fn (built-in OR custom)
    │         │     │
    │         │     ▼
    │         │   append result to Session.Messages
    │         └────►(loop until assistant emits text)
    │
    ▼
Pruner.ShouldFire? → Pruner.Prune (parks/forgets old blocks)
    │
    ▼
Session.Save (JSON on disk)
```

Built-in and custom tools share the same `Tool` shape — once `BuildTools`
returns the list, the loop is indifferent to origin.

## Agents

An agent is a YAML file at `$AXON_AGENTS_DIR/<name>.yaml`
(default `~/.config/axon/agents/`). Selected with `axon --agent <name>`.
No flag = built-in default agent.

```yaml
name: reviewer
system_prompt: ./reviewer.md          # role behavior, NOT tool docs
disable_builtins: [write, exec]       # subtract from the "hands and legs"
tools:                                # add specialized capabilities
  - name: submit_review
    type: shell                       # only kind today; mcp is reserved
    description: ...
    schema: { ... JSON Schema ... }
    command: gh pr review --{{.verdict}} --body {{.body | shellQuote}}
    timeout_seconds: 10
```

The runtime composes the final system prompt as:

```
[role prompt: user-supplied or defaultRolePrompt]
[built-in tool catalog: only enabled built-ins]
[runProbes(cwd) — language/build probes if any]
[projectOrientation(cwd) — file tree snapshot]
```

Custom tools advertise themselves through the LLM tool schema, not the
prompt.

## Custom tools

Today only `type: shell`. Implementation in `customtool.go`:

1. Parse `command` and `cwd` as Go `text/template`.
2. At call time, JSON-decode the LLM's args into a map.
3. Render the templates against the map; values can be piped through
   `shellQuote` (POSIX single-quote escaping) to prevent injection.
4. Spawn `sh -c <rendered>` bound to the turn context, with a per-tool
   timeout (default 60s). Combined stdout/stderr is the tool result.

`type: mcp` is rejected at load time as "reserved" — it is the
single extension point. Adding it later means one more arm in
`buildCustomTool`'s switch; nothing else in the framework changes.

## Invariants

- **`Session.Messages` is append-only.** Park/recall/forget are
  projections built in `ContextMessages`, never mutations. Audit
  history stays intact even after pruning.
- **Built-ins are always available unless explicitly disabled.** An
  agent config can subtract, never silently replace.
- **Custom tool names cannot collide with built-ins.** Enforced at
  load time in `AgentConfig.validate`.
- **Writes are atomic.** Every file mutation goes through
  `writeBytesRaw` (tmp-file + rename). Formatters run after, never
  during, so /undo is byte-exact.
- **Tool execution is turn-scoped.** Ctrl-C cancels the in-flight
  chat AND kills the running tool's process group. Background shells
  (`bg.go`) outlive turns but die on process exit.
- **Reason field required on every tool call.** Forces the model to
  articulate intent before paying the call's token cost.

## Things that are intentionally NOT here

- No subagents. One LLM, full context every turn, aggressive forgetting
  is the cost lever.
- No HTTP/API layer. Was removed during the framework refactor; the
  runtime is CLI-only.
- No agent registry / discovery / lifecycle management. That belongs to
  a higher layer (the "docker for agents" surface), not the runtime.
- No MCP client yet. Reserved, not implemented.
- No sandbox or per-tool permission prompt. The LLM decides what's
  destructive; the user gates with Ctrl-C and undo.

## Extending

- **New built-in tool** → add `tool_<name>.go` with a `<Name>Tool(s *Session) Tool`
  constructor and register it in `BuildTools` in `tools.go`.
- **New custom tool type** (e.g. `mcp`) → add the arm in
  `buildCustomTool` in `customtool.go`. Validation slot is already in
  `AgentConfig.validate`.
- **New slash command** → add a case in `Agent.handleSlash` in
  `commands.go`.
- **New provider** → extend `LoadProviders` in `providers.go`; the
  streaming layer is OpenAI-compatible and already handles most.
