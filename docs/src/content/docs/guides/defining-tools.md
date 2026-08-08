---
title: Defining Tools
description: Contracts and concurrency for custom capabilities.
---

Tools are the capability boundary between the Axon runtime and your infrastructure. 

## The Tool Contract

A valid `axon.Tool` must define a JSON Schema outlining its required arguments, and a Go closure `Fn` to execute the logic.

Because Axon executes tools in parallel if the LLM requests it, **your tool closures must be thread-safe.** Furthermore, they must strictly respect `ctx.Done()` to prevent leaked goroutines during a turn cancellation.

### Example: Database Query Tool

```go
var ReadDBTool = axon.Tool{
	Name:        "query_database",
	Description: "Execute a read-only SQL query to retrieve application state.",
	Schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"sql": map[string]any{
				"type": "string",
				"description": "A PostgreSQL SELECT statement.",
			},
		},
		"required": []string{"sql"},
	},
	Fn: func(ctx context.Context, args json.RawMessage) (string, error) {
		var input struct {
			SQL string `json:"sql"`
		}
		if err := json.Unmarshal(args, &input); err != nil {
			// Returning an error here passes the error back to the LLM,
			// allowing it to self-correct its JSON payload in the next frame.
			return "", fmt.Errorf("invalid json payload: %w", err)
		}

		// The provided context respects the timeout/cancellation of ag.Step()
		rows, err := db.QueryContext(ctx, input.SQL)
		if err != nil {
			return "", err 
		}
		
		return formatRows(rows), nil
	},
}
```

## Security & Context Isolation

Tools in Axon are capability-based. A tool's `Fn` receives only what it needs: the request context and the parsed arguments. 

Tools **do not** receive a reference to the `Agent`, the `Session`, or global configuration. This isolation guarantees that a compromised or hallucinated tool dispatch cannot mutate the agent's internal memory projection or extract API keys.

## Tool Registration

To attach custom tools, pass them in the `axon.Config` array during initialization.

```go
ag, err := axon.New(axon.Config{
	Model:        model, 
	SystemPrompt: "...", 
	Tools:        []axon.Tool{ReadDBTool},
})
```
