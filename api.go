package axon

import (
	"context"
	"errors"
)

// api.go — public library API.
//
// The surface a Go program imports to embed axon. The terminal coding agent
// bouton is one consumer of this API; HTTP servers, orchestrators, and test
// harnesses are others. The runtime makes no assumptions about who is
// calling — no flags, no signals, no terminal, no os.Exit.
//
// The runtime supplies no configuration of its own. It does not read a
// config file, shell out at startup, or inspect the working directory: an
// embedder decides which model to talk to and what the agent is, and passes
// both in. Everything the runtime adds to the prompt is the tool catalog.
//
// Everything is contained within this single package. There are no nested
// internal modules or complex layering rules. It is a monolithic engine:
// you define the behavior (SystemPrompt), add your tools as plugins,
// and start the agent. It is not a framework for building arbitrary agents;
// it is the agent itself.
//
// Concurrency: an *Agent drives one turn at a time. Step and Run must not be
// called concurrently with each other or with Reset, Undo or Cd. Interrupt is
// the exception — it is safe from any goroutine, and exists to be wired to a
// signal handler. Session() hands back live state that Step mutates; copy what
// you need from it between turns rather than reading it during one. Separate
// Agents share nothing.
//
// Construction:  New(Config) (*Agent, error)
// Drive loop:    (*Agent).Run(ctx, InputFunc) error
// Single step:   (*Agent).Step(ctx, string) (StepResult, error)
// Cancel turn:   (*Agent).Interrupt() bool
// Release:       (*Agent).Close() error
//
// Built-in tools (read, write, exec, search, task, bash_output,
// kill_shell) are unconditional — every agent has them. Custom tools
// supplied via Config.Tools are appended.

// ---------------------------------------------------------------------------
// Vocabulary, re-exported
//
// The types an embedder constructs, implements or inspects are defined in the
// public root package github.com/atakang7/axon, so their fields are
// documented and readable. These aliases mean you can spell them either way —
// axon.Tool and Tool are the same type — and `go doc axon` is where the
// full documentation lives.
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Session layer, re-exported
//
// Owned by internal/session. Aliases again, so a *Session obtained from
// LoadOrCreateSession can be handed straight back through Config.Session.
// ---------------------------------------------------------------------------

// OpenAI builds a Model for any OpenAI-compatible endpoint. This is the
// implementation that ships; Config.Model accepts any other.
func OpenAI(cfg ClientConfig) (Model, error) { return NewClient(cfg) }

// toolSpecs projects tools down to what the model layer is allowed to see:

// name, description, schema — never Fn. This is the one place the execution
// layer crosses into the model layer, and it is an allowlist by construction.
func toolSpecs(tools []Tool) []ToolSpec {
	specs := make([]ToolSpec, len(tools))
	for i, t := range tools {
		specs[i] = ToolSpec{Name: t.Name, Description: t.Description, Schema: t.Schema}
	}
	return specs
}

// Sentinel errors. Wrap with %w when returning from internals; check

// with errors.Is at the boundary.
var (
	ErrNoModel        = errors.New("agent: Config.Model is required")
	ErrNoSystemPrompt = errors.New("agent: Config.SystemPrompt is required")
	ErrToolNotFound   = errors.New("agent: tool not found")
	ErrDuplicateTool  = errors.New("agent: duplicate tool name")
	ErrInvalidTool    = errors.New("agent: invalid tool")
	ErrInterrupted    = errors.New("agent: turn interrupted")
)

// Config is the contract for constructing an Agent. Provider and

// SystemPrompt are required; every other field has a zero-value default.
type Config struct {
	// Model is the LLM this agent talks to. Required.
	//
	//	m, err := OpenAI(OpenAIConfig{Provider: p})
	//
	// Any implementation of the interface works, so an embedder can route
	// through its own gateway, or pass a fake and drive the loop offline.
	Model Model

	// SystemPrompt is the agent's role text — the entire "who am I"

	// answer the runtime sends to the model. Required. The runtime
	// appends the built-in tool catalog and project orientation
	// automatically; the role text should describe behavior, not
	// enumerate tools.
	SystemPrompt string

	// Tools are appended to the built-in tool set. Names must not

	// collide with built-ins (read, write, exec, search, task,
	// bash_output, kill_shell).
	Tools []Tool

	// ExcludeBuiltins names built-in tools to leave out. Empty means the

	// agent gets all seven.
	//
	// This is how you bound what an agent can do. A research agent that must
	// not touch the machine it runs on:
	//
	//	ExcludeBuiltins: []string{"write", "exec", "bash_output", "kill_shell"}
	//
	// leaving read, search and task. Excluding a name that is not a built-in
	// is not an error, so a list can outlive a renamed tool.
	ExcludeBuiltins []string

	// Pruner, when non-nil, parks old messages as the context grows.

	// nil disables pruning.
	Pruner *Pruner

	// Cwd is the working directory the agent operates against. Empty
	// means the current process cwd at New() time.
	Cwd string

	// Session, when non-nil, is reused (e.g. resuming an existing
	// conversation). nil means the runtime loads or creates the default
	// on-disk session for the current working directory.
	//
	// One session belongs to one agent. Two agents sharing a *Session race on
	// its message log; two agents that both default to the same on-disk path
	// each hold their own copy and overwrite the other's history on save.
	// Give concurrent agents their own sessions, and set AXON_SESSION_PATH or
	// construct them explicitly if they share a working directory.
	Session *Session

	// OnEvent receives observability events emitted by the runtime
	// (tokens, tool calls, turn boundaries, prune cycles). nil means
	// events are dropped. Fan out by wrapping multiple sinks inside
	// the closure.
	OnEvent func(ctx context.Context, e Event)
}

// InputFunc supplies user input to Run. Returns (line, true) for each
// turn and (_, false) when input is exhausted, at which point Run
// returns nil. Reading from a terminal, a channel, or an HTTP request
// body all satisfy this contract.
type InputFunc func() (string, bool)

// StepResult summarizes one Step call. Assistant holds the final
// assistant text emitted with no further tool calls; ToolCalls lists
// every tool invocation that happened on the way there, in order; Turn
// is the session turn counter after the step completes.
type StepResult struct {
	Assistant string
	ToolCalls []ToolCall
	Turn      int
}

// Session returns the agent's current Session. Treat it as read-mostly;
// use Reset to wipe and Undo to revert edits.
func (a *Agent) Session() *Session { return a.session }

// SessionPath returns the on-disk path of the current session file.
func (a *Agent) SessionPath() string {
	if a.session == nil {
		return ""
	}
	return a.session.Path()
}
