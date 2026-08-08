---
title: Configuration & state locations
description: Load axon.yaml, resolve credentials, and control where state is written.
---

Axon can be configured entirely in Go, or you can opt into its file loader.

## Standard locations

`Load()` is equivalent to:

```go
axon.LoadFrom(axon.ConfigPath(), axon.EnvPath())
```

The default settings path is:

```text
$AXON_CONFIG
or $XDG_CONFIG_HOME/axon/axon.yaml
or ~/.config/axon/axon.yaml
```

The credentials path is:

```text
$AXON_ENV
or $XDG_CONFIG_HOME/axon/.env
or ~/.config/axon/.env
```

`EnvPath` is deliberately tied to the config home, not the current project directory.

## Loader pipeline

`LoadFrom(configPath, envPath)` performs these steps in order:

1. parse the credentials file;
2. read YAML;
3. decode YAML with `KnownFields(true)` so unknown keys fail;
4. resolve `$VAR` / `${VAR}` references in provider `api_key` and `base_url`;
5. apply `WithDefaults()`;
6. validate providers, retry attempts, and prune mode.

The returned `Settings` has no pending substitution or defaulting work.

## Secret lookup order

For each variable reference, Axon first checks the parsed credentials file, then falls back to the process environment when the environment variable exists and is non-empty.

An unresolved variable is a configuration error; it is not replaced with an empty string.

:::caution
The credentials file itself must currently exist for `Load`/`LoadFrom` because `readEnvFile` runs before configuration decoding. A process environment variable can satisfy a referenced secret, but it does not remove that file-existence requirement.
:::

## `.env` syntax

The parser supports:

- `KEY=VALUE`;
- blank lines;
- `#` comments;
- optional `export ` prefix;
- one matching pair of single or double quotes around a value.

It is intentionally **not a shell**: it does not execute commands or interpolate variables between entries.

## State directory

When no `SessionConfig.DataDir` is supplied:

```text
$AXON_DATA_DIR
or $XDG_DATA_HOME/agent
or ~/.local/share/agent
```

Notice that the default leaf directory is currently `agent`, not `axon`.

The same data root contains default session files and background-shell log directories.

## Pin one session explicitly

Session path precedence is:

```text
$AXON_SESSION_PATH
then settings.session.path
then derived per-current-working-directory path
```

These path environment variables are the remaining ambient overrides; operational values such as timeouts and output sizes come from `Settings` instead.

## Direct configuration

You can bypass file loading completely:

```go
settings := axon.DefaultSettings()
settings.Tools.Exec.Timeout = axon.Duration(15 * time.Second)

agent, err := axon.New(axon.Config{
    Model:        model,
    SystemPrompt: "...",
    Settings:     settings,
})
```

`New` calls `WithDefaults` again, so a partially populated `Settings` is valid.
