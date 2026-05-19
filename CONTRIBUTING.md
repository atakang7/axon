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

axon uses [Conventional Commits 1.0.0](https://www.conventionalcommits.org/en/v1.0.0/). Every commit on `main` must follow the format — a CI job (`commitlint`) enforces this on pull requests, and the release pipeline derives the next semver from these prefixes. **There is no manual versioning step**; commits drive releases.

**Format**

```
<type>(<optional scope>): <short imperative subject>

<optional body explaining why, wrapped at ~72 cols>

<optional footer(s), including BREAKING CHANGE>
```

**Allowed types and what they release**

| Type       | Purpose                                              | Release bump |
| ---------- | ---------------------------------------------------- | ------------ |
| `feat`     | New user-facing feature or public-API addition       | **minor**    |
| `fix`      | Bug fix                                              | **patch**    |
| `perf`     | Performance improvement with no behavior change      | **patch**    |
| `refactor` | Internal restructure, no behavior change             | **patch**    |
| `docs`     | Documentation only                                   | **patch**    |
| `build`    | Build system, `go.mod`, release tooling              | **patch**    |
| `test`     | Adding or fixing tests                               | none         |
| `ci`       | CI configuration                                     | none         |
| `chore`    | Maintenance not covered above                        | none         |
| `style`    | Formatting, whitespace, no code change               | none         |
| `revert`   | Reverts a previous commit                            | varies       |

**Breaking changes** force a **major** bump regardless of type. Mark them either with `!` after the type (`feat!: drop NewBare constructor`) or with a `BREAKING CHANGE:` footer:

```
feat: collapse Handler interface into Config.OnEvent

BREAKING CHANGE: agent.Handler, HandlerFunc, MultiHandler are removed.
Set Config.OnEvent to a closure instead.
```

**Examples**

```
feat(agent): add SessionPath helper on *Agent
fix(exec): cancel background shell on Interrupt
perf(pruner): skip token count when last fire still fresh
docs: align README minimum embed with required SystemPrompt
refactor(memory): move Park/Forget out of tool surface
ci: enforce conventional commits on PRs
chore: bump goreleaser action to v6
```

Subjects are lowercase, imperative ("add", not "added" or "adds"), and ≤ 100 chars.

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

## Releases

Releases are fully automated. Every push to `main` runs [semantic-release](https://semantic-release.gitbook.io/) which:

1. Reads the conventional-commit messages since the last tag.
2. Computes the next semver (major / minor / patch / none) per the table above.
3. Updates `CHANGELOG.md` and commits it back as `chore(release): X.Y.Z [skip ci]`.
4. Creates and pushes the `vX.Y.Z` tag.
5. Triggers [goreleaser](https://goreleaser.com/) which cross-compiles `cmd/axon` for linux/darwin/windows × amd64/arm64 and publishes a GitHub Release with binaries and checksums.

There is no manual `git tag` step. To ship a feature, merge a `feat:` commit to `main`; to ship a fix, merge a `fix:` commit. To skip a release entirely (e.g. internal CI tweaks), use `chore:`, `ci:`, `test:`, or `style:`.

Configuration lives in:

- `.releaserc.json` — semantic-release plugins and rules
- `.goreleaser.yaml` — binary build matrix
- `.commitlintrc.json` — accepted commit types
- `.github/workflows/release.yml` — the release job

## Questions?

- Check existing issues and PRs first
- Open an issue for bugs or feature requests
- Keep discussions focused and constructive
