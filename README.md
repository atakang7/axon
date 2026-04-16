# axon

A minimal terminal coding agent. Connects to any OpenAI-compatible LLM and gives it tools to read, list, and edit files in your codebase.

## Install

```sh
go install github.com/atakang7/axon@latest
```

Or build from source:

```sh
git clone https://github.com/atakang7/axon
cd axon
go build -o axon .
```

## Setup

Create `~/.config/agent/providers.json`:

```json
{
  "providers": [
    {
      "name": "local",
      "base_url": "http://localhost:11434",
      "model": "llama3"
    },
    {
      "name": "openai",
      "base_url": "https://api.openai.com",
      "model": "gpt-4o",
      "api_key": "sk-..."
    }
  ]
}
```

## Usage

```sh
# use default provider (ollama)
axon

# select a provider
LLM_PROVIDER=openai axon
```

### Commands

| Command | Description |
|---------|-------------|
| `/new` | Start a fresh session |
| `/undo` | Revert the last file edit |
| `/session` | Show session info |

## How it works

Each session persists to `~/.local/share/agent/session.json` — full message history and edit snapshots for undo. Resume any previous session by just running `axon` again.

The agent has three tools: `list_files`, `read_file`, `edit_file`. It uses them autonomously to explore and modify your codebase based on your instructions.
