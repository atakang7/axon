---
title: Add a custom tool
description: Define a model-visible contract and a runtime implementation.
---

## 1. Define the contract

```go
lookup := axon.Tool{
    Name:        "lookup_ticket",
    Description: "Fetch one support ticket by numeric ID.",
    Schema: map[string]any{
        "type": "object",
        "additionalProperties": false,
        "properties": map[string]any{
            "id": map[string]any{"type": "integer"},
        },
        "required": []string{"id"},
    },
```

`Name`, non-nil `Schema`, and `Fn` are required by agent construction. A no-argument tool still needs a schema, for example `map[string]any{"type":"object"}`.

## 2. Implement execution and validation

```go
    Fn: func(ctx context.Context, raw json.RawMessage) (string, error) {
        var in struct {
            ID int `json:"id"`
        }
        if err := json.Unmarshal(raw, &in); err != nil {
            return "", err
        }
        if in.ID <= 0 {
            return "", fmt.Errorf("id must be positive")
        }

        ticket, err := service.Lookup(ctx, in.ID)
        if err != nil {
            return "", err
        }
        return ticket.Summary, nil
    },
}
```

Axon does not run a generic JSON Schema validator before `Fn`. The schema is the model contract; the implementation still validates decoded arguments.

## 3. Attach the tool

```go
agent, err := axon.New(axon.Config{
    Model:        model,
    SystemPrompt: "...",
    Tools:        []axon.Tool{lookup},
})
```

## Return recoverable errors

Tool errors become model-visible tool observations. Prefer errors that explain what constraint failed and what the model can change.

For example, “id must be positive” is more useful to the next reasoning step than “bad request.”

## Honor cancellation

The tool receives the active turn context. Pass it through to slow I/O or explicitly select on `ctx.Done()`.

Axon cannot forcibly stop a custom Go function that ignores cancellation.

## Keep authority narrow

Close over the smallest service/interface needed by the capability. This mirrors the built-in tools' design and reduces accidental access to unrelated credentials/state.
