# Changelog

All notable changes to axon will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://www.semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **Public Go library API.** The runtime is now importable as `github.com/atakang7/axon/agent`. New surface: `Config`, `New`, `NewBare`, `Builtins`, `Step`, `Run`, `Interrupt`, `Reset`, `Undo`, `Cd`, `Session`, `Close`, `SessionPath`. Embedders construct agents from Go directly; YAML is no longer the only way in.
- **`Handler` interface for observability** (slog-style). `Handler.Handle(ctx, Event)`, plus `HandlerFunc`, `MultiHandler`, `DiscardHandler`. The runtime emits structured `Event`s with a `Kind` discriminator (KindToken, KindToolCall, KindToolResult, KindTurnEnd, KindPruneStart/End, ...). The terminal renderer and JSONL log are now both Handlers built on this.
- **Sentinel errors:** `ErrNoProvider`, `ErrToolNotFound`, `ErrDuplicateTool`, `ErrInterrupted`.
- **Exports for CLI consumers:** `DataDir`, `ConfigDir`, `ProvidersPath`, `SessionPath`, `EnvString`, `ApplyProviderEnvOverrides`, `ProviderNames` — small surface so CLIs can resolve XDG paths without re-implementing them.

### Changed

- **Repository structure flipped:** runtime is `agent/`; reference CLI is `cmd/axon/`. The `internal/` boundary that made the runtime un-importable is gone.
- **CLI shell moved out of the runtime.** `Main()`, the interactive provider picker, `lastChoice` persistence, the YAML loader, `customtool.go`, `ui.go`, `jsonl_logger.go`, and the `pasteAwareInput` reader all live in `cmd/axon` now. The runtime no longer writes to stdout — all output goes through `Handler`.
- **Slash commands are CLI-only.** `/new`, `/undo`, `/cd`, `/pwd`, `/session` live in `cmd/axon/commands.go` and map onto methods on `*Agent`.
- **Built-ins are unconditional in `New`.** The old `DisableBuiltins` knob on the runtime is gone; use `NewBare` plus `Builtins` for explicit composition. The CLI still honors YAML's `disable_builtins` by going through `NewBare`.
- **`buildSystemPrompt` no longer takes `*AgentConfig`** — it takes a system-prompt string and a "disabled built-ins" set. Decouples the runtime from any config-file format.

### Removed

- `agent.Main()` — replaced by `New` + `Step`/`Run` on the embedder side. `cmd/axon/main.go` is the new CLI entry.
- `agent.BuildTools` — `New` does the composition.
- Direct `ui*` and `logger.Emit` calls from the runtime — every observable moment is an `Event` now.
- All test files. They referenced the pre-refactor types and will be reintroduced against the new API in a follow-up.

## [0.3.0] - 2026-05-07

### Added

- TTL-based memory management with auto-parking
- Enhanced system prompt with REALITY CHECK requirement
- Tool surface: `read`, `write`, `exec`, `search`, `task`, `park`, `recall`, `forget`, `refresh`, `bash_output`, `kill_shell`
- Structured turn lifecycle with mandatory verification
- Dashboard showing active blocks, TTL counts, parked blocks, and current task
- Aggressive context triage with immediate forgetting of raw data
- Pruner component for automatic context management (fires at ~10K tokens)

### Updated

- System prompt now requires REALITY CHECK before any tool call
- Memory system replaced archive/retrieve with TTL-based park/recall/forget/refresh
- README updated with current tool descriptions and examples
- Enhanced guidance for proper prompt usage and turn discipline

### Core loop (Updated)

The agent follows a strict turn lifecycle:

1. **REALITY CHECK** - Must output GOAL, CONSTRAINTS, ACTION, TRASH before any tool
2. **ANCHORING PASS** - Full six-slot ATTENTION block (GOAL, STATE, HISTORY, CONSTRAINTS, MOVES, DIMENSION)
3. **MOMENTUM BEAT** - Three-line reasoning between tool calls (DELTA, DRIFT, NEXT)
4. **GATHER & COMMUNE** - Bundle task + first action if requirements clear
5. **EXECUTE & VERIFY** - One code change per turn, mandatory verification
6. **PURGE CONTEXT** - Forget raw data immediately, manage TTL pressure
7. **DELIVER & HALT** - Memory tools last, silent completion

## [0.2.0] - 2025-01-15

### Added

- Session persistence with automatic resume
- File edit undo functionality (`/undo`)
- Multiple provider support (OpenAI, Claude, Ollama, LM Studio)
- Environment variable overrides for provider config

### Changed

- Moved from custom prompts to structured tool definitions

## [0.1.0] - 2024-12-01

### Added

- Initial release
- Basic tool loop: read, write, exec, search
- OpenAI-compatible API support
