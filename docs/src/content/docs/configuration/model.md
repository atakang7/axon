---
title: Model requests
description: Configure the shipped OpenAI-compatible client's request size, reasoning, and stream timeouts.
---

The `model` section configures **clients built through `Settings.NewClient`**.

It does not configure an arbitrary `Model` object passed to `Agent.New`.

## Schema and defaults

```yaml
model:
  max_tokens: 20000
  request_timeout: 30m
  idle_timeout: 20s
  reasoning_effort: ""
  exclude_reasoning: false
```

| Field | Default | Applied as |
| --- | --- | --- |
| `max_tokens` | `20000` | default `max_tokens` for a request whose `Request.MaxTokens` is zero |
| `request_timeout` | `30m` | `http.Client.Timeout` for the whole streamed response |
| `idle_timeout` | `20s` | maximum silence between scanner chunks before stream cancellation |
| `reasoning_effort` | empty | `reasoning.effort` when non-empty |
| `exclude_reasoning` | `false` | provider reasoning exclusion fields when true |

## `max_tokens`

The primary agent loop currently leaves `Request.MaxTokens` at zero, so the shipped client uses its configured client-level `max_tokens` for normal model requests.

The pruner is different: it explicitly sets `Request.MaxTokens` from `pruner.max_tokens`.

## Whole-request vs idle timeout

These solve different failure modes:

**`request_timeout`** bounds total wall-clock duration of the HTTP request.

**`idle_timeout`** bounds the gap between streamed chunks. Each scanner line resets the idle timer; if the provider goes silent long enough, the client cancels and reports a stalled stream.

The defaults keep idle timeout much shorter than the whole-request timeout. The loader does not currently validate that relationship for custom values, so configure it deliberately.

## Reasoning fields

When `reasoning_effort` is non-empty or `exclude_reasoning` is true, the shipped client adds a `reasoning` object.

When exclusion is true it also sends:

```json
"include_reasoning": false
```

Axon does not validate allowed effort strings; the value is forwarded to the provider.

## Direct `ClientConfig`

`axon.OpenAI(ClientConfig)` / `NewClient(ClientConfig)` also default zero token/time fields from `DefaultSettings().Model`.

If you construct that client directly, `Settings.Model` has no later opportunity to modify it.
