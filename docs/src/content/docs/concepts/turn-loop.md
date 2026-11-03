---
title: Turn Loop
description: One user turn, including model and tool calls.
---

`agent.Step(ctx, input)` runs model requests until the agent finishes or returns an error.

```text
+-------------+  Step()  +-------+  Complete()  +-------+
| application | -------> | Agent | ----------> | Model |
+-------------+          +-------+             +-------+
```

The `Agent` executes returned `Tool.Fn` calls, saves results to `Session` and repeats.

| Method | Result |
|--------|--------|
| `Step(ctx, text)` | `StepResult` with assistant text, tool calls and turn number |
| `Run(ctx, input)` | Repeated `Step` calls until input ends |
| `Interrupt()` | Cancels the active turn; safe from another goroutine |
| `Reset()` | Resets the session and background processes |
| `Undo()` | Restores the last tracked file edit |

## Limits

- `Step`, `Run` and `Reset` must not run concurrently on one agent.
- `MaxIterations` defaults to no limit; set it for batch use.
