---
title: Configuration Schemas
description: Writing system settings and environment variables.
---

Write configuration logic using two explicit files: `axon.yaml` (structural settings) and `.env` (secrets).

## axon.yaml

Define endpoints, available models, and thresholds. Write this file to `~/.config/axon/axon.yaml`.

```yaml
providers:
  openrouter:
    base_url: https://openrouter.ai/api
    api_key: ${OPENROUTER_API_KEY}
    models:
      deepseek/deepseek-v3.2:
        route: null
      qwen/qwen3.6-flash:
        route: null

model:
  temperature: 0.1
  max_tokens: 4096
```

## .env

Define secrets matching the interpolated `${VAR}` keys in the YAML. Write this file to `~/.config/axon/.env`.

```bash
OPENROUTER_API_KEY=sk-or-v1-abcdef1234567890
```

## Environment Overrides

To explicitly override file paths at runtime without altering code, export the following environment variables prior to running the executable:

- `AXON_CONFIG`: Absolute path to `axon.yaml`.
- `AXON_ENV`: Absolute path to `.env`.
- `AXON_DATA_DIR`: Absolute path for the session database.
