---
title: Complete axon.yaml
description: Every current Settings field in one code-grounded configuration example.
---

This example contains the complete current `Settings` YAML surface. Values shown are the runtime defaults where a default exists; provider values are illustrative placeholders.

```yaml
providers:
  openrouter:
    base_url: https://openrouter.ai/api
    api_key: ${OPENROUTER_API_KEY}
    models:
      vendor/model-a: {}
      vendor/model-b:
        route: Optional Route Name

session:
  # Empty takes the default state root:
  # $AXON_DATA_DIR, else $XDG_DATA_HOME/agent,
  # else ~/.local/share/agent
  data_dir: ""

  # Empty derives one session per process cwd.
  # AXON_SESSION_PATH overrides this field.
  path: ""

model:
  max_tokens: 20000
  request_timeout: 30m
  idle_timeout: 20s
  reasoning_effort: ""
  exclude_reasoning: false

retry:
  max_attempts: 10
  backoff_cap: 60s
  on_status: [429, 500, 502, 503, 504]

tools:
  read:
    lines: 200
    max_bytes: 2MiB

  exec:
    timeout: 30s
    max_timeout: 10m
    output_bytes: 12000
    tail_lines: 50
    max_tail_lines: 500
    kill_grace: 2s

  bash_output:
    max_bytes: 32KiB

  search:
    timeout: 30s
    max_matches: 100
    output_bytes: 12000

pruner:
  mode: moderate
  window_blocks: 30
  floor_tokens: 10000
  growth_tokens: 5000
  max_tokens: 4096
  timeout: 60s
```

## Credentials file

With the provider above, the default credentials file could contain:

```dotenv
OPENROUTER_API_KEY=...
```

## Important non-obvious semantics

- `providers` and `model` are applied when the application calls `Settings.NewClient`; `Agent.New` does not auto-create/reconfigure a model from them.
- `pruner.window_blocks` applies to primary context projection even when no secondary pruner model is configured.
- non-positive numeric/duration/byte settings are generally replaced by defaults.
- `tools.exec.tail_lines` is currently not used as a fallback by foreground exec, which requires the tool call to provide `tail_lines`.
- `tools.bash_output.max_bytes`, `tools.search.max_matches`, and `tools.read.lines` are defaults, not hard maximums on positive per-call overrides.
- `tools.search.max_matches` maps to ripgrep `--max-count`, which is per file.

Read the individual configuration pages before tuning these values in production.
