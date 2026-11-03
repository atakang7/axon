---
title: Add a tool
description: Register an application tool with an agent.
---

1. Create an `axon.Tool` with a name, JSON Schema and function:

   ```go
   echo := axon.Tool{
       Name: "echo",
       Description: "Return the fixed word pong.",
       Schema: map[string]any{"type": "object"},
       Fn: func(_ context.Context, _ json.RawMessage) (string, error) {
           return "pong", nil
       },
   }
   ```

2. Pass the tool to `axon.New` through `Config.Tools`:

   ```go
   cfg.Tools = append(cfg.Tools, echo)
   agent, err := axon.New(cfg)
   ```

`axon.New` rejects duplicate names, nil schemas and nil functions. `Fn` receives the current turn's context.
