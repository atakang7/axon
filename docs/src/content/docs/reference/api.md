---
title: Go API
description: Exported entry points and concurrency rules.
---

The module path is `github.com/atakang7/axon/v2`; its Go package name is `axon`.

| Symbol | Contract |
| --- | --- |
| `OpenAI(ClientConfig)` | Returns a streaming `Model` for an OpenAI-compatible endpoint |
| `Model.Complete(context.Context, Request)` | Produces a `*Msg` or an error |
| `New(Config)` | Constructs an `*Agent` |
| `Agent.Step(ctx, input)` | Runs a turn and returns `StepResult` |
| `Agent.Run(ctx, InputFunc)` | Calls `Step` until input ends |
| `Agent.Interrupt()` | Cancels the active call; safe across goroutines |
| `Agent.Reset()` | Resets the session and tools |
| `Agent.Undo()` | Restores the most recent recorded edit |
| `Agent.Close()` | Releases background processes and MCP clients |

`Config.Model` and `Config.SystemPrompt` are required. `Config.MaxIterations=0` leaves the turn unbounded.

`Step`, `Run` and `Reset` must not run concurrently. Call `Close` when finished.
