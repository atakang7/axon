---
title: Sessions & paths
description: Configure persistent session placement and background-shell state locations.
---

```yaml
session:
  data_dir: ""
  path: ""
```

The default values are resolved at runtime rather than represented as literal YAML strings.

## `data_dir`

This is the root for Axon's default session files and background-shell log hierarchy.

Precedence:

```text
AXON_DATA_DIR
then session.data_dir
then $XDG_DATA_HOME/agent
then ~/.local/share/agent
```

The default leaf is currently named **`agent`**, not `axon`.

## `path`

Pins the session to one exact file.

Precedence:

```text
AXON_SESSION_PATH
then session.path
then <data_dir>/sessions/<derived-key>.json
```

A pinned path wins regardless of `data_dir`.

## Derived session key

Without a pin, Axon uses the process current working directory to build a stable key:

```text
<cwd-basename>-<first 12 hex chars of SHA-256(abs-cwd)>
```

For example, two different directories both named `api` get distinct hashes and therefore distinct session files.

## `Config.Cwd` does not select the default session

`New` chooses/loads the session **before** applying `Config.Cwd` to that session.

So:

```go
axon.New(axon.Config{Cwd: "/repo/b", ...})
```

still derives the default session filename from the process working directory at construction time, unless `Config.Session` or a pinned session path is supplied.

Use `session.path`, `AXON_SESSION_PATH`, or a caller-built `Session` when session identity must follow some application-specific workspace key.

## Background log hierarchy

The configured data root also produces:

```text
<data_dir>/bg/<pid>/shells-<random>/bash_N.log
```

The PID separates process runs; each agent shell registry creates its own temporary subdirectory so independently created registries do not reuse `bash_1.log`.

If the configured background root cannot be created, the registry falls back to the OS temporary directory.

## Custom Session wins over path selection

When `Config.Session` is non-nil, `New` reuses that exact pointer and does not call the configured `SessionFile()` path loader.
