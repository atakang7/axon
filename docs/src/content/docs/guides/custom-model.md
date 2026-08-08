---
title: Implement a Model
description: Adapt a non-OpenAI protocol, internal gateway, or deterministic fake to Axon.
---

Implement one method:

```go
type Model interface {
    Complete(ctx context.Context, req axon.Request) (*axon.Msg, error)
}
```

## Minimal implementation

```go
type MyModel struct {
    client *VendorClient
}

func (m *MyModel) Complete(ctx context.Context, req axon.Request) (*axon.Msg, error) {
    // 1. Translate req.Messages.
    // 2. Translate req.Tools (contracts only).
    // 3. Call your provider with ctx.
    // 4. Emit optional stream callbacks while data arrives.
    // 5. Translate the provider reply into axon.Msg.
    return &axon.Msg{
        Role:    "assistant",
        Content: "done",
    }, nil
}
```

## Translate tool calls, do not execute them

When your provider asks to invoke a tool, return it in `Msg.ToolCalls`.

Axon owns execution. Your `Model` should not call `Tool.Fn` itself; it does not receive those functions in `Request` anyway.

## Preserve raw JSON arguments

`ToolCall.Function.Arguments` is a JSON string. Axon passes it to the selected tool as `json.RawMessage` without schema validation.

Return syntactically meaningful provider output and let the tool validate its domain arguments.

## Support streaming when available

Use the optional callbacks:

```go
req.Stream.Token("partial text")
req.Stream.Reasoning("partial reasoning")
req.Stream.ToolArgs("tool-name", "partial-json")
```

Check for nil before calling them.

## Honor context cancellation

`ctx` is canceled when the parent context fails or `Agent.Interrupt()` cancels the active turn.

Pass it into network/database calls so interruption actually stops your model request.

## Errors and retry behavior

Axon's agent retry loop understands `*axon.APIError` status classification plus several standard transport/network errors.

A custom model can return its own errors, but unknown error types are not automatically considered retryable. If you want status-based retry behavior compatible with Axon's policy, return `*axon.APIError` for HTTP-style failures.

## Testing benefit

Because `Model` is a one-method interface, deterministic scripted models can drive the entire agent loop with no network or API key. This is the preferred way to test tool sequences and interruption behavior around an embedded agent.
