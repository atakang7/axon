---
title: Custom Tools
description: How to add your own tools to an agent.
---

## Defining a tool

```go
tool := axon.Tool{
    Name:        "deploy",
    Description: "Deploy the current build to staging or production.",
    Schema: map[string]any{
        "type": "object",
        "properties": map[string]any{
            "env": map[string]any{
                "type": "string",
                "enum": []any{"staging", "prod"},
            },
        },
        "required":             []any{"env"},
        "additionalProperties": false,
    },
    Fn: func(ctx context.Context, args json.RawMessage) (string, error) {
        var p struct{ Env string `json:"env"` }
        json.Unmarshal(args, &p)
        // deploy logic here
        return "deployed to " + p.Env, nil
    },
}
```

## Required fields

| Field | Required | Notes |
|-------|----------|-------|
| `Name` | ✓ | Must not collide with a built-in the agent still has |
| `Schema` | ✓ | Always required — even for no-arg tools use `map[string]any{"type": "object"}` |
| `Fn` | ✓ | `func(ctx context.Context, args json.RawMessage) (string, error)` |
| `Description` | — | Optional, but a tool the model can't distinguish from others won't get called |

**Why Schema is always required:** Providers disagree about what a null parameter schema means. The disagreement surfaces as a rejected request, not a clear error.

## Registration

Pass custom tools via `Config.Tools`:

```go
agent, err := axon.New(axon.Config{
    Model:        model,
    SystemPrompt: "...",
    Tools:        []axon.Tool{deployTool, rollbackTool},
})
```

Custom tools are appended after built-ins. Duplicate names are rejected at construction.

## Tool function contract

```go
func(ctx context.Context, args json.RawMessage) (string, error)
```

- **ctx** is turn-scoped — cancelled when the turn is interrupted via `agent.Interrupt()`. Long-running work should honour it.
- **args** is raw JSON as the model emitted it. Not guaranteed to match the schema.
- Return a string result or an error. Errors are fed back to the model as the tool result.

## What tools cannot access

Tools receive only what they need. They cannot:
- Read the conversation history or session
- Access provider credentials
- Modify agent configuration
- Reach other tools

This is enforced structurally — the function signature carries no agent reference.

## No-argument tool

```go
axon.Tool{
    Name:        "timestamp",
    Description: "Returns the current UTC timestamp.",
    Schema:      map[string]any{"type": "object"},
    Fn: func(ctx context.Context, _ json.RawMessage) (string, error) {
        return time.Now().UTC().Format(time.RFC3339), nil
    },
}
```
