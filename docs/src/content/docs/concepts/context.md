---
title: Context Management
description: Session history and model-visible context.
---

`Session.Messages` keeps the history while `ContextMessages` returns the model-visible view.

```text
+---------+  reads  +-----------------+  view  +--------+
| Session | ------> | ContextMessages | -----> | Client |
+---------+         +-----------------+        +--------+
```

Checkpoints replace older tool results with short references in the view; the saved messages remain available.

## Config

| Setting | Default | What |
|---------|---------|------|
| `pruner.mode` | `moderate` | Checkpoint threshold policy |
| `pruner.trigger_tokens` | 32,000 in moderate mode | Estimated tokens before a checkpoint |
| `pruner.growth_tokens` | 12,000 | Growth before the next checkpoint |
| `pruner.max_tokens` | 16,000 | Checkpoint reply limit |
| `pruner.timeout` | 5m | Checkpoint request deadline |
| `pruner.keep_runs` | 3 in moderate mode | Recent runs kept unfolded |

`low` triggers at 48,000 tokens; `extreme` triggers at 20,000.

## Limits

- The model input ceiling is 70,000 estimated tokens (`HardContextLimit`).
- Legacy `window_blocks`, `floor_tokens`, `semantic` and `categories` YAML fields are accepted but ignored.
- `Config.Pruner` can supply a separate checkpoint model; otherwise the main model is used.
