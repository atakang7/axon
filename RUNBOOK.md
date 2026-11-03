# Runbook

Axon is a library, not a standalone server. Commands are run from the repository root unless noted. **Verification status: untested** in this documentation pass (no network checkout or Go 1.26.2 locally); CI defines the executable gate.

## 1. Verify the Go package

```sh
go build ./...
go vet ./...
go test -race ./...
gofmt -l .
```

Expected: build, vet and tests exit successfully; `gofmt -l .` prints no filenames. CI runs these on Ubuntu and runs build, vet and tests on Windows (`.github/workflows/ci.yml`).

## 2. Run the consumer example

From a directory outside this repository:

```sh
mkdir axon-demo && cd axon-demo
go mod init example.com/axon-demo
go get github.com/atakang7/axon/v2
```

Copy the `main.go` example from [Run an agent](docs/src/content/docs/how-to/first-agent.md) into this directory, set `OPENROUTER_API_KEY` in your environment, then run:

```sh
go run .
```

Expected: the model responds with the directory listing. **Untested** here: requires a reachable OpenAI-compatible endpoint and a valid credential. This sequence is for a consumer module; the repository itself has no `main` package.

## 3. Build the documentation site

From the repository root:

```sh
cd docs
npm ci
npm run build
```

Expected: Astro writes `docs/dist/`; the directory is ignored by Git (`docs/.gitignore`). **Untested** here; CI builds this site with Node.js 22.12.0. For local preview, run `npm run dev` from `docs/`.

## 4. Session recovery and shutdown

- `Agent.SessionPath()` reports the JSON session path. Normally it is under the configured data directory's `sessions/` folder (`api.go`, `config.go`).
- If session JSON cannot be parsed, `LoadOrCreateSessionAt` attempts to rename it to `<path>.corrupt.<unix-time>` and opens a fresh session (`session.go`). Preserve that file for inspection.
- Call `Agent.Close()` to stop background shells and MCP child processes; `Agent.Reset()` clears in-memory session state (`setup.go`). Neither is a remote infrastructure teardown.
- Session writes are not atomic, and not all later save errors stop a turn. See [known weaknesses](docs/weaknesses.md).

## 5. Related instructions

- [Contributor checks](CONTRIBUTING.md)
- [Settings and file locations](docs/src/content/docs/reference/settings.md)
- [MCP setup](docs/src/content/docs/how-to/connect-mcp.md)
