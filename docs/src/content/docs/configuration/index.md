---
title: Configuration model
description: The two configuration planes in Axon, when each is consumed, and how values are resolved.
---

Axon has **two configuration planes**. Treating them as one is the fastest way to misunderstand the runtime.

## 1. Agent wiring: `axon.Config`

`axon.Config` is a constructor input. It answers:

> **Which concrete objects and capabilities make up this agent instance?**

It contains things such as:

- the primary `Model`;
- the system prompt;
- custom tools;
- excluded built-ins;
- an optional pruner `Model`;
- an optional pre-built `Session`;
- an optional working directory;
- MCP servers;
- the event callback;
- one resolved `Settings` value.

`Model` and `SystemPrompt` are required. The rest changes composition/lifecycle.

## 2. Operational policy: `axon.Settings`

`Settings` answers:

> **How should the runtime and shipped model client behave?**

It contains:

- provider/model definitions;
- shipped-client request settings;
- retry policy;
- built-in tool defaults/limits;
- context/pruner policy;
- session/state locations.

`axon.yaml` is simply one way to construct `Settings`. It is not global runtime state.

## Nothing is loaded implicitly

`axon.New` does not call `axon.Load`.

A file-configured application is explicitly assembled like this:

```text
axon.yaml + .env
      │
      ▼
   Load()
      │
      ▼
   Settings ──▶ Settings.NewClient(provider, model) ──▶ Model
      │                                                │
      └──────────────────────┐                         │
                             ▼                         ▼
                         axon.Config ───────────────▶ New()
```

This is intentional. An application can skip the files entirely, use a custom `Model`, construct different `Settings` for multiple agents, or choose providers dynamically.

## Which section is consumed where?

This table is the most useful configuration reference in the project:

| Settings section | Consumer | When it matters |
| --- | --- | --- |
| `providers` | `Settings.Provider` / `Settings.NewClient` | when the application constructs Axon's shipped client |
| `model` | `Settings.NewClient` | copied into the shipped client's request/token/reasoning/timeout settings |
| `retry` | `Agent` | primary-model retry loop during turns |
| `tools` | `New` → `Limits` → built-ins | when built-in tools are bound to the agent |
| `pruner.window_blocks` | primary context projection | every primary-model request, even without `Config.Pruner` |
| other `pruner` fields | `Pruner` | only when a pruner model exists (or is later installed) |
| `session` | `New` / session-path helpers | when Axon chooses session and background-log locations |

### Consequence: `model.*` does not mutate arbitrary models

If you construct a `Model` yourself and pass it to `axon.New`, the agent does not reach inside it and apply `Settings.Model`.

These two are **not equivalent**:

```go
// Settings.Model is applied here.
model, _ := settings.NewClient("openrouter", "model-id")
```

```go
// This model has whatever ClientConfig you explicitly gave it.
model, _ := axon.OpenAI(customClientConfig)
agent, _ := axon.New(axon.Config{Model: model, Settings: settings, SystemPrompt: "..."})
```

The same principle applies to providers: `providers:` is construction data, not a provider router inside `Agent`.

## Sources of Settings

You have two supported patterns.

### Direct Go values

```go
settings := axon.DefaultSettings()
settings.Tools.Exec.Timeout = axon.Duration(20 * time.Second)
```

Or populate only the fields you care about; `New` calls `WithDefaults()`.

### `axon.yaml` + credentials file

```go
settings, err := axon.Load()
```

The loader strictly decodes YAML, resolves provider secret references, applies defaults, and validates provider/pruner structure.

## Defaults are fill semantics

`WithDefaults()` treats empty strings and **non-positive numeric/duration/byte fields** as unset for fields it fills.

That has a real consequence: many settings cannot express a literal zero. For example, `window_blocks: 0` is replaced by the default `30`; you cannot disable recency windowing through ordinary `Settings` construction even though the lower-level projection function understands zero as “no window.”

## File-loader contract is stricter than direct Settings

`Load` currently requires:

- a credentials file that exists;
- at least one provider;
- a `base_url` for every provider;
- at least one model under every provider;
- a valid pruner mode when specified.

A direct `Settings` value can be used without provider definitions when your application supplies its own model.

## Continue by section

- [Providers](/axon/configuration/providers/)
- [Model requests](/axon/configuration/model/)
- [Retry policy](/axon/configuration/retry/)
- [Tool limits](/axon/configuration/tools/)
- [Context & pruner](/axon/configuration/pruner/)
- [Sessions & paths](/axon/configuration/session/)
- [Environment variables](/axon/configuration/environment/)
- [Durations & byte sizes](/axon/configuration/scalars/)
- [Complete `axon.yaml`](/axon/configuration/complete-example/)
