---
title: Configuration
description: Managing settings and credentials in Axon.
---

Axon requires two configuration files to separate settings from secrets. 

| File | Contains | Version Control |
| --- | --- | --- |
| `~/.config/axon/axon.yaml` | System settings | **Yes** |
| `~/.config/axon/.env` | Credentials | **No** |

You can override these locations by setting `AXON_CONFIG` and `AXON_ENV`.

## Setup

```bash
mkdir -p ~/.config/axon
curl -o ~/.config/axon/axon.yaml https://raw.githubusercontent.com/atakang7/axon/main/axon.example.yaml

printf 'OPENROUTER_API_KEY=sk-or-...\n' > ~/.config/axon/.env
chmod 600 ~/.config/axon/.env
```

## Structure

```yaml
providers:
  openrouter:
    base_url: https://openrouter.ai/api
    api_key: ${OPENROUTER_API_KEY}
    models:
      deepseek/deepseek-v3.2:
```

Variables defined as `${VAR}` resolve strictly from the `.env` file, falling back to the process environment. An unresolved variable errors at load, preventing downstream API authentication failures.
