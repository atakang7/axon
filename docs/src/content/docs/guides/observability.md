---
title: Build observability
description: Consume Axon's synchronous event stream without adding backpressure to the runtime.
---

## Attach a callback

```go
agent, err := axon.New(axon.Config{
    Model:        model,
    SystemPrompt: "...",
    OnEvent: func(ctx context.Context, e axon.Event) {
        log.Printf("turn=%d kind=%s", e.Turn, e.Kind)
    },
})
```

## Keep the callback fast

The callback is synchronous. If it blocks, the turn blocks at the emission point.

For a remote telemetry backend, decouple delivery:

```go
events := make(chan axon.Event, 512)

go func() {
    for e := range events {
        exporter.Write(e)
    }
}()

onEvent := func(ctx context.Context, e axon.Event) {
    select {
    case events <- e:
    default:
        // Define your own overload policy.
    }
}
```

Choose blocking, dropping, sampling, or bounded persistence deliberately; Axon does not impose an event-buffering policy.

## Separate progress UI from audit events

Use incremental events for presentation:

```text
token
reasoning
tool_arg_delta
```

Use resolved boundaries for action/audit records:

```text
tool_call
tool_result
tool_error
assistant_end
turn_start/turn_end
```

A `tool_arg_delta` may contain incomplete JSON. `tool_call` contains the complete arguments Axon is about to execute.

## Add model identity in your layer

`session_start` / `session_end` include `SessionInfo`, but the current generic `Model` interface gives Axon no universal method to ask a model for provider/model metadata. Those fields are therefore left blank by `New`/`Close`.

If your application knows the selected provider/model, add them to your log/span context outside the Axon event payload.

See [Events reference](/axon/reference/events/) for the complete payload map.
