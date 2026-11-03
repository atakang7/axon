---
title: Custom Tools
description: Register a Tool with a name, schema and function.
---

Pass a `Tool` through `Config.Tools` to make it available alongside built-in tools.

```go
echo := axon.Tool{
    Name:        "echo",
    Description: "Return a text value.",
    Schema: map[string]any{
        "type": "object",
        "properties": map[string]any{
            "text": map[string]any{"type": "string"},
        },
        "required": []string{"text"},
    },
    Fn: func(ctx context.Context, args json.RawMessage) (string, error) {
        var input struct { Text string `json:"text"` }
        if err := json.Unmarshal(args, &input); err != nil {
            return "", err
        }
        return input.Text, nil
    },
}
```

Register it using `axon.New(axon.Config{Model: model, SystemPrompt: "...", Tools: []axon.Tool{echo}})`.

## Limits

- Tool names must be unique across custom and built-in tools.
- Validate JSON arguments inside `Fn`; a schema does not guarantee model output.
- Respect `ctx` cancellation in long-running tools.
