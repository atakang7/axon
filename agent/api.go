package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// New constructs an Agent. Built-in tools are always present; cfg.Tools
// are appended.
func New(cfg Config) (*Agent, error) {
	if cfg.Provider.Name == "" && cfg.Provider.BaseURL == "" && cfg.Provider.Model == "" {
		return nil, ErrNoProvider
	}
	if cfg.SystemPrompt == "" {
		return nil, ErrNoSystemPrompt
	}
	client, err := NewClient(cfg.Provider)
	if err != nil {
		return nil, fmt.Errorf("agent: build client: %w", err)
	}

	session := cfg.Session
	if session == nil {
		session = LoadOrCreateSession()
	}
	if cfg.Cwd != "" {
		if err := session.SetCwd(cfg.Cwd); err != nil {
			return nil, fmt.Errorf("agent: set cwd: %w", err)
		}
	}

	tools := builtinTools(session)
	seen := map[string]bool{}
	for _, t := range tools {
		seen[t.Name] = true
	}
	for _, t := range cfg.Tools {
		if seen[t.Name] {
			return nil, fmt.Errorf("%w: %q", ErrDuplicateTool, t.Name)
		}
		seen[t.Name] = true
		tools = append(tools, t)
	}

	if len(session.Messages) == 0 {
		session.Messages = []Msg{{Role: "system", Content: buildSystemPrompt(session, cfg.SystemPrompt)}}
	}

	return &Agent{
		client:       client,
		tools:        tools,
		session:      session,
		pruner:       cfg.Pruner,
		onEvent:      cfg.OnEvent,
		systemPrompt: cfg.SystemPrompt,
		customTools:  cfg.Tools,
	}, nil
}

func builtinTools(s *Session) []Tool {
	return []Tool{
		ReadTool(s),
		WriteTool(s),
		ExecTool(s),
		BashOutputTool(s),
		KillShellTool(s),
		SearchTool(s),
		TaskTool(s),
	}
}

// Reset wipes the session and rebuilds the system prompt and tool set.
// Background shells are killed.
func (a *Agent) Reset() {
	bgReg.killAll()
	a.session.Reset()
	a.initSessionMessages()
	tools := builtinTools(a.session)
	tools = append(tools, a.customTools...)
	a.tools = tools
}

// Undo reverts the last recorded edit (atomic file write). Returns the
// path that was restored and true, or ("", false) if nothing to undo.
func (a *Agent) Undo() (string, bool) {
	e, ok := a.session.Undo()
	if !ok {
		return "", false
	}
	if err := writeBytesRaw(e.Path, []byte(e.Before)); err != nil {
		return "", false
	}
	_ = a.session.Save()
	return e.Path, true
}

// Cd changes the agent's working directory. Returns the resolved
// absolute path on success.
func (a *Agent) Cd(target string) (string, error) {
	if err := a.session.SetCwd(target); err != nil {
		return "", err
	}
	_ = a.session.Save()
	return a.session.Cwd, nil
}

// Step submits one user message and drives the loop until the
// assistant emits text with no further tool calls.
func (a *Agent) Step(ctx context.Context, userInput string) (StepResult, error) {
	if userInput == "" {
		return StepResult{}, fmt.Errorf("agent: empty input")
	}
	a.session.Turn++
	a.session.Append(Msg{Role: "user", Content: userInput})
	if err := a.session.Save(); err != nil {
		return StepResult{}, fmt.Errorf("agent: save session: %w", err)
	}
	a.emit(ctx, Event{Kind: KindUserInput, Text: userInput})

	var calls []ToolCall
	var assistantText string

	for {
		if a.pruner != nil && a.pruner.ShouldFire(a.session, a.lastPruneTokens) {
			before := a.lastPruneTokens
			a.emit(ctx, Event{Kind: KindPruneStart, Prune: &PruneInfo{Before: before}})
			next, err := a.pruner.Prune(ctx, a.session, a.lastPruneTokens)
			if err == nil {
				a.lastPruneTokens = next
				a.emit(ctx, Event{Kind: KindPruneEnd, Prune: &PruneInfo{Before: before, After: next}})
			} else {
				a.emit(ctx, Event{Kind: KindError, Err: err, Text: "prune skipped"})
			}
		}

		turnCtx, cancel := context.WithCancel(ctx)
		cf := context.CancelFunc(cancel)
		a.turnCancel.Store(&cf)

		msg, err := a.chat(turnCtx, a.tools)
		if err != nil {
			a.turnCancel.Store(nil)
			cancel()
			if errors.Is(err, context.Canceled) {
				return StepResult{Turn: a.session.Turn, ToolCalls: calls}, ErrInterrupted
			}
			return StepResult{Turn: a.session.Turn, ToolCalls: calls}, err
		}
		a.session.Append(*msg)
		a.session.Save()

		if msg.Content != "" {
			assistantText = msg.Content
			a.emit(ctx, Event{Kind: KindAssistantEnd, Text: msg.Content})
		}

		if len(msg.ToolCalls) == 0 {
			a.turnCancel.Store(nil)
			cancel()
			a.emit(ctx, Event{Kind: KindTurnEnd})
			return StepResult{
				Assistant: assistantText,
				ToolCalls: calls,
				Turn:      a.session.Turn,
			}, nil
		}

		for _, tc := range msg.ToolCalls {
			calls = append(calls, tc)
			a.emit(ctx, Event{Kind: KindToolCall, Tool: &ToolEvent{
				ID: tc.ID, Name: tc.Function.Name, Args: json.RawMessage(tc.Function.Arguments),
			}})
			result := a.runTool(turnCtx, tc)
			a.session.Append(result)
			blockID := a.session.Messages[len(a.session.Messages)-1].ID
			if blockID != "" {
				a.session.Messages[len(a.session.Messages)-1].Content = "[#" + blockID + "]\n" + result.Content
			}
			a.session.Save()
			a.emit(ctx, Event{Kind: KindToolResult, Tool: &ToolEvent{
				ID: tc.ID, Name: tc.Function.Name, Result: result.Content, BlockID: blockID,
			}})
		}
		a.turnCancel.Store(nil)
		cancel()
	}
}

// Run drives the loop using input until it returns false or ctx is
// cancelled. Each line becomes one Step.
func (a *Agent) Run(ctx context.Context, input InputFunc) error {
	for {
		line, ok := input()
		if !ok {
			return nil
		}
		if _, err := a.Step(ctx, line); err != nil {
			if errors.Is(err, ErrInterrupted) {
				continue
			}
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
}

// Close releases resources held by the agent: background shells, file
// handles, etc. Idempotent.
func (a *Agent) Close() error {
	bgReg.killAll()
	return nil
}

// Session returns the agent's current Session. Treat it as read-mostly;
// use Reset to wipe and Undo to revert edits.
func (a *Agent) Session() *Session { return a.session }

// SessionPath returns the on-disk path of the current session file.
func (a *Agent) SessionPath() string {
	if a.session == nil {
		return ""
	}
	return a.session.path
}
