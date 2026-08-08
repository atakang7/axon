---
title: Providers
description: Configure OpenAI-compatible endpoints and the models available through them.
---

The `providers` section is an **address book for constructing Axon's shipped OpenAI-compatible client**.

It does not choose a provider automatically and it is not consulted by `Agent.Step`.

## Schema

```yaml
providers:
  <provider-name>:
    base_url: <string>
    api_key: <string>
    models:
      <model-id>:
        route: <string>
```

## Provider fields

| Field | Required by `Load` | Meaning |
| --- | --- | --- |
| `base_url` | yes | API root used to build the chat-completions endpoint |
| `api_key` | no | bearer token; may be literal or a variable reference |
| `models` | yes, at least one | model identifiers the application may resolve for this endpoint |

Provider names are application-facing labels. Model IDs are the exact strings sent as the request's `model` field.

## URL normalization

The shipped client trims trailing `/` from `base_url`.

If the result does not end in `/v1`, Axon appends `/v1`, then calls:

```text
<normalized-base>/chat/completions
```

So both of these normalize correctly:

```yaml
base_url: https://example.com
# request → https://example.com/v1/chat/completions
```

```yaml
base_url: https://example.com/v1
# request → https://example.com/v1/chat/completions
```

For a provider whose API root contains another path segment, include that segment:

```yaml
base_url: https://openrouter.ai/api
# request → https://openrouter.ai/api/v1/chat/completions
```

## Secret references

`api_key` and `base_url` are the only provider fields currently expanded for `$VAR` / `${VAR}` references.

```yaml
api_key: ${OPENROUTER_API_KEY}
```

Resolution order for a referenced variable is:

1. Axon's credentials file;
2. the process environment, when the variable exists and is non-empty;
3. otherwise load fails.

A literal value containing no `$` is left untouched.

See [Environment variables](/axon/configuration/environment/).

## Models

A model entry may be empty:

```yaml
models:
  vendor/model-a: {}
  vendor/model-b: {}
```

The application resolves one explicitly:

```go
model, err := settings.NewClient("openrouter", "vendor/model-a")
```

Unknown provider/model errors list the configured alternatives. `ProviderNames()` and `ModelNames()` return stable sorted names for UI/model-picker use.

## `route`

`route` is the only current per-model option:

```yaml
models:
  vendor/model-a:
    route: Provider Route Name
```

When non-empty, Axon builds provider-specific JSON equivalent to:

```json
{"order":["Provider Route Name"],"allow_fallbacks":true}
```

and sends it as the top-level `provider` field in the shipped client's request.

This shape is an OpenRouter-style routing hint. Axon does not otherwise interpret the payload.

## No implicit default model

Listing one model does not make it the default. `Settings.Provider` requires a non-empty model name, and `Settings.NewClient` requires the application to select it explicitly.
