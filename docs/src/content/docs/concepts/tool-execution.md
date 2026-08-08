---
title: Tool execution
description: How built-in, custom, and MCP tools become model-callable capabilities.
---

Every tool ultimately becomes the same runtime value:

```go
type Tool struct {
    Name        string
    Description string
    Schema      map[string]any
    Fn          func(context.Context, json.RawMessage) (string, error)
}
```

## Construction order

During `New`:

1. MCP servers start and their discovered tools are appended to `Config.Tools`;
2. built-ins are bound, minus `ExcludeBuiltins`;
3. caller/MCP tools are validated;
4. duplicate names are rejected;
5. the final toolset is stored on the agent.

A built-in name becomes available to a custom tool only when that built-in was excluded.

## What the model sees

Tool implementation functions are projected away at the model boundary. `toolSpecs` sends only:

- name;
- description;
- JSON Schema.

The shipped client serializes these as OpenAI function tools.

The initial Axon system message also embeds each tool's description and schema and tells the model to use native JSON tool calling rather than textual XML/Markdown calls.

## Execution is sequential

A model may return multiple tool calls. Axon appends them to `StepResult.ToolCalls` and executes them one at a time in the returned order.

A tool function receives the active turn context. Built-ins that start their own work generally honor cancellation through that context; custom tools must choose to honor it themselves.

## Tool errors stay inside the conversation

When a tool `Fn` returns an error, `runTool` emits `tool_error` and converts the error string into a normal `role=tool` message.

The model can therefore inspect the failure and recover on the next loop iteration. A tool failure is not automatically a `Step` failure.

If the model names a tool that does not exist, the runtime similarly returns a tool message containing `"tool not found"`.

## Capability boundaries for built-ins

Built-ins are intentionally constructed against narrow interfaces instead of the whole `Agent`:

- `Workspace`: directory resolution + edit recording;
- `Plan`: register/advance/replan task state;
- `BackgroundShells`: process registry;
- `Limits`: output/time caps projected from tool settings.

This keeps provider credentials and unrelated runtime state outside the built-in tool surface.

## Built-ins

The standard set is:

```text
read
write
exec
bash_output
kill_shell
search
task
```

See [Built-in tools](/axon/reference/built-in-tools/) for exact schemas, modes, limits, and edge behavior.
