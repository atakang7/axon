# Contributing

Axon is a Go library. The documentation site lives in [docs/](docs/README.md).

## Local checks

Go 1.26.2 is specified in `go.mod`. Run these commands from the repository root:

```sh
go build ./...
go vet ./...
go test -race ./...
```

The CI workflow also checks `gofmt -l .` and runs `go test -timeout 5m ./...` on Windows.

For changes to the [documentation site](docs/README.md), use Node.js 22.12.0:

```sh
cd docs
npm ci
npm run build
```

## Changes

Keep changes within the Go package or the documentation site as appropriate. Tests use temporary directories and scripted models for provider behavior. Add a regression test when fixing a reported failure.

Run the checks that cover the changed files before opening a pull request.

## Commits and releases

Commit subjects use [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/). Examples:

```text
fix(client): handle an SSE data field
docs: update the API reference
ci: pin the Node.js runtime
```

Pull requests run the [CI workflow](.github/workflows/ci.yml). Merges to `main` trigger the [release workflow](.github/workflows/release.yml) and the [docs deployment](.github/workflows/deploy-docs.yml). Releases use semantic-release; the release workflow publishes GitHub release notes.
