---
title: axon.yaml Reference
description: Every field in the configuration file.
---

Copy to `~/.config/axon/axon.yaml`. No secrets in this file — `${VAR}` is resolved from the `.env` beside it. Safe to commit.

Every field is optional. Unset fields take defaults. A config that sets 3 things is better than one that restates 30.

## Complete reference

```yaml
# ──────────────────────────────────────────
# Providers — where requests go
#
# One protocol: OpenAI-style streaming chat completions at
# {base_url}/v1/chat/completions with a bearer token.
# ──────────────────────────────────────────
providers:
  openrouter:
    base_url: https://openrouter.ai/api
    api_key: ${OPENROUTER_API_KEY}          # resolved from .env
    models:
      qwen/qwen3.6-flash:                  # no options needed

      deepseek/deepseek-v3.2:
        route: Baidu Qianfan               # OpenRouter routing hint

      moonshotai/kimi-k2.5:
        route: modelrun/fp4

  ollama:
    base_url: http://localhost:11434        # no key needed
    models:
      qwen2.5-coder:7b:

# ──────────────────────────────────────────
# Session — where conversation state lives
# ──────────────────────────────────────────
session:
  data_dir: ~/.local/share/agent            # root for sessions + bg logs
  # path: /tmp/pinned-session.json          # pin one file (rarely wanted)

# ──────────────────────────────────────────
# Model — how each request is shaped
# ──────────────────────────────────────────
model:
  max_tokens: 20000                         # cap per reply
  request_timeout: 30m                      # whole streamed response
  idle_timeout: 20s                         # gap between chunks
  reasoning_effort: ""                      # "", low, medium, high
  exclude_reasoning: false

# ──────────────────────────────────────────
# Retry
# ──────────────────────────────────────────
retry:
  max_attempts: 10                          # includes first try
  backoff_cap: 60s                          # ceiling on exponential wait
  on_status: [429, 500, 502, 503, 504]      # HTTP codes to retry

# ──────────────────────────────────────────
# Tools — caps on what a single tool call costs
# ──────────────────────────────────────────
tools:
  read:
    lines: 200                              # default slice size
    max_bytes: 2MiB                         # full read cap

  exec:
    timeout: 30s                            # foreground default
    max_timeout: 10m                        # ceiling on model-requested
    output_bytes: 12000                     # captured output
    tail_lines: 50                          # kept when output overflows
    max_tail_lines: 500                     # ceiling on tail
    kill_grace: 2s                          # wait after SIGKILL

  bash_output:
    max_bytes: 32KiB                        # one background poll

  search:
    timeout: 30s
    max_matches: 100
    output_bytes: 12000

# ──────────────────────────────────────────
# Pruner — secondary model that parks stale context
# ──────────────────────────────────────────
pruner:
  mode: moderate                            # low | moderate | extreme
  window_blocks: 30                         # free recency window
  floor_tokens: 10000                       # below = skip prune
  growth_tokens: 5000                       # accumulate before re-prune
  max_tokens: 16000                         # curator reply cap
  timeout: 60s
```

## Value types

| Type | Examples | Notes |
|------|----------|-------|
| Duration | `30s`, `10m`, `1h30m` | Bare number = seconds |
| Bytes | `12KB`, `2MiB`, `32KiB` | KB=1000, KiB=1024. Bare number = bytes |

## Providers

Axon speaks exactly one protocol: OpenAI-style streaming chat completions. Any endpoint that speaks it works.

- `base_url` — API root. `/v1` appended when absent. **Required.**
- `api_key` — bearer token. Write as `${VAR}` to resolve from `.env`.
- `models` — models this endpoint may be asked for. Axon does not choose between them — it only lists. Your application pins one.
- `route` — OpenRouter routing hint. Ignored by endpoints that don't understand it.

## The .env file

Beside `axon.yaml`. Contains only secrets.

```bash
# ~/.config/axon/.env
OPENROUTER_API_KEY=sk-or-...
```

`${VAR}` in the config is resolved from this file first, then `os.LookupEnv` as fallback. An unresolved reference is an error at load time, not a silent empty string.
