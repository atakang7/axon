package axon

import (
	"context"
	"errors"
	"path/filepath"
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
	ErrMaxIterations  = errors.New("agent: max iterations reached")
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

	// MaxIterations bounds how many model calls one Step may make before the
	// loop gives up and returns ErrMaxIterations. Zero means unbounded, which
	// is the right default interactively — a human is watching and can
	// interrupt. Unattended embedders (batch jobs, benchmark harnesses) should
	// always set it: without a bound, a model that keeps calling tools without
	// ever answering will spend the caller's budget until the process is
	// killed by something else.
	MaxIterations int
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
// SessionsDir returns the directory holding this agent's session files, or
// the default location when the agent was constructed without an explicit one.
// Intended for embedders that list sessions for a switcher alongside
// ListSessions.
func (a *Agent) SessionsDir() string {
	return filepath.Join(a.settings.Session.dataDir(), "sessions")
}
