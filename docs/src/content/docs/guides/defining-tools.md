---
title: Custom tools
description: Define JSON-schema tools and attach them to an Agent.
---

A custom tool has four fields. Three are required by `New`: `Name`, `Schema`, and `Fn`. `Description` is optional at validation time but strongly affects whether a model knows when to call the tool.

```go
tool := axon.Tool{
    Name:        "project_name",
    Description: "Return the current project's display name.",
    Schema: map[string]any{
        "type":                 "object",
        "properties":           map[string]any{},
        "additionalProperties": false,
    },
    Fn: func(ctx context.Context, raw json.RawMessage) (string, error) {
        return "axon", nil
    },
}
```

Attach it at construction:

```go
agent, err := axon.New(axon.Config{
    Model:        model,
    SystemPrompt: "...",
    Tools:        []axon.Tool{tool},
})
```

## Schema is always required

Even a no-argument tool must supply a non-nil JSON Schema. Axon rejects a nil schema before the first model call.

The runtime does not validate model-emitted arguments against that schema before calling `Fn`. Your function must decode and validate `json.RawMessage` itself.

## Tool names must be unique

By default these names are already occupied:

```text
read, write, exec, bash_output, kill_shell, search, task
```

To replace a built-in intentionally, exclude it first:

```go
axon.Config{
    ExcludeBuiltins: []string{"search"},
    Tools:           []axon.Tool{mySearch},
}
```

MCP-discovered tools and caller-supplied tools share the same duplicate-name validation.

## Context cancellation

The `ctx` passed to `Fn` is the active turn context and is canceled by `Agent.Interrupt()` or parent-context cancellation.

Long-running custom tools should select on `ctx.Done()` or pass the context to their own I/O. Axon cannot force a tool implementation that ignores cancellation to stop.

## Error contract

Return a normal Go error when the tool operation fails:

```go
return "", fmt.Errorf("lookup failed: %w", err)
```

Axon emits `tool_error`, converts the error text to a `role=tool` message, and lets the model reason about the failure on the next loop iteration.

## Do not expose more capability than necessary

Built-in tools use narrow `Workspace` and `Plan` interfaces for a reason. Apply the same pattern to custom tools: close over the smallest service/interface the function needs rather than the entire application container.
