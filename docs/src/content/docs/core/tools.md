---
title: Writing Custom Tools
description: Define capability boundaries and logic closures.
---

Write tools by satisfying the `axon.Tool` interface. 

Tools execute concurrently during a `Step` if the model dispatches them in parallel. Ensure your closures are thread-safe.

## Tool Definition

Define a strict JSON schema and map the inputs to a Go struct inside the execution closure.

```go
var QueryDatabaseTool = axon.Tool{
    Name:        "query_db",
    Description: "Execute a read-only SQL query against the primary database.",
    Schema: map[string]any{
        "type": "object",
        "properties": map[string]any{
            "sql": map[string]any{
                "type": "string",
                "description": "A valid PostgreSQL SELECT statement.",
            },
        },
        "required": []string{"sql"},
    },
    // The ctx is derived from the parent Step() call.
    Fn: func(ctx context.Context, args json.RawMessage) (string, error) { 
        var input struct {
            SQL string `json:"sql"`
        }
        if err := json.Unmarshal(args, &input); err != nil {
            return "", fmt.Errorf("invalid arguments: %w", err)
        }
        
        // Ensure you respect ctx.Done() in long-running I/O operations.
        results, err := executeQuery(ctx, input.SQL)
        if err != nil {
            // Return errors to allow the model to auto-correct.
            return "", err
        }
        
        return results, nil
    },
}
```

## Registration

Inject the tool array into `axon.Config` during initialization.

```go
ag, err := axon.New(axon.Config{
    Model:        model, 
    SystemPrompt: "...", 
    Tools:        []axon.Tool{QueryDatabaseTool},
})
```

To strip all default built-in tools (read, write, exec, search, task) and strictly allow only your provided tools, set `ExcludeBuiltins: true` in `axon.Config`.
