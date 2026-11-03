---
title: Quickstart
description: Run one agent turn from a Go module.
---

Use Go 1.26.2 or later with an OpenAI-compatible API key.

## Run

```sh
mkdir axon-demo
cd axon-demo
go mod init example.com/axon-demo
go get github.com/atakang7/axon/v2
```

Create `main.go` from the [README example](https://github.com/atakang7/axon#run), then run:

```sh
AXON_BASE_URL=https://openrouter.ai/api \
AXON_MODEL=deepseek/deepseek-v3.2 \
OPENROUTER_API_KEY='your-key' go run .
```

## Config

| Var | Default | What |
|-----|---------|------|
| `AXON_BASE_URL` | unset | Endpoint root in the example |
| `AXON_MODEL` | unset | Provider model ID |
| `OPENROUTER_API_KEY` | unset | API key in the example |

For YAML configuration, copy `axon.example.yaml` to `~/.config/axon/axon.yaml` and `.env.example` to `~/.config/axon/.env`, then call `axon.Load()` in your application.

## Limits

- Call `agent.Close()` when finished to stop background shells and MCP subprocesses.
- Set `MaxIterations` on unattended agents.
- `Step` and `Run` are not safe to call concurrently on one agent.
