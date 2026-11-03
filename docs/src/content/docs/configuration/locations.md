---
title: File Locations
description: Where Axon reads and writes files.
---

## Config files

| File | Default | Override |
|------|---------|----------|
| `axon.yaml` | `~/.config/axon/axon.yaml` | `$AXON_CONFIG` |
| `.env` | `~/.config/axon/.env` | `$AXON_ENV` |

Both follow XDG: `$XDG_CONFIG_HOME/axon/` when set.

## State files

| File | Default | Override |
|------|---------|----------|
| Session data | `~/.local/share/agent/` | `$AXON_DATA_DIR` or `session.data_dir` in yaml |
| Session file | derived from cwd hash | `$AXON_SESSION_PATH` or `session.path` in yaml |
| Background logs | `{data_dir}/bg/{pid}/` | follows data_dir |

## Session file derivation

Each working directory gets its own session file: `{data_dir}/sessions/{basename}-{hash}.json`.

The basename makes the file recognizable. The hash (SHA-256 of the absolute path, 6 bytes) keeps two same-named directories apart.

```
~/.local/share/agent/sessions/
├── myproject-a3f2c1d8e9f0.json
├── backend-7b2e4d1c5a3f.json
└── ...
```

## Environment variables

Only four survive. All say *where something is*, never *what it contains*.

| Variable | Purpose |
|----------|---------|
| `AXON_CONFIG` | Path to `axon.yaml` |
| `AXON_ENV` | Path to `.env` |
| `AXON_DATA_DIR` | Where state is written |
| `AXON_SESSION_PATH` | Pin one session file |

`AXON_DATA_DIR` and `AXON_SESSION_PATH` override the config rather than defaulting under it — their purpose is to redirect state without editing a file (e.g., a container mounting a volume, or a test that must not touch the developer's real session).
