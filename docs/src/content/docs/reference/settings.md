---
title: Settings
description: Configuration locations, environment overrides and defaults.
---

`Load()` reads both `axon.yaml` and `.env`. `LoadFrom(configPath, envPath)` accepts explicit paths. `New(Config)` does not call either function.

| File or variable | Default | Use |
| --- | --- | --- |
| `axon.yaml` | `~/.config/axon/axon.yaml` | Providers and runtime policy |
| `.env` | `~/.config/axon/.env` | Credentials used by `${VAR}` in provider settings |
| `AXON_CONFIG` | unset | Override YAML path |
| `AXON_ENV` | unset | Override credentials path |
| `AXON_DATA_DIR` | unset | Override state directory |
| `AXON_SESSION_PATH` | unset | Override session file |
| `XDG_CONFIG_HOME` | `~/.config` | Base for configuration paths |
| `XDG_DATA_HOME` | `~/.local/share` | Base for state directory |

| Setting | Default |
| --- | --- |
| `model.max_tokens` | `20000` |
| `model.request_timeout` | `30m` |
| `model.idle_timeout` | `20s` |
| `retry.max_attempts` | `11` (first attempt included) |
| `retry.backoff_cap` | `60s` |
| `retry.on_status` | `[429, 500, 502, 503, 504]` |
| `tools.read.lines` | `500` |
| `tools.read.max_bytes` | `2MiB` |
| `tools.exec.timeout` | `2m` |
| `tools.exec.max_timeout` | `10m` |
| `pruner.mode` | `moderate` |
| `pruner.growth_tokens` | `12000` |

`pruner.mode` triggers checkpoints at 48,000 (`low`), 32,000 (`moderate`) or 20,000 (`extreme`) estimated tokens. `pruner.trigger_tokens` overrides the trigger.

`axon.example.yaml` in the repository root contains the complete YAML shape.
