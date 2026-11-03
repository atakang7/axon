# Axon

Axon is a Go library that runs tool-using agents against OpenAI-compatible streaming APIs.

```text
+-------------+  Step()  +-------+  Complete()  +--------+  HTTP  +----------+
| application | -------> | Agent | ----------> | Client | -----> | provider |
+-------------+          +-------+             +--------+        +----------+
```

## Run

Requires Go 1.26.2 or later.

```sh
mkdir axon-demo
cd axon-demo
go mod init example.com/axon-demo
go get github.com/atakang7/axon/v2
cat > main.go <<'EOF'
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
        Model: model, SystemPrompt: "You are a coding assistant.", MaxIterations: 20,
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
EOF
AXON_BASE_URL=https://openrouter.ai/api \
AXON_MODEL=deepseek/deepseek-v3.2 \
OPENROUTER_API_KEY='your-key' go run .
```

## Config

| Var | Default | What |
|-----|---------|------|
| `AXON_BASE_URL` | unset | API root in the example |
| `AXON_MODEL` | unset | Model ID in the example |
| `OPENROUTER_API_KEY` | unset | Bearer token in the example |
| `AXON_CONFIG` | `~/.config/axon/axon.yaml` | YAML path for `axon.Load()` |
| `AXON_ENV` | `~/.config/axon/.env` | Credentials path for `axon.Load()` |
| `AXON_DATA_DIR` | `~/.local/share/agent` | Session and background-log root |
| `AXON_SESSION_PATH` | per working directory | Override session file |

File-based settings use [`axon.example.yaml`](axon.example.yaml) and [`.env.example`](.env.example); the example above reads environment variables directly.

## CI and release

```text
+------+  push  +-------------+  calls  +--------+  needs  +---------+
| main | -----> | release.yml | -------> | ci.yml | -------> | release |
+------+        +-------------+          +--------+          +---------+
```

Run the local checks with `go build ./...`, `go vet ./...` and `go test -race ./...`; docs build with `cd docs && npm ci && npm run build`.

## Limits

- Built-in shell commands run with the host process's permissions, without a sandbox.
- `Step`, `Run` and `Reset` cannot run concurrently on one agent.
- Turns have no default iteration cap; set `MaxIterations` for unattended use.
- Providers must serve streaming chat completions.

[Documentation](https://atakang7.github.io/axon/) | [MIT license](LICENSE)
