# axon

Axon is a Go runtime for developers embedding tool-using language model agents in applications.

```text
+-------------+  Step  +-------+  Complete  +--------+  HTTP  +----------+
| application | -----> | Agent | ---------> | Client | ----> | provider |
+-------------+        +-------+            +--------+        +----------+
                         |
                   calls v
                      +-------+
                      | tools |
                      +-------+
```

## Quick start

Requires Go 1.26.2.

```sh
mkdir axon-demo && cd axon-demo
go mod init example.com/axon-demo
go get github.com/atakang7/axon/v2
```

Save the [agent example](docs/src/content/docs/how-to/first-agent.md) as `main.go`, set `OPENROUTER_API_KEY` and run `go run .`.

## Layout

| Path | What it is |
| --- | --- |
| [Go package](README.md) | `package axon`: turn loop, tools, sessions, clients and settings |
| [docs/](docs/README.md) | Astro Starlight documentation site |

## Docs

- [Run an agent](docs/src/content/docs/how-to/first-agent.md)
- [Go API](docs/src/content/docs/reference/api.md)
- [Built-in tools](docs/src/content/docs/reference/tools.md)
- [Settings and defaults](docs/src/content/docs/reference/settings.md)
- [Architecture](docs/src/content/docs/explanation/architecture.md)
- [Contributing](CONTRIBUTING.md)

## Limits

- The shipped client requires an OpenAI-compatible streaming endpoint.
- `Step`, `Run` and `Reset` are not safe for concurrent calls.
- `MaxIterations=0` leaves a turn without a model-call limit.
- Shell tools run with the application's operating system permissions.
