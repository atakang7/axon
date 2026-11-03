---
title: axon.yaml Reference
description: Supported YAML settings and defaults.
---

Copy [`axon.example.yaml`](https://github.com/atakang7/axon/blob/main/axon.example.yaml) to `~/.config/axon/axon.yaml` and keep API keys in the adjacent `.env` file.

```yaml
providers:
  openrouter:
    base_url: https://openrouter.ai/api
    api_key: $OPENROUTER_API_KEY
    models:
      deepseek/deepseek-v3.2:
model:
  request_timeout: 30m
  idle_timeout: 20s
retry:
  max_attempts: 11
  on_status: [429, 500, 502, 503, 504]
```

## Config

| Setting | Default | What |
|---------|---------|------|
| `model.max_tokens` | 20000 | Reply token limit |
| `model.request_timeout` | 30m | Full HTTP request |
| `model.idle_timeout` | 20s | Gap between stream chunks |
| `retry.max_attempts` | 11 | Total attempts, including the first |
| `retry.backoff_cap` | 60s | Maximum retry delay |
| `tools.read.max_bytes` | 2 MiB | File-read cap |
| `tools.exec.timeout` | 2m | Default shell timeout |
| `tools.exec.max_timeout` | 10m | Shell timeout ceiling |
| `pruner.mode` | `moderate` | Context checkpoint policy |
| `pruner.growth_tokens` | 12000 | Checkpoint growth threshold |

`$OPENROUTER_API_KEY` resolves from `.env` first, then the process environment.

## Limits

- Missing provider variables fail at load time.
- Unset fields use defaults; refer to [the example file](https://github.com/atakang7/axon/blob/main/axon.example.yaml) for every supported field.
