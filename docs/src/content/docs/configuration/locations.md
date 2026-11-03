---
title: File Locations
description: Configuration and session paths.
---

Axon reads YAML and credentials only when the application calls `Load` or `LoadFrom`.

| Var | Default | What |
|-----|---------|------|
| `AXON_CONFIG` | `~/.config/axon/axon.yaml` | Settings file |
| `AXON_ENV` | `~/.config/axon/.env` | Credential file |
| `AXON_DATA_DIR` | `~/.local/share/agent` | Session and background logs |
| `AXON_SESSION_PATH` | per working directory | Session JSON override |
| `XDG_CONFIG_HOME` | `~/.config` | Base config directory |
| `XDG_DATA_HOME` | `~/.local/share` | Base data directory |

The default session path is `{data_dir}/sessions/{basename}-{12-hex-digit-hash}.json`.

## Limits

- `AXON_SESSION_PATH` and `AXON_DATA_DIR` override YAML paths.
- Keep the credentials file out of Git.
