---
title: Model & messages
description: Public model transport contracts and the shipped OpenAI-compatible client.
---

## `Model`

```go
type Model interface {
    Complete(ctx context.Context, req Request) (*Msg, error)
}
```

## `Request`

```go
type Request struct {
    Messages  []Msg
    Tools     []ToolSpec
    MaxTokens int
    Stream    Stream
}
```

`MaxTokens == 0` means the model implementation may apply its own default. Axon's shipped client uses its `ClientConfig.MaxTokens`.

## `Stream`

```go
type Stream struct {
    Token     func(text string)
    Reasoning func(text string)
    ToolArgs  func(name, delta string)
}
```

Callbacks are optional.

## `Msg`

```go
type Msg struct {
    Role        string
    Content     string
    Reasoning   string
    ToolCalls   []ToolCall
    ToolCallID  string
    ToolName    string
    ID          string
    Parked      bool
    ParkSummary string
    ParkReason  string
}
```

Internal session/context metadata (`ID`, parking fields) is stripped when constructing the normal provider-visible context projection.

## `ToolCall`

Contains provider call ID/type and a function name + raw JSON argument string.

The runtime does not guarantee that provider-emitted arguments satisfy the tool schema.

## `ToolSpec`

```go
type ToolSpec struct {
    Name        string
    Description string
    Schema      map[string]any
}
```

This is the model-visible projection of a runtime `Tool`.

## `Provider`

```go
type Provider struct {
    Name    string
    BaseURL string
    Model   string
    APIKey  string
    Extra   json.RawMessage
}
```

## `ClientConfig`

Adds default max tokens, reasoning fields, whole-request timeout, and stream-idle timeout around a `Provider`.

## `Client`

Axon's shipped `Model` implementation. Safe-for-concurrent-use according to its public contract.

It sends streamed OpenAI-compatible chat completions, assembles fragmented tool calls by provider index, orders final tool calls by index, and substitutes `{}` for a tool call whose streamed argument string is empty.

## `APIError`

```go
type APIError struct {
    Status int
    Body   string
}
```

Returned for non-2xx responses. Response body is capped at 4096 bytes before storage in the error.
