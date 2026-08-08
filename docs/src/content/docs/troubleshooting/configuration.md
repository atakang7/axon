---
title: Configuration troubleshooting
description: Diagnose missing files, unresolved variables, strict YAML errors, and provider/model resolution failures.
---

## `configuration file not found`

`Load()` searched the standard config path and did not find a file.

Check:

```text
$AXON_CONFIG
$XDG_CONFIG_HOME/axon/axon.yaml
~/.config/axon/axon.yaml
```

or use `LoadFrom` with explicit paths.

## `credentials file not found`

The loader currently requires the credentials file itself to exist before parsing YAML.

Create the file—even if your referenced secrets come from process environment variables—or bypass `Load` and construct `Settings` directly.

## Unknown YAML field

Axon decodes with `KnownFields(true)`. A typo such as:

```yaml
timeuot: 30s
```

is an error, not an ignored extension field.

Compare against the [complete configuration](/axon/configuration/complete-example/).

## Variable “is not set”

A `$VAR` / `${VAR}` used in provider `api_key` or `base_url` was absent from both:

1. parsed credentials entries;
2. non-empty process environment variables.

The error names the missing variable.

## `no providers`

The file loader requires at least one provider even if your application architecture could otherwise use a custom model.

When you need Settings only for retry/tool/session policy with a custom `Model`, construct `Settings` directly rather than using the current loader contract.

## Unknown provider/model

`Settings.Provider` reports configured alternatives. Provider/model selection is explicit; no first/default model is selected automatically.

## A zero setting did not disable something

Most filled numeric/duration/byte settings treat non-positive values as “unset” and replace them with defaults.

Examples include `pruner.window_blocks`, which becomes 30 rather than disabling windowing.

## `AXON_READ_MAX_BYTES` did nothing

Current configuration does not read that variable. The read full-file refusal message still mentions this older variable name, but the active setting is:

```yaml
tools:
  read:
    max_bytes: 4MiB
```
