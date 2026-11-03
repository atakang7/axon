---
title: Retries
description: HTTP, transport and unusable-reply retries.
---

`Agent.completeWith` retries selected provider errors until a response succeeds or attempts run out.

| Setting | Default | What |
|---------|---------|------|
| `retry.max_attempts` | 11 | Total requests including the first |
| `retry.backoff_cap` | 60s | Delay ceiling |
| `retry.on_status` | 429, 500, 502, 503, 504 | Retried HTTP responses |

Backoff starts at 5 seconds and doubles to the 60-second cap.

## Limits

- Network timeouts and connection failures can be retried; cancelled turns stop.
- Most unusable responses get one retry; reasoning-only responses use a separate limit.
- `Retry-After` may increase the delay up to the configured cap.
