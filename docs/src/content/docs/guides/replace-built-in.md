---
title: Replace a built-in tool
description: Remove one built-in capability and reuse its model-visible name for your own implementation.
---

A custom tool cannot normally collide with an existing built-in name. `New` rejects duplicate names.

To replace one deliberately, exclude it first.

## Example: replace `search`

```go
customSearch := axon.Tool{
    Name:        "search",
    Description: "Search through the application's indexed repository service.",
    Schema:      searchSchema,
    Fn:          searchFn,
}

agent, err := axon.New(axon.Config{
    Model:           model,
    SystemPrompt:    "...",
    ExcludeBuiltins: []string{"search"},
    Tools:           []axon.Tool{customSearch},
})
```

Built-in names are:

```text
read
write
exec
bash_output
kill_shell
search
task
```

## Why keep the same name?

Reusing a familiar built-in name can let your system prompt and model behavior remain stable while changing implementation authority—for example replacing unrestricted filesystem search with a service-backed index.

## Reset preserves the replacement

The agent remembers both the exclusion list and caller-supplied tools. `Reset` rebinds built-ins with the same exclusions and appends the same custom tools again.

## MCP collisions follow the same rule

MCP-discovered tools are added to the custom-tool set before duplicate validation. If an MCP server exposes `search` while built-in `search` is still enabled, construction fails.

Exclude the built-in if that collision is intentional.
