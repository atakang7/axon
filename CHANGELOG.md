# Changelog

## [Unreleased]

### Added
- Streaming SSE responses from any OpenAI-compatible API
- Persistent sessions with full message history
- File tools: `read_file`, `list_files`, `edit_file`
- Undo support for file edits
- Multi-provider support via `~/.config/agent/providers.json`
- Braille spinner with clean terminal output
- Retry logic with backoff on LLM failures
- Ctrl+C to abort in-flight requests
