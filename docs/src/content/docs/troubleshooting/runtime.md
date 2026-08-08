---
title: Runtime & tools troubleshooting
description: Diagnose stream stalls, tool surprises, shell lifetime, ripgrep failures, and MCP hangs.
---

## `stream stalled: no data for ...`

The shipped model client received no scanner line for the configured `model.idle_timeout` and canceled the request.

Possible causes:

- provider stopped emitting SSE data;
- proxy buffered/held the response;
- idle timeout is too aggressive for the endpoint.

Transport timeout errors are retryable up to `retry.max_attempts`.

## Foreground `exec` says `tail_lines is required`

The JSON Schema does not mark `tail_lines` as required, but the executable foreground validation currently does.

Supply a positive value:

```json
{"command":"go test ./...","tail_lines":80}
```

The configured `tools.exec.tail_lines` is not currently substituted when omitted.

## Search says ripgrep is missing

Built-in search executes `rg` and requires it in `PATH`.

Install ripgrep in the Axon process environment or replace/exclude the built-in `search` capability.

## Search returns more matches than expected

`max_matches` maps to ripgrep `--max-count`, which is per file. The byte-output cap is the global guard on returned search text.

## Background output grows unexpectedly large

A positive per-call `bash_output.max_bytes` request is not clamped by the configured default. Audit the actual tool arguments when you need strict upper bounds.

## `Interrupt` did not stop a custom tool

Axon cancels the turn context. A custom `Tool.Fn` must honor that context itself; Go cannot force a function that ignores cancellation to return.

## MCP tool call hangs

The current MCP response wait has no per-call timeout/context select. If the child remains alive but never responds, the adapter can wait indefinitely.

Supervise the server/process externally or avoid that MCP dependency until the failure is corrected.

## Undo of a newly created file left an empty file

The edit ledger records an empty “before” value for a path that did not exist. Undo writes those prior bytes back; it does not currently distinguish creation from replacement by deleting the path.

## My tool says a formatter runs, but the file is unformatted

The current `write` tool description contains stale text about post-write formatting. The executable implementation only performs the requested mutation and atomic write; it does not run a formatter.
