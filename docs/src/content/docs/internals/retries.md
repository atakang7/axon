---
title: Retry Logic
description: How Axon handles transient failures.
---

Network failures and truncated streams are an inevitable part of speaking to LLM providers. Axon handles these automatically so your application doesn't have to.

## The Retry Loop

Inside `agent.Step()`, the call to the model is wrapped in a retry loop (`agent.chat()`).

```
chat()
│
├─ attempt 0..MaxAttempts:
│   ├─ if attempt > 0: backoff = min(2^attempt seconds, BackoffCap)
│   ├─ emit(KindInfo, "retrying in X seconds")
│   ├─ model.Complete(...)
│   │
│   ├─ success:
│   │   ├─ unusableReply? (empty, leaked tool markup)
│   │   │   → retry on attempt 0, fail on attempt 1+
│   │   └─ return msg
│   │
│   └─ error:
│       ├─ context.Canceled or DeadlineExceeded → stop
│       ├─ APIError with status in OnStatus → retry
│       ├─ io.EOF, io.ErrUnexpectedEOF → retry
│       ├─ net.Error (timeout) → retry
│       ├─ ECONNREFUSED, ECONNRESET → retry
│       ├─ DNSError → retry
│       └─ anything else → stop
│
└─ exhausted → return last error
```

## Transport vs. Policy

Retries are split into two categories:

### 1. Transport failures (Always retried)
If the connection drops, DNS fails, or the stream truncates unexpectedly (`io.EOF`), Axon **always** retries. These failures say nothing about whether the request itself was acceptable.

### 2. HTTP Status Codes (Policy-driven)
If the provider returns an HTTP error code (e.g., 429 Too Many Requests, 502 Bad Gateway), Axon checks `retry.on_status` in the configuration. Only configured status codes are retried.

```yaml
# Default policy
retry:
  max_attempts: 10
  backoff_cap: 60s
  on_status: [429, 500, 502, 503, 504]
```

## Unusable Replies

Sometimes the model returns a 200 OK, but the content is unusable. For example, some models occasionally leak `<tool_call>` XML markup into the text instead of using the proper JSON tool calling API.

Axon intercepts these specific anomalies (`unusableReply`) and treats them as a failure. However, it only retries this once (attempt 0). If the model fails the same way on attempt 1, the error is returned to the embedder to prevent an infinite loop of bad generation.
