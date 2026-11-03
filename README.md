# Axon

Axon is a Go library for running tool-using language model agents against an OpenAI-compatible streaming API. It handles model turns and session storage so an application can focus on its own interface and workflow.

## Quick start

Requires Go 1.26.2 or newer and an API key for the endpoint you choose.

```sh
mkdir axon-demo
cd axon-demo
go mod init example.com/axon-demo
go get github.com/atakang7/axon/v2
```

Create `main.go`:

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"
    "time"

    "github.com/atakang7/axon/v2"
)

func main() {
    key := os.Getenv("OPENROUTER_API_KEY")
    if key == "" {
        log.Fatal("OPENROUTER_API_KEY is required")
    }

    model, err := axon.OpenAI(axon.ClientConfig{
        Provider: axon.Provider{
            BaseURL: os.Getenv("AXON_BASE_URL"),
            Model:   os.Getenv("AXON_MODEL"),
            APIKey:  key,
        },
    })
    if err != nil {
        log.Fatal(err)
    }

    agent, err := axon.New(axon.Config{
        Model:        model,
        SystemPrompt: "You are a coding assistant.",
        MaxIterations: 20,
    })
    if err != nil {
        log.Fatal(err)
    }
    defer agent.Close()

    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
    defer cancel()
    result, err := agent.Step(ctx, "List the files in this directory.")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(result.Assistant)
}
```

Run it with your endpoint settings (the model name must exist at that endpoint):

```sh
export AXON_BASE_URL=https://openrouter.ai/api
export AXON_MODEL=deepseek/deepseek-v3.2
export OPENROUTER_API_KEY='your-key'
go run .
```

## Configuration

The example above reads the environment directly. Axon does not load `.env` files unless the application calls `axon.Load()` or `axon.LoadFrom()`.

For file-backed settings, copy `axon.example.yaml` to `~/.config/axon/axon.yaml` and `.env.example` to `~/.config/axon/.env`, then fill in the key. Call `axon.Load()`, followed by `settings.NewClient("openrouter", "deepseek/deepseek-v3.2")`. Set `AXON_CONFIG` or `AXON_ENV` to use other file locations. Keep `.env` out of source control.

Request and stream-idle timeouts default to 30 minutes and 20 seconds. Change them through `ClientConfig` or the YAML model settings.

## How it works

`axon.New` binds a model to the built-in file and shell tools. Each `Step` sends a turn to the provider, executes tool calls, and returns the final reply. Sessions are stored on disk, with one default session per working directory. Applications may pass their own tools or subscribe to events.

See the [API and configuration documentation](https://atakang7.github.io/axon/) for sessions, custom tools and MCP servers.

## Deployment and usage

Axon is a library, not a standalone service. Build and deploy the Go program that imports it. Call `Close()` when shutting down to terminate background commands and MCP clients.

By default, session files live in `~/.local/share/agent/sessions/` (or under `XDG_DATA_HOME` when set). Set `AXON_DATA_DIR` to move local state.

To check a change to this repository:

```sh
go build ./...
go vet ./...
go test -race ./...
```

The documentation site is built separately with `cd docs && npm ci && npm run build`.

## Limitations

Built-in file and shell tools run with the host process's permissions. Axon is not an operating-system sandbox; restrict the agent's working directory and credentials accordingly.

`Step`, `Run` and `Reset` must not run concurrently on one agent. The default turn has no iteration limit; set `MaxIterations` for unattended jobs. Providers must support streaming chat completions.

## License

[MIT](LICENSE)
