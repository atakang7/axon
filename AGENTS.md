# Agent contributor notes

Axon is a Go library with a single root `package axon`. The Astro documentation site is under `docs/`. Do not add a CLI or service entry point when documenting it.

## Code paths

- `api.go`, `setup.go`, `loop.go`, `agent.go`: public contract, construction, turns and retries.
- `client.go`: OpenAI-compatible streaming transport.
- `session.go`, `context_budget.go`, `checkpoint.go`: stored history and model context.
- `std_tools.go`, `mcp.go`: local tools and subprocess tools.
- `load.go`, `settings.go`, `axon.example.yaml`: opt-in file settings.

## Changes

- Preserve `Step` / `Run` / `Reset` concurrency boundaries (`api.go`). The embedder supplies a `Model`, system prompt and `OnEvent` callback; `New` does not load config files.
- Do not describe shell tools or MCP subprocesses as sandboxed. Avoid committing credentials or writing secrets into examples.
- If a public API changes, update `docs/src/content/docs/reference/` and the corresponding how-to page. For changed behavior, add or update a nearby `*_test.go`.

## Validation

Run `go build ./...`, `go vet ./...`, `go test -race ./...` and check `gofmt -l .`. For changes under `docs/`, run `cd docs && npm ci && npm run build`. See [RUNBOOK.md](RUNBOOK.md) and [CONTRIBUTING.md](CONTRIBUTING.md).

CI checks Ubuntu, Windows, the docs site and commit messages. Use Conventional Commit subjects (`.commitlintrc.json`); do not claim checks passed without their results.
