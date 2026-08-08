package axon

import (
	"context"
	"errors"
)

// api.go — public library API for embedding axon in Go programs.
// Monolithic engine: no configuration of its own, embedder provides model and system prompt.
// Built-in tools: read, write, exec, search, task, bash_output, kill_shell (exclude via Config.ExcludeBuiltins).
// Concurrency: Step/Run/Reset must not be called concurrently; Interrupt is goroutine-safe.

// OpenAI builds a Model for any OpenAI-compatible endpoint.
func OpenAI(cfg ClientConfig) (Model, error) { return NewClient(cfg) }

// toolSpecs projects tools to their model-visible surface (name, description, schema only).
func toolSpecs(tools []Tool) []ToolSpec {
	specs := make([]ToolSpec, len(tools))
	for i, t := range tools {
		specs[i] = ToolSpec{Name: t.Name, Description: t.Description, Schema: t.Schema}
	}
	return specs
}

// Sentinel errors; wrap with %w, check with errors.Is at boundary.
var (
	ErrNoModel        = errors.New("agent: Config.Model is required")
	ErrNoSystemPrompt = errors.New("agent: Config.SystemPrompt is required")
	ErrToolNotFound   = errors.New("agent: tool not found")
	ErrDuplicateTool  = errors.New("agent: duplicate tool name")
	ErrInvalidTool    = errors.New("agent: invalid tool")
	ErrInterrupted    = errors.New("agent: turn interrupted")
)

// Config is the contract for constructing an Agent. Model and SystemPrompt are required.
type Config struct {
	Model Model

	SystemPrompt string

	Tools []Tool

	ExcludeBuiltins []string

	Pruner Model

	Cwd string

	Session *Session

	OnEvent func(ctx context.Context, e Event)

	MCPServers []MCPServer

	Settings Settings
}

// InputFunc supplies user input to Run. Returns (line, true) per turn, (_, false) when exhausted.
type InputFunc func() (string, bool)

// StepResult summarizes one Step call: final assistant text, tool calls made, turn counter.
type StepResult struct {
	Assistant string
	ToolCalls []ToolCall
	Turn      int
}

// Session returns the agent's current Session.
func (a *Agent) Session() *Session { return a.session }

// SessionPath returns the on-disk path of the current session file.
func (a *Agent) SessionPath() string {
	if a.session == nil {
		return ""
	}
	return a.session.Path()
}
