---
title: Overview
description: What Axon runs and what the application supplies.
---

Axon manages model turns and tools; the application supplies the model, system prompt and interface.

```text
+-------------+  New()  +-------+  Complete()  +--------+  HTTP  +----------+
| application | ------> | Agent | ----------> | Client | -----> | provider |
+-------------+         +-------+             +--------+        +----------+
```

The `Model` interface has one method:

```go
Complete(ctx context.Context, req Request) (*Msg, error)
```

Use `axon.OpenAI` for an OpenAI-compatible endpoint, or implement `Model` for another protocol.

[Run an agent](/axon/start/quickstart/).
