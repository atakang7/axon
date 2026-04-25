# axon

A minimal terminal coding agent for developers. Connects to any OpenAI‑compatible LLM and gives it flat tools to read files, write files, search text, run shell commands, and manage archived context.

**Principles**
- **Config without magic defaults** – runtime behavior comes from provider config or env
- **Session persistence** – every conversation is saved and resumes automatically
- **LLM-managed context** – the model decides when to compress or restore older context
- **Deterministic** – follows a predictable tool loop
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
| Command | Description |
|---------|-------------|
| `/new` | Start a fresh session (clears history) |
| `/undo` | Revert the last file edit |
| `/session` | Show session ID, turn count, edit count |
| Ctrl‑C | Abort in‑flight LLM request |

### Example workflow
```
❯ add a function that greets the user

  ⎿  search { "query": "greet", "path": "." }
  ⎿  read { "path": "main.go" }
  
  I'll add a greet() function to main.go.

  ⎿  write { "path": "main.go", "old_str": "...", "new_str": "..." }
  
  Done. The function greets(name string) string is now in main.go.
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

### The agent loop
The agent follows a simple tool loop every turn:

1. **Aggressive context expansion** – Uses `read`, `search`, and `exec` to inspect files, search text, list directories, and run cheap read-only shell commands.
2. **Reason on the full picture** – With complete context, defines the task, identifies the minimal change, executes.
3. **Optional memory operations** – If the model decides older context should be compressed, it can call `archive`, and later `retrieve_archive` if needed.

### Tools
| Tool | Description |
|------|-------------|
| `read` | Read one file with line numbers |
| `write` | Create a file, replace a whole file, or replace one exact snippet |
| `search` | Search file contents recursively |
| `exec` | Run a shell command (`allow_write=true` required for mutations) |
| `archive` | Move old messages out of active context |
| `retrieve_archive` | Recall archived messages |

The agent uses `read` for file inspection, `write` for edits, `search` for text lookup, and `exec` for everything shell-shaped like `ls`, `find`, tests, and builds.

### Memory system
- The runtime sends raw conversation history to the model
- The model can archive conversations with `archive`
- Archived messages can be retrieved with `retrieve_archive`
- Context compression is model-directed, not automatic

### Safety guards
- Never edits files the user didn't ask about
- Never runs mutating shell commands without explicit `allow_write=true`
- Never uses `exec` to hunt for problems the user didn't point at
- Mutating command detection (`rm`, `mv`, `go fmt`, `git add`, etc.) blocks execution unless `allow_write=true`

---

## Configuration reference

### Environment variables
| Variable | Default | Purpose |
|----------|---------|---------|
| `LLM_PROVIDER` | none | Which configured provider to use when more than one exists |
| `LLM_PROVIDER_NAME` | `env` | Name to use for an env-only provider |
| `LLM_MODEL` | none | Model for env-only config, or override the selected provider model |
| `LLM_BASE_URL` | none | Base URL for env-only config, or override the selected provider base URL |
| `LLM_API_KEY` | none | API key for env-only config, or override the selected provider API key |
| `LLM_PROVIDER_EXTRA` | none | Raw provider JSON for env-only config or provider override |
| `AXON_CONFIG_DIR` | `${XDG_CONFIG_HOME:-~/.config}/agent` | Config directory override |
| `AXON_DATA_DIR` | `${XDG_DATA_HOME:-~/.local/share}/agent` | Data directory override |
| `AXON_PROVIDERS_PATH` | `${XDG_CONFIG_HOME:-~/.config}/agent/providers.json` | Provider config file path |
| `AXON_SESSION_PATH` | `${XDG_DATA_HOME:-~/.local/share}/agent/session.json` | Session file path |
| `AXON_READ_LIMIT` | `200` | Default max lines returned by `read` |
| `AXON_SEARCH_LIMIT` | `100` | Default max matches returned by `search` |
| `AXON_SEARCH_OUTPUT_LIMIT` | `12000` | Max captured bytes for `search` output |
| `AXON_EXEC_TIMEOUT_SECONDS` | `30` | Default timeout for `exec` |
| `AXON_EXEC_OUTPUT_LIMIT` | `12000` | Max captured bytes for `exec` output |
| `AXON_EXEC_TAIL_LINES` | `50` | Default trailing lines kept for `exec` output |
| `AXON_EXEC_MAX_TAIL_LINES` | `500` | Hard cap for `exec.tail_lines` |

### File locations
| Path | Purpose |
|------|---------|
| `${XDG_CONFIG_HOME:-~/.config}/agent/providers.json` | Default provider config location |
| `${XDG_DATA_HOME:-~/.local/share}/agent/session.json` | Default session location |

---

## Development

### Build and test
```sh
go build ./...
go test ./...
```

### Architecture
- `main.go` – entry point, provider selection, session load
- `agent.go` – core loop and slash-command handling
- `llm.go` – OpenAI‑compatible HTTP client with streaming
- `session.go` – persistent session (history, edits, undo)
- `memory.go` – archiving/retrieval of old messages
- `tools.go` – flat tool implementations (`read`, `write`, `exec`, `search`)
- `ui.go` – terminal UI (braille spinner, colors, prompts)

### Contributing
See [CONTRIBUTING.md](./CONTRIBUTING.md). Keep changes minimal, focused, and tested.

---

## Changelog
See [CHANGELOG.md](./CHANGELOG.md).

---

## License
MIT. See [LICENSE](./LICENSE).
