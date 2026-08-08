---
title: Configuration reference
description: Settings schema, defaults, units, provider configuration, and path overrides.
---

`Settings` maps field-for-field to `axon.yaml`. Zero/empty operational fields are filled from `DefaultSettings()`.

## Providers

```yaml
providers:
  <name>:
    base_url: https://example.com/v1
    api_key: ${API_KEY}
    models:
      <model-id>:
        route: optional-routing-name
```

`base_url` is required for every configured provider. Every provider must list at least one model when loading from YAML.

`route` is the only current per-model option and is converted to an OpenRouter-style provider routing payload.

## Defaults

### `session`

| Field | Default |
| --- | --- |
| `data_dir` | `$XDG_DATA_HOME/agent` or `~/.local/share/agent` |
| `path` | empty → derived per process cwd |

### `model`

| Field | Default |
| --- | ---: |
| `max_tokens` | `20000` |
| `request_timeout` | `30m` |
| `idle_timeout` | `20s` |
| `reasoning_effort` | empty |
| `exclude_reasoning` | `false` |

### `retry`

| Field | Default |
| --- | --- |
| `max_attempts` | `10` |
| `backoff_cap` | `60s` |
| `on_status` | `429, 500, 502, 503, 504` |

Retry waits are exponential after the first failed attempt (`2s`, `4s`, `8s`, ...) and capped by `backoff_cap`.

### `tools.read`

| Field | Default |
| --- | ---: |
| `lines` | `200` |
| `max_bytes` | `2 MiB` |

### `tools.exec`

| Field | Default |
| --- | ---: |
| `timeout` | `30s` |
| `max_timeout` | `10m` |
| `output_bytes` | `12000` |
| `tail_lines` | `50` |
| `max_tail_lines` | `500` |
| `kill_grace` | `2s` |

### `tools.bash_output`

| Field | Default |
| --- | ---: |
| `max_bytes` | `32 KiB` |

### `tools.search`

| Field | Default |
| --- | ---: |
| `timeout` | `30s` |
| `max_matches` | `100` |
| `output_bytes` | `12000` |

### `pruner`

| Field | Default |
| --- | ---: |
| `mode` | `moderate` |
| `window_blocks` | `30` |
| `floor_tokens` | `10000` |
| `growth_tokens` | `5000` |
| `max_tokens` | `4096` |
| `timeout` | `60s` |

Valid modes are `low`, `moderate`, and `extreme`.

## Duration syntax

`Duration` accepts Go duration strings such as:

```yaml
request_timeout: 30m
idle_timeout: 20s
timeout: 1h30m
```

A bare number is interpreted as seconds. Negative durations are rejected during YAML decoding.

## Byte-size syntax

`Bytes` accepts decimal and binary suffixes:

```yaml
max_bytes: 2MB
max_bytes: 2MiB
output_bytes: 12KB
```

Supported units include `B`, `K/KB/KiB`, `M/MB/MiB`, and `G/GB/GiB` (case-insensitive after normalization). A bare integer is bytes.

## Zero-value rule

`WithDefaults` treats non-positive numeric/duration/byte fields as unset. A meaningful literal zero therefore cannot currently be expressed for those settings; it becomes the default.

## Environment/path overrides

| Variable | Meaning |
| --- | --- |
| `AXON_CONFIG` | settings-file path |
| `AXON_ENV` | credentials-file path |
| `AXON_DATA_DIR` | state root override |
| `AXON_SESSION_PATH` | exact session-file override |
| `XDG_CONFIG_HOME` | base for default config + env paths |
| `XDG_DATA_HOME` | base for default state path |

Other operational limits are not read from environment variables by the current runtime.
