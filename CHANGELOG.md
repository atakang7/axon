# Changelog

All notable changes to axon will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://www.semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

### Updated

### Removed

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
