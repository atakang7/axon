---
title: Turn Loop
description: How Step() drives the agent through model calls and tool execution.
---

## Step

`agent.Step(ctx, "user input")` is the core operation. It submits one user message and drives the loop until the model emits text with no further tool calls.

```
Step(ctx, "user input")
│
├─ session.Turn++
├─ session.Append(user message)
├─ session.Save()
│
└─ loop {
     │
     ├─ Pruner check (see Context Management)
     │
     ├─ model.Complete(Request{
     │     Messages: session.ContextMessages(window),
     │     Tools:    toolSpecs(all tools),
     │     Stream:   { Token, Reasoning, ToolArgs, Usage }
     │   })
     │
     ├─ if no tool calls → return StepResult
     │
     └─ for each tool call:
          ├─ find tool by name
          ├─ call Fn(ctx, args)
          ├─ append result to session
          └─ continue loop
   }
```

## StepResult

```go
type StepResult struct {
    Assistant string      // final assistant text
    ToolCalls []ToolCall   // all tool calls made this turn
    Turn      int          // turn counter
}
```

## Run

`agent.Run(ctx, inputFunc)` calls `Step` for each line the input function returns.

```go
type InputFunc func() (string, bool)
```

Returns `(line, true)` per turn, `("", false)` when exhausted. Interruptions are handled — the loop continues after `ErrInterrupted`.

## Interrupt

```go
ok := agent.Interrupt() // cancel in-flight model call
```

Goroutine-safe. Returns `false` if no turn is active. The interrupted `Step` returns `ErrInterrupted`.

## Runtime controls

```go
agent.SetModel(newModel)                    // swap model mid-session
agent.SetPrunerModel(cheapModel)            // swap or enable pruner
agent.SetPruneMode(axon.PruneExtreme)       // change pruning aggressiveness

agent.Reset()                               // wipe session, kill bg shells, rebuild tools
path, ok := agent.Undo()                    // revert last file edit atomically
dir, err := agent.Cd("/new/path")           // change working directory
```

All of these must not be called while `Step` is running.
