---
title: Architecture
description: Why the Go runtime is a single package.
---

Axon is a Go library with a flat `package axon`. The embedding application chooses the model, prompt, tools and user interface. `Agent.Step` owns the turn loop.

```text
+-------------+  Step  +-------+  Complete  +---------+
| application | -----> | Agent | ---------> | Model   |
+-------------+        +-------+            +---------+
                         |  |
                  calls  |  | writes
                         v  v
                      +------+  +---------+
                      | tools|  | Session |
                      +------+  +---------+
```

The model receives tool specifications without function implementations. After a tool call, the agent executes the matching function and appends its result to the session before asking the model again.

The flat package keeps public types and runtime behavior together for embedders. The documentation site is a separate Astro project; Go library users do not need it.
