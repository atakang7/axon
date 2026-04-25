# Changelog

All notable changes to axon will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Streaming SSE responses from any OpenAI‑compatible API
- Persistent sessions with full message history
- Flat tool surface: `read`, `write`, `exec`, `search`, `archive`, `retrieve_archive`
- Undo support for file edits (`/undo` command)
- Multi‑provider support via provider config or env overrides
- Memory system: model-directed archive/retrieve tools
- Braille spinner with clean terminal output
- Retry logic with exponential backoff on LLM failures
- Ctrl‑C to abort in‑flight requests
- Environment-driven paths and runtime limits
- Safety guard against mutating shell commands without `allow_write=true`

### Core loop
The agent follows a simple tool loop every turn:
1. **Aggressive context expansion** – Explore with `read`, `search`, and `exec`
2. **Reason on the full picture** – Define task, identify minimal change, execute
3. **Optional memory operations** – Archive or retrieve context only when the model chooses to
