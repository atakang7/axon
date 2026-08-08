---
title: Context & pruner
description: Configure recency windowing and the optional secondary context-curation model.
---

The `pruner` section configures **two related but distinct mechanisms**:

1. the recency window used for every primary-model context projection;
2. the optional `Pruner` object, which exists only when a pruner `Model` is supplied.

## Defaults

```yaml
pruner:
  mode: moderate
  window_blocks: 30
  floor_tokens: 10000
  growth_tokens: 5000
  max_tokens: 4096
  timeout: 60s
```

| Field | Default | Consumer |
| --- | ---: | --- |
| `mode` | `moderate` | curator prompt / mutable prune mode |
| `window_blocks` | `30` | **primary context projection and pruner** |
| `floor_tokens` | `10000` | pruner fire threshold |
| `growth_tokens` | `5000` | minimum projected growth between curator attempts |
| `max_tokens` | `4096` | `Request.MaxTokens` for the curator call |
| `timeout` | `60s` | context timeout around one curator call |

## `window_blocks` works without a pruner model

This is the most important configuration fact on this page.

Primary model requests always call session context projection with `settings.Pruner.WindowBlocks`. Old active blocks outside the window are collapsed into transient breadcrumbs even when `Config.Pruner == nil`.

So disabling the secondary model does **not** disable context windowing.

## You cannot currently configure zero blocks

The lower-level projection treats `windowBlocks <= 0` as “disable windowing.” However, `Settings.WithDefaults()` replaces non-positive `pruner.window_blocks` with the default `30` during normal agent construction.

Therefore `window_blocks: 0` does not disable windowing through `axon.yaml` or a normal zero-valued `Settings`; it becomes 30.

## Curator thresholds

When a `Pruner` exists, it estimates current projected tokens approximately as character count divided by four.

It fires when:

```text
projected tokens >= floor_tokens
AND
(first fire OR projected tokens - last_fire >= growth_tokens)
```

A failed curator attempt still updates its last-fire baseline so a broken/slow curator is not retried on every loop iteration.

## Modes

Valid values:

```text
low
moderate
extreme
```

They change the natural-language threshold given to the curator. They do not change recency-window mechanics or the hard protected-ID checks.

An invalid non-empty mode loaded from YAML fails configuration validation.

## `max_tokens`

Unlike normal primary calls—which usually rely on the shipped client's client-level max token setting—the pruner explicitly supplies this value in its `Request`.

## `timeout`

One curator call is wrapped in a `context.WithTimeout` using this value. A curator timeout is reported to the primary turn as a pruning error and the turn continues.

## Enable the secondary pruner

Settings alone do not create a curator model:

```go
agent, err := axon.New(axon.Config{
    Model:        primary,
    Pruner:       cheapLongContextModel,
    SystemPrompt: "...",
    Settings:     settings,
})
```

You can also install/change one later with `SetPrunerModel`.
