# Contributing

Thank you for considering contributing to axon! Please follow these guidelines to keep the codebase consistent and maintainable.

## Getting started

### Prerequisites
- Go 1.26.2 or later
- Git

### Setup
```sh
git clone https://github.com/atakang7/axon
cd axon
go build ./...  # verify it compiles
```

The repo currently ships without tests — they were removed during the runtime/CLI split and will be reintroduced against the new API. New behavior changes should land with tests.

## Philosophy

**Minimalism is paramount.** axon is intentionally small and focused. Before adding anything, ask:
- Is this truly essential to the core experience?
- Could this be done by the user (or the agent) instead of being built‑in?
- Does it align with the "terminal coding agent" vision?

**No unnecessary abstractions.** Prefer concrete, straightforward code. If you find yourself creating interfaces or factories, reconsider.

**Self‑documenting code.** Comments are for *why*, not *what*. The code should speak for itself.

## Guidelines

### Code style
- Follow standard Go conventions (gofmt, go vet, goimports)
- Use short, descriptive names
- Functions should do one thing
- Keep functions under 50 lines when possible
- Export only what's needed

### Testing
- Every change that touches logic should have a test (the suite is being rebuilt; new tests are welcome)
- Tests should be fast and isolated
- Use table‑driven tests for similar cases
- Mock external dependencies (filesystem, HTTP) when appropriate
- Run `go build ./...` before submitting; run `go test ./...` once tests exist in the area you touched

### Pull requests
- **One feature/fix per PR** – keep changes focused
- **Descriptive title** – what changed, not "Update foo"
- **Clear description** – what problem it solves, how it works
- **Reference issues** – link to related issues
- **Update documentation** – if behavior changes, update README or comments

### Commit messages
Use conventional commit style:
```
feat: add /stats command
fix: handle empty providers.json gracefully
docs: expand tool documentation
test: cover edge case in write
refactor: simplify memory archiving logic
```

## Development workflow

1. **Fork** the repository
2. **Create a branch** from `main`
3. **Make your changes** (with tests where applicable)
4. **Verify** with `go build ./...`
5. **Update documentation** if behavior or surface changes
6. **Push** to your fork
7. **Open a pull request**

## Project structure

axon is split into a runtime library and a reference CLI:

```
axon/
├── agent/                     # the runtime library (import this)
│   ├── api.go                 # Config, New, Step, Run, Reset, Undo, Cd, Session, SessionPath, Close
│   ├── agent.go               # Agent struct, chat/retry, runTool
│   ├── handler.go             # Event, Kind, ToolEvent, PruneInfo, SessionInfo
│   ├── exports.go             # DataDir, ConfigDir, ProvidersPath, SessionPath, EnvString, ...
│   ├── session.go             # append-only session log, edit history, undo
│   ├── memory.go              # park/recall/forget Session methods; TaskTool
│   ├── prompt.go              # buildSystemPrompt (role + catalog + probes + orientation)
│   ├── pruner.go              # secondary-LLM pruner
│   ├── providers.go           # Provider type + LoadProviders
│   ├── config.go              # env/XDG path resolution
│   ├── llm.go                 # OpenAI-compatible streaming chat client
│   ├── tools.go               # Tool type, schema helpers, tool-name constants
│   ├── tools_helpers.go       # atomic writes, formatters, binary refusal
│   ├── tool_read.go           # ReadTool
│   ├── tool_write.go          # WriteTool
│   ├── tool_search.go         # SearchTool
│   ├── tool_exec.go           # ExecTool, BashOutputTool, KillShellTool
│   ├── bg.go                  # background shell registry
│   └── probes.go              # language/build detection
├── cmd/axon/                  # reference CLI (one consumer of the library)
│   ├── main.go                # entry point: flags → Config → agent.New → REPL
│   ├── picker.go              # interactive provider picker
│   ├── yamlcfg.go             # YAML agent personality loader
│   ├── customtool.go          # YAML ToolConfig → agent.Tool adapter
│   ├── tty_handler.go         # TTY renderer consuming agent.Event
│   ├── commands.go            # slash-command dispatch
│   └── input.go               # paste-aware stdin reader
├── examples/minimal/          # smallest possible embed
├── benchmark/                 # benchmark scripts
├── README.md
├── ARCHITECTURE.md
├── CONTRIBUTING.md
├── CHANGELOG.md
├── LICENSE
├── go.mod
└── go.sum
```

The runtime knows nothing about terminals, flags, YAML, or `os.Exit` — those concerns live in `cmd/axon`. Library users build `agent.Config` directly.

## Questions?

- Check existing issues and PRs first
- Open an issue for bugs or feature requests
- Keep discussions focused and constructive
