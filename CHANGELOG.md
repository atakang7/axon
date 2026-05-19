## [1.0.0](https://github.com/atakang7/axon/compare/v0.4.3...v1.0.0) (2026-05-19)


### ⚠ BREAKING CHANGES

* github.com/atakang7/axon/cmd/axon is gone. Replace
"go install github.com/atakang7/axon/cmd/axon@latest" with
"go install github.com/atakang7/bouton/cmd/bouton@latest". The runtime
import path github.com/atakang7/axon/agent is unchanged.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>

### Features

* drop bundled CLI; axon is now library-only ([69e2026](https://github.com/atakang7/axon/commit/69e20268278087c6a71ba41c8dca4a3c4d145de0))

## [0.4.3](https://github.com/atakang7/axon/compare/v0.4.2...v0.4.3) (2026-05-19)


### Bug Fixes

* **ci:** narrow semantic-release BREAKING-CHANGE keywords ([ffe6a57](https://github.com/atakang7/axon/commit/ffe6a570c7d5e788c2909d756d1a4dcb447884f8))

## [0.4.2](https://github.com/atakang7/axon/compare/v0.4.1...v0.4.2) (2026-05-19)


### Bug Fixes

* **ci:** drop Windows from goreleaser matrix ([fd401bf](https://github.com/atakang7/axon/commit/fd401bf14bfa861f99cd32a19f1b5022786ca326))

## [0.4.1](https://github.com/atakang7/axon/compare/v0.4.0...v0.4.1) (2026-05-19)


### ⚠ BREAKING CHANGES

* syntax, and the automated release flow.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>

### Bug Fixes

* **ci:** verify automated release pipeline end-to-end ([0670412](https://github.com/atakang7/axon/commit/0670412ac7edcfd9e0229fa6f0de01820cd8e26b))


### CI

* automate releases on every push to main ([4572ce3](https://github.com/atakang7/axon/commit/4572ce3bd7295102d40c1e6707b8a14072db8d99))

# Changelog

All notable changes to axon will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://www.semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.4.0] - 2026-05-19

### Added

- **Public Go library API.** The runtime is importable as `github.com/atakang7/axon/agent`. Surface: `Config`, `New`, `Step`, `Run`, `Interrupt`, `Reset`, `Undo`, `Cd`, `Session`, `Close`, `SessionPath`.
- **`Config.OnEvent`** — plain `func(ctx, Event)` field for observability. The runtime emits structured `Event`s with a `Kind` discriminator (`KindToken`, `KindToolCall`, `KindToolResult`, `KindTurnEnd`, `KindPruneStart`/`End`, ...). Fan-out is whatever the embedder writes inside the closure; no Handler interface, no MultiHandler ceremony.
- **Sentinel errors:** `ErrNoProvider`, `ErrNoSystemPrompt`, `ErrToolNotFound`, `ErrDuplicateTool`, `ErrInterrupted`.
- **CLI exports:** `DataDir`, `ConfigDir`, `ProvidersPath`, `SessionPath`, `EnvString`, `ApplyProviderEnvOverrides`, `ProviderNames` — small surface so CLIs can resolve XDG paths without re-implementing them.
- **`examples/minimal/main.go`** — the 30-line embed.

### Changed

- **Repository structure flipped:** runtime is `agent/`; reference CLI is `cmd/axon/`. The `internal/` boundary that made the runtime un-importable is gone.
- **CLI shell moved out of the runtime.** `Main()`, the interactive provider picker, `lastChoice` persistence, the YAML loader, `customtool.go`, `ui.go`, and the `pasteAwareInput` reader all live in `cmd/axon` now. The runtime no longer writes to stdout — all output goes through `Config.OnEvent`.
- **Slash commands are CLI-only.** `/new`, `/undo`, `/cd`, `/pwd`, `/session` live in `cmd/axon/commands.go` and map onto methods on `*Agent`.
- **`Config.SystemPrompt` is required.** The runtime has no opinion of its own about what an agent is; the role text is the embedder's call. CLI ships a small default-prompt string for its own use.

### Removed

- `agent.Main()` — replaced by `New` + `Step`/`Run`.
- `agent.BuildTools` — `New` does the composition.
- `agent.NewBare`, `agent.Builtins`, `Config.DisableBuiltins`, YAML `disable_builtins` — built-ins are unconditional. One constructor.
- `agent.Handler` interface, `HandlerFunc`, `MultiHandler`, `DiscardHandler` — replaced by `Config.OnEvent`. Composition is a closure.
- JSONL event log and `--log-json` flag. Embedders who want structured logs write 5 lines of `OnEvent` that delegate to `slog` or anything else.
- `defaultRolePrompt` — the runtime no longer ships a coding-agent personality.
- Direct `ui*` and `logger.Emit` calls from the runtime.
- All test files. They referenced the pre-refactor types and will be reintroduced against the new API in a follow-up.
- **`park`, `recall`, `forget`, `refresh` as model-facing tools.** Park / Recall / Forget are now `Session` methods driven by the secondary-LLM pruner, not tools the model invokes. `refresh` is gone entirely. The current built-in tool set is: `read`, `write`, `exec`, `bash_output`, `kill_shell`, `search`, `task`.
- **REALITY CHECK / ANCHORING PASS / MOMENTUM BEAT prompt regime.** The system prompt is now a thin role + built-in tool catalog + probes + project orientation; see `agent/prompt.go`.

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
