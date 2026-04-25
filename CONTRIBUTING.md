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
go test ./...   # verify tests pass
```

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
- Every change that touches logic should have a test
- Tests should be fast and isolated
- Use table‑driven tests for similar cases
- Mock external dependencies (filesystem, HTTP) when appropriate
- Run `go test ./...` before submitting

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
3. **Make your changes** with tests
4. **Verify** with `go build ./... && go test ./...`
5. **Update documentation** if needed
6. **Push** to your fork
7. **Open a pull request**

## Project structure

```
axon/
├── main.go          # Entry point, provider selection
├── agent.go         # Core loop
├── llm.go           # OpenAI‑compatible HTTP client
├── session.go       # Persistent session (history, edits)
├── memory.go        # Archiving/retrieval system
├── tools.go         # Flat tool implementations
├── ui.go            # Terminal UI
├── agent_test.go    # Unit tests
├── README.md        # User documentation
├── CONTRIBUTING.md  # This file
├── CHANGELOG.md     # Release history
├── go.mod           # Go module definition
└── .github/         # CI, issue templates, PR template
```

## Questions?

- Check existing issues and PRs first
- Open an issue for bugs or feature requests
- Keep discussions focused and constructive
