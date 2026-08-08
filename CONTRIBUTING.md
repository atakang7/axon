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

Run `go test -race ./...`. Tests run against real files and real processes in `t.TempDir()`, never mocks of our own code, and never touch the network — the turn loop is exercised against a scripted `Model`. New behaviour changes land with tests.

## Philosophy

**Minimalism is paramount.** axon is intentionally small and focused. Before adding anything, ask:
- Is this truly essential to the core experience?
- Could this be done by the user (or the agent) instead of being built‑in?
- Is it a *runtime* concern, or a product decision? The runtime reads no config file, shells out to nothing at startup, and adds nothing to the system prompt but the tool catalog. Anything that costs tokens on every call belongs to the embedder.

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
- Tests should be fast and isolated; use `t.TempDir()` and `t.Setenv`
- Use table‑driven tests for similar cases
- Prefer real execution over mocks: real files, real processes, real `rg`. Fake only what crosses the network — implement `agent.Model` for that
- When a test covers a bug fix, verify it fails without the fix before you submit it
- Run `go build ./... && go vet ./... && go test -race ./...` before submitting

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

axon is a library-only repository. The terminal coding agent lives in [bouton](https://github.com/atakang7/bouton).

The runtime knows nothing about terminals, flags, YAML, or `os.Exit`. Library users build `Config` directly.

The project is structured as a flat, single-package architecture (`package axon`) to maximize simplicity and minimize internal boundaries. All core logic (agent, tools, llm client, session) lives in the root directory.

### Architecture

The architecture adheres strictly to Go minimalism:
- **Flat structure**: No nested packages. This forces a cohesive API and prevents dependency cycles.
- **Single responsibility**: Each file handles a specific domain (`agent.go`, `tools.go`, `client.go`, `session.go`).
- **No external dependencies**: The runtime relies entirely on the standard library.

## Releases

Releases are fully automated. Every push to `main` runs [semantic-release](https://semantic-release.gitbook.io/) which:

1. Reads the conventional-commit messages since the last tag.
2. Computes the next semver (major / minor / patch / none) per the table above.
3. Updates `CHANGELOG.md` and commits it back as `chore(release): X.Y.Z [skip ci]`.
4. Creates and pushes the `vX.Y.Z` tag.
5. Publishes a GitHub Release pointing at the tag. axon is a library-only repo; no binaries are shipped here. (Binaries for the terminal CLI live on [bouton's releases](https://github.com/atakang7/bouton/releases).)

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
