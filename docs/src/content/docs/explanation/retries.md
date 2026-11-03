---
title: Retry policy
description: Why the runtime retries provider failures.
---

`Agent.completeWith` retries configured HTTP statuses, timeouts, transient network errors and truncated streams. It stops after `retry.max_attempts`, which includes the initial attempt.

A complete response can take up to `model.request_timeout`. The SSE client also cancels a stream after `model.idle_timeout` without data. These defaults are 30 minutes and 20 seconds.

The retry policy covers provider transport failures. The agent returns other errors to its caller. A model reply that contains only unusable tool markup gets a bounded retry.
