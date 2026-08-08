<div align="center">
  <h1>Axon</h1>
  <p><b>Go Runtime for LLM Agents</b></p>
</div>

Axon orchestrates the agent loop. It streams model responses, dispatches tool calls, maintains an append-only session, prunes context under token constraints, and emits deterministic structured events.

You provide the model, system prompt, and tools. Axon handles the execution.

>**[Documentation](https://atakang7.github.io/axon)**

## Documentation Tree

- **[Overview]**
  - [Introduction](docs/src/content/docs/overview/introduction.md)
  - [Quick Start](docs/src/content/docs/overview/quick-start.md)
- **[Core Components]**
  - [Configuration](docs/src/content/docs/core/configuration.md)
  - [Tools](docs/src/content/docs/core/tools.md)
  - [Events & Observability](docs/src/content/docs/core/events.md)

## License

[MIT](LICENSE).
