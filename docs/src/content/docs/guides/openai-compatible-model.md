---
title: Use an OpenAI-compatible endpoint
description: Construct Axon's shipped streaming model client directly or from Settings.
---

Axon's built-in client targets OpenAI-style streamed chat completions.

## From `Settings`

Use this when provider/model/request settings live in `axon.yaml`:

```go
settings, err := axon.Load()
if err != nil {
    return err
}

model, err := settings.NewClient("provider-name", "model-id")
if err != nil {
    return err
}
```

This applies `Settings.Model` to the client.

## Direct construction

Use this when your application already owns configuration:

```go
model, err := axon.OpenAI(axon.ClientConfig{
    Provider: axon.Provider{
        Name:    "gateway",
        BaseURL: "https://llm.example.internal/v1",
        Model:   "model-id",
        APIKey:  token,
    },
    MaxTokens:        12000,
    RequestTimeout:   10 * time.Minute,
    IdleTimeout:      15 * time.Second,
    ReasoningEffort:  "high",
    ExcludeReasoning: false,
})
```

Zero token/time values fall back to `DefaultSettings().Model`.

## What the endpoint must understand

The client sends:

- POST `/chat/completions` under the normalized `/v1` base;
- `stream: true`;
- OpenAI-style message objects;
- OpenAI function tools;
- `parallel_tool_calls: true` when tools are present;
- SSE `data:` response lines containing `choices[0].delta`;
- `reasoning_content` when the provider exposes reasoning in that field.

The runtime later executes tool calls sequentially even though the request advertises parallel tool-call support.

## Provider-specific request data

`Provider.Extra`, when non-empty JSON, is inserted under the request's top-level `provider` key.

Use it only for endpoints that understand that field.
