---
title: Security model
description: What Axon isolates, what it does not isolate, and what an embedder must trust.
---

Axon limits tool cost and scopes internal capabilities, but **Axon is not a sandbox**.

That is the most important security fact in the package.

## Filesystem access is not confined to the working directory

`Workspace.ResolvePath` joins relative paths against the session working directory, but absolute paths are returned unchanged. Relative paths containing `..` are also not rejected.

The built-in `read`, `write`, and `search` tools therefore use `Cwd` as a convenience base, not as a security boundary.

## `exec` is a real shell

Foreground and background execution use:

```text
sh -lc <model-supplied command>
```

with the selected working directory. Axon creates a process group so it can terminate process trees, and stdin is `/dev/null`, but the command otherwise runs with the permissions of the Axon process.

Do not execute an untrusted agent on a host where those permissions are unacceptable.

## MCP servers are real subprocesses

`MCPServer` commands are spawned as child processes. They inherit the process environment; optional `Env` entries are appended to it. MCP tools can do whatever their server implementation permits.

## Secrets are separated from built-in tool settings

The configuration loader supports `${VAR}` references in provider `api_key` and `base_url`. Provider secrets are resolved before the `Settings` reaches the runtime.

Built-in tools receive flattened `Limits`, not the `Settings` tree, so the built-in implementations have no path to provider credentials through their constructor dependencies.

This does **not** constrain caller-supplied custom tool closures: your own `Tool.Fn` can capture anything your application gives it.

## Session files are private by mode, shell logs are not a secret store

Session JSON is written with mode `0600`. Parent state directories are created with `0755`.

Background shell logs are ordinary files under the configured data directory and are created with the process umask applied. Treat the entire data directory as potentially sensitive because tool output can contain source code, paths, environment-derived output, or command results.

## Output limits are cost controls, not content filters

Read/search/exec/background-output caps are designed to bound latency, memory, and context growth. They do not sanitize secrets or malicious content.

The read tool refuses likely binary files to avoid wasting context, but that heuristic is not a data-loss-prevention system.

## Recommended embedding boundary

Run Axon with the **OS/container permissions you are willing to give the model**. If you need filesystem, network, syscall, credential, or process isolation, provide it outside Axon using the host/container/runtime boundary.
