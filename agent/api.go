package agent

import (
	"context"
	"errors"

	"github.com/atakang7/axon/internal/llm"
	"github.com/atakang7/axon/internal/session"
)

// api.go — public library API.
//
// The surface a Go program imports to embed axon. The reference CLI in
// cmd/axon is one consumer of this API; HTTP servers, orchestrators,
// and test harnesses are others. The runtime makes no assumptions about
// who is calling — no flags, no signals, no terminal, no os.Exit.
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
// Model layer, re-exported
//
// Provider, Msg, ToolCall and Client are owned by internal/llm. These are
// aliases, not wrappers: agent.Msg and llm.Msg are the same type, so a Session
// marshals identically and an embedder can pass values across either name.
// Re-exporting here keeps internal/llm unreachable from outside the module
// while leaving the public API exactly as it was.
// ---------------------------------------------------------------------------

type (
	// Provider selects the LLM endpoint: base URL, model, credentials.
	Provider = llm.Provider
	// Msg is one entry in the conversation log.
	Msg = llm.Msg
	// ToolCall is a model's request to invoke one tool.
	ToolCall = llm.ToolCall
	// Client is the OpenAI-compatible streaming chat client.
	Client = llm.Client
	// ToolSpec is a tool as the model sees it — name, description, schema,
	// no implementation. Client methods take these; use toolSpecs to project
	// a []Tool down to them.
	ToolSpec = llm.ToolSpec
)

// ---------------------------------------------------------------------------
// Session layer, re-exported
//
// Owned by internal/session. Aliases again, so a *Session obtained from
// LoadOrCreateSession can be handed straight back through Config.Session.
// ---------------------------------------------------------------------------

type (
	// Session is the conversation log plus working directory, edit history
	// and task plan. Treat it as read-mostly.
	Session = session.Session
	// Task is the agent's registered objective and step plan.
	Task = session.Task
	// TaskStep is one committed step of a Task.
	TaskStep = session.TaskStep
	// Edit is one recorded file mutation, the unit Undo reverts.
	Edit = session.Edit
	// ParkedBlock is the stored original of a message the pruner parked.
	ParkedBlock = session.ParkedBlock
)

// LoadOrCreateSession loads the session for the current working directory,
// or creates a fresh one. A corrupt file is backed up, never silently lost.
func LoadOrCreateSession() *Session { return session.LoadOrCreateSession() }

// ErrAmbiguousProvider signals that several (provider, model) pairs are
// configured and no LLM_PROVIDER selector was given — prompt the user.
var ErrAmbiguousProvider = llm.ErrAmbiguousProvider

// NewClient builds a chat client for the given provider.
func NewClient(p Provider) (*Client, error) { return llm.NewClient(p) }

// LoadProviders reads providers.json. A missing file is not an error.
func LoadProviders() (map[string]Provider, error) { return llm.LoadProviders() }

// ResolveProvider picks one (provider, model) pair from configuration and the
// environment, returning ErrAmbiguousProvider when the choice is not unique.
func ResolveProvider(providers map[string]Provider) (Provider, error) {
	return llm.ResolveProvider(providers)
}

// toolSpecs projects tools down to what the model layer is allowed to see:
// name, description, schema — never Fn. This is the one place the execution
// layer crosses into the model layer, and it is an allowlist by construction.
func toolSpecs(tools []Tool) []llm.ToolSpec {
	specs := make([]llm.ToolSpec, len(tools))
	for i, t := range tools {
		specs[i] = llm.ToolSpec{Name: t.Name, Description: t.Description, Schema: t.Schema}
	}
	return specs
}

// Sentinel errors. Wrap with %w when returning from internals; check
// with errors.Is at the boundary.
var (
	ErrNoProvider     = errors.New("agent: no provider configured")
	ErrNoSystemPrompt = errors.New("agent: Config.SystemPrompt is required")
	ErrToolNotFound   = errors.New("agent: tool not found")
	ErrDuplicateTool  = errors.New("agent: duplicate tool name")
	ErrInterrupted    = errors.New("agent: turn interrupted")
)

// Config is the contract for constructing an Agent. Provider and
// SystemPrompt are required; every other field has a zero-value default.
type Config struct {
	// Provider selects the LLM endpoint. Required.
	Provider Provider

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

	// Pruner, when non-nil, lets the runtime drop or park old messages
	// when context grows. nil disables pruning.
	Pruner *Pruner

	// MaxTokens, when > 0, overrides the default per-request max_tokens cap.
	// Leave zero to use the runtime default. Embedders can lower this for
	// budget-sensitive providers that reject very large caps.
	MaxTokens int

	// ReasoningEffort forwards an OpenRouter/OpenAI-style reasoning effort to
	// providers that support it. Use "none" for fast tool-use runs on models
	// that otherwise spend too long thinking before tool calls.
	ReasoningEffort string

	// ExcludeReasoning asks providers to omit reasoning tokens from responses.
	ExcludeReasoning bool

	// Cwd is the working directory the agent operates against. Empty
	// means the current process cwd at New() time.
	Cwd string

	// Session, when non-nil, is reused (e.g. resuming an existing
	// conversation). nil means the runtime loads or creates the
	// default on-disk session at SessionPath().
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
