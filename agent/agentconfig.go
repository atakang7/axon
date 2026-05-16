package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// agentconfig.go — agent personality + custom tooling definition.
//
// An "agent" in axon is a system prompt plus a tool set. The core runtime
// always provides the built-in tools (read/write/exec/search/task — the
// "hands and legs" every agent needs). An agent config adds a role-shaped
// system prompt on top and optionally appends custom tools or disables
// specific built-ins.
//
// Configs live at $AXON_AGENTS_DIR (default ~/.config/axon/agents/) as
// <name>.yaml. Select one at launch with `axon --agent <name>`. When no
// flag is passed the runtime uses the built-in default — current behavior.
//
// MCP-readiness: every custom tool declares `type: shell` (the only kind
// implemented today). A `type: mcp` entry is the future expansion point and
// will plug in via the same Tool surface, no schema changes required.

// AgentConfig is the on-disk shape of an agent definition.
type AgentConfig struct {
	// Name is the agent identifier (matches the file basename when loaded
	// from disk). Required.
	Name string `yaml:"name"`

	// Description is human-prose, shown in `axon --list-agents` and logs.
	Description string `yaml:"description,omitempty"`

	// SystemPrompt is a path (absolute, or relative to the config file) to
	// a .md/.txt file containing the agent's role prompt. The runtime
	// appends the built-in tool catalog and project orientation
	// automatically — the user's prompt should describe behavior, not
	// re-document the tools.
	SystemPrompt string `yaml:"system_prompt,omitempty"`

	// SystemPromptInline is an inline alternative to SystemPrompt. If both
	// are set, SystemPrompt (file) wins.
	SystemPromptInline string `yaml:"system_prompt_inline,omitempty"`

	// DisableBuiltins names built-in tools to omit. By default all are on.
	// Names must match the tool* constants in tools.go (read, write, exec,
	// search, task, bash_output, kill_shell).
	DisableBuiltins []string `yaml:"disable_builtins,omitempty"`

	// Tools is the list of custom tools available to this agent.
	Tools []ToolConfig `yaml:"tools,omitempty"`

	// sourcePath is set by the loader and used to resolve relative paths
	// (system_prompt, command working dirs, etc.). Not serialized.
	sourcePath string `yaml:"-"`
}

// ToolConfig is one custom tool definition.
type ToolConfig struct {
	// Name as exposed to the model. Must not collide with built-in names.
	Name string `yaml:"name"`

	// Type discriminates the implementation. Today only "shell". Reserved:
	// "mcp" for stdio-based JSON-RPC plugin tools.
	Type string `yaml:"type"`

	// Description shown to the model in the tool catalog.
	Description string `yaml:"description"`

	// Schema is a JSON Schema object (as a map) describing the argument
	// shape. The runtime forwards it to the LLM provider unchanged.
	Schema map[string]any `yaml:"schema"`

	// --- shell-type fields ---

	// Command is a Go text/template rendered against the args at call
	// time. Use {{.field}} to interpolate; pipe through `shellQuote` for
	// any value that becomes a shell word.
	Command string `yaml:"command,omitempty"`

	// Cwd is the working directory for the spawned process. Templated the
	// same way as Command. Empty = inherit axon's cwd.
	Cwd string `yaml:"cwd,omitempty"`

	// TimeoutSeconds caps execution. 0 → 60s default.
	TimeoutSeconds int `yaml:"timeout_seconds,omitempty"`
}

// AgentsDir returns the directory where agent YAML files live. Override via
// AXON_AGENTS_DIR; default is $XDG_CONFIG_HOME/axon/agents (or
// ~/.config/axon/agents).
func AgentsDir() string {
	if d := os.Getenv("AXON_AGENTS_DIR"); d != "" {
		return d
	}
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "axon", "agents")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "axon-agents"
	}
	return filepath.Join(home, ".config", "axon", "agents")
}

// LoadAgentConfig reads <name>.yaml from AgentsDir() and validates it. If
// name is "" or "default", a zero AgentConfig is returned — callers treat
// that as "use the built-in default prompt and full built-in tool set".
func LoadAgentConfig(name string) (*AgentConfig, error) {
	if name == "" || name == "default" {
		return &AgentConfig{Name: "default"}, nil
	}
	path := filepath.Join(AgentsDir(), name+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("agent %q not found: %s does not exist", name, path)
		}
		return nil, fmt.Errorf("read agent %q: %w", name, err)
	}
	var cfg AgentConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse agent %q: %w", name, err)
	}
	cfg.sourcePath = path
	if cfg.Name == "" {
		cfg.Name = name
	}
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("agent %q: %w", name, err)
	}
	return &cfg, nil
}

// validate enforces tool-name uniqueness, non-collision with built-ins, and
// per-type required fields. Run by the loader; callers don't need to invoke.
func (c *AgentConfig) validate() error {
	builtins := map[string]bool{
		toolRead: true, toolWrite: true, toolExec: true, toolSearch: true,
		toolTask: true, toolBashOutput: true, toolKillShell: true,
	}
	for _, b := range c.DisableBuiltins {
		if !builtins[b] {
			return fmt.Errorf("disable_builtins: unknown built-in tool %q", b)
		}
	}
	seen := map[string]bool{}
	for i, t := range c.Tools {
		if t.Name == "" {
			return fmt.Errorf("tools[%d]: name is required", i)
		}
		if builtins[t.Name] {
			return fmt.Errorf("tools[%d] %q: collides with a built-in tool name", i, t.Name)
		}
		if seen[t.Name] {
			return fmt.Errorf("tools[%d] %q: duplicate tool name", i, t.Name)
		}
		seen[t.Name] = true
		if t.Description == "" {
			return fmt.Errorf("tools[%d] %q: description is required", i, t.Name)
		}
		switch t.Type {
		case "shell":
			if strings.TrimSpace(t.Command) == "" {
				return fmt.Errorf("tools[%d] %q: shell tools require a command", i, t.Name)
			}
		case "mcp":
			return fmt.Errorf("tools[%d] %q: type=mcp is reserved but not yet implemented", i, t.Name)
		default:
			return fmt.Errorf("tools[%d] %q: unknown type %q (expected: shell)", i, t.Name, t.Type)
		}
	}
	return nil
}

// LoadSystemPrompt resolves SystemPrompt (file) or SystemPromptInline. The
// path is resolved relative to the config file's directory. Returns "" if
// the agent declares no custom prompt — callers fall back to the built-in
// default.
func (c *AgentConfig) LoadSystemPrompt() (string, error) {
	if c.SystemPrompt == "" {
		return c.SystemPromptInline, nil
	}
	path := c.SystemPrompt
	if !filepath.IsAbs(path) && c.sourcePath != "" {
		path = filepath.Join(filepath.Dir(c.sourcePath), path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read system_prompt %s: %w", path, err)
	}
	return string(data), nil
}

// DisabledBuiltin reports whether the agent has disabled the named built-in.
func (c *AgentConfig) DisabledBuiltin(name string) bool {
	for _, b := range c.DisableBuiltins {
		if b == name {
			return true
		}
	}
	return false
}
