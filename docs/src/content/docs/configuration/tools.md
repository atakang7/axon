---
title: Tool limits
description: Configure built-in tool defaults and ceilings, with exact notes on which values are hard limits.
---

The `tools` section is projected into a flat `Limits` value when an agent is constructed.

Do not assume every field named `max_*` or every configured default behaves as a hard user-request ceiling. The current executable paths differ by tool; this page classifies them exactly.

## Complete defaults

```yaml
tools:
  read:
    lines: 200
    max_bytes: 2MiB

  exec:
    timeout: 30s
    max_timeout: 10m
    output_bytes: 12000
    tail_lines: 50
    max_tail_lines: 500
    kill_grace: 2s

  bash_output:
    max_bytes: 32KiB

  search:
    timeout: 30s
    max_matches: 100
    output_bytes: 12000
```

## At a glance

| Field | Current runtime role | Hard ceiling? |
| --- | --- | --- |
| `read.lines` | default line count for slice reads | **no** — caller can request a larger positive `limit` |
| `read.max_bytes` | refuses full-file read above size | **yes** |
| `exec.timeout` | default foreground timeout | default only |
| `exec.max_timeout` | clamps requested foreground timeout | **yes** |
| `exec.output_bytes` | caps captured foreground stdout+stderr bytes | **yes** |
| `exec.tail_lines` | stored/projected setting | **currently not used as a fallback by foreground exec** |
| `exec.max_tail_lines` | clamps requested `tail_lines` | **yes** |
| `exec.kill_grace` | foreground timeout cleanup / `kill_shell` grace | **yes for those paths** |
| `bash_output.max_bytes` | default poll size when call omits/nonpositively sets `max_bytes` | **no** — caller can request a larger value |
| `search.timeout` | timeout for each ripgrep invocation | **yes** |
| `search.max_matches` | default `max_matches` passed to ripgrep | **no** — caller can request a larger value |
| `search.output_bytes` | caps combined ripgrep output bytes | **yes** |

## Read settings

### `read.lines`

Used when `read` runs in `slice` mode and the call's `limit <= 0`.

A positive per-call `limit` is accepted without clamping to this value.

### `read.max_bytes`

Used only for `full` mode. If the file's stat size exceeds the configured value, Axon returns a refusal message instead of reading the file.

:::note
The current refusal string tells the model to raise `AXON_READ_MAX_BYTES`, but the current configuration system no longer reads that environment variable. The real setting is `tools.read.max_bytes`.
:::

## Exec settings

### `timeout`

Used when foreground `timeout_seconds <= 0`.

### `max_timeout`

A positive requested timeout larger than this is clamped down.

### `output_bytes`

Hard byte cap on the foreground command's combined stdout/stderr capture buffer. Additional writes are discarded while the command continues.

### `tail_lines`

`Limits.ExecTailLines` is populated from this field, but the current foreground exec validation requires the tool call itself to provide `tail_lines > 0`; it does not substitute `ExecTailLines` when missing.

So this setting currently has no effect on normal foreground calls unless another caller consumes `Limits` directly.

### `max_tail_lines`

Requested foreground tail size is clamped to this value.

### `kill_grace`

Used when a timed-out foreground command has been SIGKILLed but Axon is waiting for its output-copy path to release, and by the explicit `kill_shell` tool between SIGTERM and SIGKILL.

Agent-wide `BackgroundShells.KillAll()` currently uses its own fixed 2-second grace rather than this configured value.

## Background output

`bash_output.max_bytes` is a **default**, not a maximum. When a call supplies a positive `max_bytes`, the implementation uses that requested value directly.

## Search settings

### `timeout`

Hard timeout around the `rg` subprocess.

### `max_matches`

Used when the call omits/nonpositively sets its own value. The value is passed to ripgrep as `--max-count`.

That option is **per file**, so `100` means up to 100 matching lines in each searched file, not 100 matches globally.

### `output_bytes`

Hard cap on captured search output across the invocation.

## Zero values

`WithDefaults` replaces non-positive tool settings with defaults, so zero is generally not expressible as “unlimited” or “disabled.”
