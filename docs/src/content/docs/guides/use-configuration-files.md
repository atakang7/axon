---
title: Use axon.yaml
description: Load operational settings and construct a provider-backed model explicitly.
---

Use file configuration when you want provider definitions and operational policy outside the binary.

## 1. Create the config directory

By default:

```bash
mkdir -p ~/.config/axon
```

## 2. Create `axon.yaml`

```yaml
providers:
  openrouter:
    base_url: https://openrouter.ai/api
    api_key: ${OPENROUTER_API_KEY}
    models:
      <model-id>: {}

retry:
  max_attempts: 5

tools:
  exec:
    max_timeout: 5m
```

Unknown YAML fields are rejected, so a misspelled setting does not silently disappear.

## 3. Create the credentials file

```bash
cat > ~/.config/axon/.env <<'EOF'
OPENROUTER_API_KEY=...
EOF
chmod 600 ~/.config/axon/.env
```

The current loader requires this file to exist even when secrets could be resolved from process environment variables.

## 4. Load Settings

```go
settings, err := axon.Load()
if err != nil {
    log.Fatal(err)
}
```

At this point variable references have been resolved, defaults filled, and loader validation completed.

## 5. Choose a provider/model

```go
model, err := settings.NewClient("openrouter", "<model-id>")
if err != nil {
    log.Fatal(err)
}
```

Do not skip this conceptual step: `Load` does not choose or construct the primary model automatically.

## 6. Give both objects to the agent

```go
agent, err := axon.New(axon.Config{
    Model:        model,
    SystemPrompt: "You are a coding agent.",
    Settings:     settings,
})
```

`Settings` configures agent-side policy such as retry/tool/context/session behavior, while the `Model` object is already a concrete transport/decision engine.

## Use custom locations

Set `AXON_CONFIG` and `AXON_ENV`:

```bash
export AXON_CONFIG=/etc/myapp/axon.yaml
export AXON_ENV=/run/secrets/axon.env
```

Or call `LoadFrom(configPath, envPath)` directly.

For the complete precedence model, see [Environment variables](/axon/configuration/environment/).
