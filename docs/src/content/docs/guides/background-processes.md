---
title: Run background processes
description: Start servers/watchers, poll only new output, and clean them up deliberately.
---

Use background execution for commands whose completion time is uncertain or whose purpose is to stay alive.

## Start the process

The model-facing `exec` call uses:

```json
{
  "command": "go run ./cmd/server",
  "run_in_background": true
}
```

A successful start returns a handle such as:

```text
shell_id: bash_1
pid: 12345
status: running
```

For background execution, `tail_lines` and foreground timeout fields are ignored.

## Poll output

Call `bash_output`:

```json
{
  "shell_id": "bash_1",
  "tail_lines": 50
}
```

The tool returns only log bytes appended since the previous read for that shell.

Use `max_bytes` when one delta itself may be large:

```json
{
  "shell_id": "bash_1",
  "max_bytes": 16384
}
```

A positive per-call `max_bytes` currently overrides the configured default and is not clamped by `tools.bash_output.max_bytes`.

## Stop the process

```json
{
  "shell_id": "bash_1"
}
```

through `kill_shell` sends SIGTERM to the process group, waits the configured `tools.exec.kill_grace`, then SIGKILLs if needed.

## Clean up even when the model forgets

`Reset` and `Close` call the registry's `KillAll`, so background processes do not intentionally survive the agent lifecycle.

Still prefer explicit `kill_shell` when the process is no longer useful: it releases ports/resources earlier and gives the model a clear lifecycle.

## Do not use foreground for uncertain I/O

Foreground commands have a bounded timeout and stdin `/dev/null`, but a server/watcher or uncertain remote client is semantically background work even if it sometimes exits quickly.

Design around whether the runtime should wait for completion, not around your guess of how fast the command will be today.
