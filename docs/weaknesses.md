# Known weaknesses

These are visible implementation boundaries, not a claim that an incident has occurred.

## MCP calls can wait indefinitely

`mcp.go` implements `mcpClient.call` by receiving directly from `<-ch` without a timeout or context cancellation. A server that never responds can stall startup or a tool call. Pass a context through MCP calls, bound the wait and remove abandoned pending requests.

## Session persistence is not atomic

`session.go` uses `os.WriteFile` to replace the JSON session and does not check the `MkdirAll` error. `loop.go` reports some later save failures as events rather than failing the turn. Interrupted writes can leave an incomplete record. Write to a temporary file, sync and rename it, and make persistence failures visible to callers.

## Local tools are not sandboxed

`std_tools.go` exposes a shell tool; `mcp.go` starts child processes. Both run with the embedding application's operating system privileges. `Config.ExcludeBuiltins` can remove shell access, but there is no isolated execution boundary in the library. Restrict enabled tools or run the embedding process in a separate sandbox when inputs are untrusted.

## Unattended turns need an explicit cap

`api.go` gives `Config.MaxIterations=0` an unbounded model-call loop. Interactive callers can interrupt, but batch jobs need to set a positive value and enforce context deadlines to bound usage.
