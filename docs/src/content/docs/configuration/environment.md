---
title: Environment variables
description: Path overrides, XDG integration, and provider secret expansion.
---

Axon intentionally uses environment variables for **locations and secret substitution**, not for the general operational settings tree.

## Path variables

| Variable | Purpose | Precedence role |
| --- | --- | --- |
| `AXON_CONFIG` | exact `axon.yaml` path | overrides default config location |
| `AXON_ENV` | exact credentials-file path | overrides default credentials location |
| `AXON_DATA_DIR` | state root | overrides `session.data_dir` |
| `AXON_SESSION_PATH` | exact session JSON path | overrides `session.path` and derived path |
| `XDG_CONFIG_HOME` | base for default config/credentials location | fallback before `~/.config` |
| `XDG_DATA_HOME` | base for default state root | fallback before `~/.local/share` |

Values read through Axon's `String` helper are trimmed.

## Default config locations

```text
ConfigPath():
  $AXON_CONFIG
  else $XDG_CONFIG_HOME/axon/axon.yaml
  else ~/.config/axon/axon.yaml
```

```text
EnvPath():
  $AXON_ENV
  else $XDG_CONFIG_HOME/axon/.env
  else ~/.config/axon/.env
```

The credentials file follows the config home, not the project working directory.

## Provider variable substitution

Inside `providers`, `api_key` and `base_url` support `$VAR` and `${VAR}` syntax.

For each reference Axon checks:

1. entries parsed from the credentials file;
2. the process environment when the value exists and is non-empty;
3. otherwise configuration loading fails and names the missing variable.

Example:

```yaml
providers:
  openrouter:
    base_url: ${OPENROUTER_BASE_URL}
    api_key: ${OPENROUTER_API_KEY}
```

## Credentials-file syntax

Supported:

```dotenv
# comment
KEY=value
export OTHER=value
QUOTED="value with spaces"
SINGLE='value with spaces'
SPACED = value
```

Blank lines and comments are ignored. Lines without `=` are skipped.

The file is **not a shell**: no command substitution and no interpolation between entries is performed.

## The credentials file is currently required by `Load`

`LoadFrom` reads the credentials file before decoding YAML. A missing file returns `ErrMissingEnv` even when all API keys are literal or supplied by the process environment.

If you do not want that file contract, construct `Settings` directly instead of calling `Load`.

## Removed-style AXON settings are not active configuration

Current operational limits (read bytes, search limits, exec timeouts, etc.) are taken from `Settings`, not a family of `AXON_*` limit variables.

Some runtime error/help strings may still mention an older variable name; follow the configuration manual and `Settings` schema rather than those stale hints.
