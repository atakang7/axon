# axon

A minimal terminal coding agent for developers. Connects to any OpenAI‑compatible LLM and gives it flat tools to read files, write files, search text, run shell commands, and manage context with TTL-based memory.

**Principles**

- **Config without magic defaults** – runtime behavior comes from provider config or env
- **Session persistence** – every conversation is saved and resumes automatically
- **TTL-based context management** – automatic parking/recall based on token pressure
- **Deterministic** – follows a structured turn lifecycle
- **Undo‑ready** – every file edit can be reverted with `/undo`

---

## Install

### From source

```sh
git clone https://github.com/atakang7/axon
cd axon
go build -o axon .
# optionally move to your PATH
mv axon ~/.local/bin/
```

### With `go install`

```sh
go install github.com/atakang7/axon@latest
```

---

## Setup

### 1. Provider config file

Create `${XDG_CONFIG_HOME:-~/.config}/agent/providers.json`, or point `AXON_PROVIDERS_PATH` at a different file:

```json
{
  "providers": [
    {
      "name": "ollama",
      "base_url": "http://localhost:11434",
      "model": "llama3"
    },
    {
      "name": "openai",
      "base_url": "https://api.openai.com",
      "model": "gpt-4o",
      "api_key": "sk-..."
    },
    {
      "name": "claude",
      "base_url": "https://api.anthropic.com",
      "model": "claude-3-opus-20240229",
      "api_key": "sk-ant-..."
    },
    {
      "name": "lmstudio",
      "base_url": "http://localhost:1234",
      "model": "local-model"
    }
  ]
}
```

- `name` – used in `LLM_PROVIDER`
- `base_url` – API endpoint (axon adds `/v1` if missing)
- `model` – model identifier for that provider
- `api_key` – optional; bearer token sent in `Authorization` header
- `provider` – optional extra provider‑specific JSON (e.g., `"openai"` for LiteLLM)

If exactly one provider is configured, axon uses it automatically. If multiple providers are configured, set `LLM_PROVIDER`.

### 2. Env-only provider config

You can skip the config file and launch directly with env vars:

```sh
LLM_MODEL=gpt-4o \
LLM_BASE_URL=https://api.openai.com \
LLM_API_KEY=sk-... \
axon
```

---

## Usage

### Start a session

```sh
axon                     # uses the only configured provider
LLM_PROVIDER=openai axon # select one configured provider
LLM_MODEL=gpt-4o LLM_BASE_URL=https://api.openai.com axon
```

### In‑session commands

| Command    | Description                             |
| ---------- | --------------------------------------- |
| `/new`     | Start a fresh session (clears history)  |
| `/undo`    | Revert the last file edit               |
| `/session` | Show session ID, turn count, edit count |
| Ctrl‑C     | Abort in‑flight LLM request             |

### Example workflow

```
❯ Create a Go HTTP server on port 8080 with /health endpoint

> 1. GOAL: Create server.go with basic HTTP server
> 2. CONSTRAINTS: Go language, port 8080, /health endpoint
> 3. ACTION: task + write
> 4. TRASH: none

  ⎿  task { "objective": "Create Go HTTP server", "definition_of_done": "Server compiles and responds on port 8080", "hypothesis": "Go has built-in net/http", "steps": ["Write server.go with HTTP handler", "Verify with go build"], "reason": "Starting server creation task" }
  ⎿  write { "path": "server.go", "mode": "create", "content": "package main\n\nimport (...)", "reason": "Creating HTTP server implementation" }

  ⎿  exec { "mode": "verify", "tail_lines": 20, "expected_outcome": "Server compiles", "reason": "Verify server compiles without errors" }

  Done. Server created and verified.
```

---

## How it works

### Session persistence

Every session is saved to `${XDG_DATA_HOME:-~/.local/share}/agent/session.json` by default, or to `AXON_SESSION_PATH` if set:

- Full message history (user, assistant, tool calls/results)
- File‑edit snapshots (for undo)
- Current working directory
- Turn counter

Run `axon` again and you resume exactly where you left off.

### The turn lifecycle

Every turn follows a structured lifecycle:

1. **REALITY CHECK** – Output GOAL, CONSTRAINTS, ACTION, TRASH before any tool
2. **ANCHORING PASS** – Full reasoning with ATTENTION block (GOAL, STATE, HISTORY, CONSTRAINTS, MOVES, DIMENSION)
3. **MOMENTUM BEAT** – Three-line reasoning between tool calls (DELTA, DRIFT, NEXT)
4. **GATHER & COMMENCE** – Bundle task + first action if requirements clear
5. **EXECUTE & VERIFY** – One code change per turn, mandatory verification
6. **PURGE CONTEXT** – Forget raw data immediately, manage TTL pressure
7. **DELIVER & HALT** – Memory tools last, silent completion

### Tools

| Tool          | Description                                                                                       |
| ------------- | ------------------------------------------------------------------------------------------------- |
| `read`        | Read files (skeleton/slice/full modes) with line numbers                                          |
| `write`       | Create, overwrite, or modify files (create/overwrite/replace_string/replace_lines/insert_at_line) |
| `search`      | Search file contents (literal/regex/trace modes)                                                  |
| `exec`        | Run shell commands (run/verify modes) with output tailing                                         |
| `bash_output` | Read new output from a background shell (delta only)                                              |
| `kill_shell`  | Stop a background shell                                                                           |
| `task`        | Register/advance/replan task objectives                                                           |
| `park`        | Replace block content with breadcrumb (recoverable)                                               |
| `recall`      | Restore parked content                                                                            |
| `forget`      | Remove block entirely (irrecoverable)                                                             |
| `refresh`     | Reset TTL for active blocks                                                                       |

### Task tool

For multi-step work, use the `task` tool with actions:

- **register**: Commit objective, definition_of_done, hypothesis, and step list (min 2 steps)
- **advance**: Mark current step done, move to next
- **replan**: Replace hypothesis and steps (when foundation collapses)

```json
{
  "action": "register",
  "objective": "Create Go HTTP server",
  "definition_of_done": "Server compiles and responds on port 8080",
  "hypothesis": "Go has built-in net/http",
  "steps": ["Write server.go with HTTP handler", "Verify with go build"],
  "reason": "Starting server creation task"
}
```

### Memory system (TTL-based)

The agent manages context automatically using TTL:

- Each active block has a TTL that decrements each turn
- Dashboard shows TTL counts: `#m3 TTL=2` means 2 turns until auto-park
- Pruner fires when context exceeds ~10,000 tokens
- Blocks auto-park when TTL reaches 0
- Use `refresh` to keep blocks active, `park` for recoverable compression, `forget` for removal

**State model:**

| State     | Description                                                    |
| --------- | -------------------------------------------------------------- |
| Active    | Full content in message stream                                 |
| Parked    | Replaced by breadcrumb: `[#m3 parked \| reason: X \| gist: Y]` |
| Forgotten | Dropped entirely (no breadcrumb)                               |

### Safety guards

- Never edits files the user didn't ask about
- Never runs mutating shell commands without explicit `allow_write=true`
- Never uses `exec` to hunt for problems the user didn't point at
- Mutating command detection (`rm`, `mv`, `go fmt`, `git add`, etc.) blocks execution unless `allow_write=true`

---

## Configuration reference

### Environment variables

| Variable                    | Default                                               | Purpose                                                                  |
| --------------------------- | ----------------------------------------------------- | ------------------------------------------------------------------------ |
| `LLM_PROVIDER`              | none                                                  | Which configured provider to use when more than one exists               |
| `LLM_PROVIDER_NAME`         | `env`                                                 | Name to use for an env-only provider                                     |
| `LLM_MODEL`                 | none                                                  | Model for env-only config, or override the selected provider model       |
| `LLM_BASE_URL`              | none                                                  | Base URL for env-only config, or override the selected provider base URL |
| `LLM_API_KEY`               | none                                                  | API key for env-only config, or override the selected provider API key   |
| `LLM_PROVIDER_EXTRA`        | none                                                  | Raw provider JSON for env-only config or provider override               |
| `AXON_CONFIG_DIR`           | `${XDG_CONFIG_HOME:-~/.config}/agent`                 | Config directory override                                                |
| `AXON_DATA_DIR`             | `${XDG_DATA_HOME:-~/.local/share}/agent`              | Data directory override                                                  |
| `AXON_PROVIDERS_PATH`       | `${XDG_CONFIG_HOME:-~/.config}/agent/providers.json`  | Provider config file path                                                |
| `AXON_SESSION_PATH`         | `${XDG_DATA_HOME:-~/.local/share}/agent/session.json` | Session file path                                                        |
| `AXON_READ_LIMIT`           | `200`                                                 | Default max lines returned by `read`                                     |
| `AXON_SEARCH_LIMIT`         | `100`                                                 | Default max matches returned by `search`                                 |
| `AXON_SEARCH_OUTPUT_LIMIT`  | `12000`                                               | Max captured bytes for `search` output                                   |
| `AXON_EXEC_TIMEOUT_SECONDS` | `30`                                                  | Default timeout for `exec`                                               |
| `AXON_EXEC_OUTPUT_LIMIT`    | `12000`                                               | Max captured bytes for `exec` output                                     |
| `AXON_EXEC_TAIL_LINES`      | `50`                                                  | Default trailing lines kept for `exec` output                            |
| `AXON_EXEC_MAX_TAIL_LINES`  | `500`                                                 | Hard cap for `exec.tail_lines`                                           |

### File locations

| Path                                                  | Purpose                          |
| ----------------------------------------------------- | -------------------------------- |
| `${XDG_CONFIG_HOME:-~/.config}/agent/providers.json`  | Default provider config location |
| `${XDG_DATA_HOME:-~/.local/share}/agent/session.json` | Default session location         |

---

## Development

### Build and test

```sh
go build ./...
go test ./...
```

### Architecture

- `main.go` – entry point, provider selection, session load
- `agent.go` – core loop, turn lifecycle, slash-command handling
- `llm.go` – OpenAI‑compatible HTTP client with streaming
- `session.go` – persistent session (history, edits, undo)
- `memory.go` – TTL-based context management (park/recall/forget/refresh)
- `pruner.go` – automatic context pruning based on token pressure
- `tools.go` – flat tool implementations (`read`, `write`, `exec`, `search`)
- `ui.go` – terminal UI (spinner, colors, prompts)
- `config.go` – provider resolution, env/config loading
- `picker.go` – model picker for multi-model providers
- `probes.go` – system probes (git status, go version, etc.)

### Contributing

See [CONTRIBUTING.md](./CONTRIBUTING.md). Keep changes minimal, focused, and tested.

---

## Changelog

See [CHANGELOG.md](./CHANGELOG.md).

---

## License

MIT. See [LICENSE](./LICENSE).
