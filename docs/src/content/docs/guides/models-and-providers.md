---
title: Models & providers
description: Use the shipped OpenAI-compatible client or implement Model yourself.
---

The runtime depends on `Model`, not on a specific vendor SDK.

## Use a provider from Settings

A loaded configuration can resolve a provider/model pair and build the shipped client:

```go
model, err := settings.NewClient("openrouter", "<model-id>")
```

`Settings.Provider(endpoint, model)` validates both names and returns the lower-level `Provider` value if you need it directly.

Provider and model names are not auto-selected. The embedder chooses them.

## Shipped client protocol

`axon.Client` sends streamed requests to:

```text
<base-url>/v1/chat/completions
```

If `BaseURL` already ends in `/v1`, Axon does not append another one.

Requests use:

- `Content-Type: application/json`;
- `Accept: text/event-stream`;
- bearer authentication when `APIKey` is non-empty;
- `stream: true`;
- `max_tokens`;
- OpenAI function-tool objects;
- `parallel_tool_calls: true` when tools exist;
- optional `reasoning` fields;
- optional provider-specific JSON from `Provider.Extra` under the request's `provider` key.

## Streaming behavior

The client assembles text, `reasoning_content`, and fragmented tool-call arguments from SSE deltas.

Callbacks are invoked as chunks arrive:

```go
type Stream struct {
    Token     func(text string)
    Reasoning func(text string)
    ToolArgs  func(name, delta string)
}
```

Malformed/unparseable SSE lines are ignored rather than aborting an otherwise valid response.

A scanner line is capped at 1 MiB. The entire request has `RequestTimeout`, and silence between chunks is bounded by `IdleTimeout`.

## HTTP errors

Non-2xx responses become `*APIError` with:

- numeric `Status`;
- a response `Body` capped at 4 KiB.

The numeric status is what the agent retry policy evaluates.

## Custom provider protocol

Implement the interface directly when an endpoint does not speak OpenAI-compatible chat completions:

```go
type MyModel struct{}

func (m *MyModel) Complete(ctx context.Context, req axon.Request) (*axon.Msg, error) {
    // Translate req.Messages and req.Tools to your provider.
    // Call req.Stream callbacks as output arrives if you support streaming.
    // Return an assistant Msg, including ToolCalls when requested.
    return &axon.Msg{Role: "assistant", Content: "..."}, nil
}
```

`Request.Tools` contains `ToolSpec` values only. A model implementation cannot invoke Axon tool functions directly through that request.

## OpenRouter routing hint

`ModelOptions.Route` is converted to provider JSON equivalent to:

```json
{"order":["<route>"],"allow_fallbacks":true}
```

and forwarded through `Provider.Extra`. Axon does not interpret that payload after building it.
