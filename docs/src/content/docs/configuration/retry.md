---
title: Retry policy
description: Configure which primary-model failures Axon retries and how long it backs off.
---

Retry policy belongs to the **agent loop**, not the shipped HTTP client. It applies around `Model.Complete`, so it can also retry a custom model when that model returns error types Axon recognizes.

## Defaults

```yaml
retry:
  max_attempts: 10
  backoff_cap: 60s
  on_status: [429, 500, 502, 503, 504]
```

| Field | Default | Meaning |
| --- | --- | --- |
| `max_attempts` | `10` | total attempts including the first |
| `backoff_cap` | `60s` | ceiling on exponential delay |
| `on_status` | `429,500,502,503,504` | HTTP status codes retried for `*APIError` |

## Backoff schedule

There is no delay before the first request.

After a retryable failure, Axon waits approximately:

```text
attempt 2 → 2s
attempt 3 → 4s
attempt 4 → 8s
attempt 5 → 16s
...
```

The duration is capped by `backoff_cap`. The current algorithm has no jitter.

Before sleeping, Axon emits an `info` event describing the retry and delay.

## Always-classified transport failures

Independent of `on_status`, Axon considers these retryable:

- `io.EOF`;
- `io.ErrUnexpectedEOF`;
- `net.Error` timeouts;
- connection refused;
- connection reset;
- DNS errors.

They are still bounded by `max_attempts`.

## Never retried

Explicit `context.Canceled` and `context.DeadlineExceeded` are returned immediately.

Unknown error types that are neither `APIError` nor one of the recognized transport cases are also returned immediately.

## Disable retries

Set:

```yaml
retry:
  max_attempts: 1
```

An empty `on_status` list is not a reliable way to disable HTTP retries because `WithDefaults()` replaces an empty list with the default statuses.

## Empty successful responses

A technically successful model call that returns neither assistant text nor tool calls is treated as an error.

That path has a special rule: Axon retries once, then fails on the second empty response even if `max_attempts` is larger.
