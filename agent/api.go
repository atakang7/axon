package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// api.go — public library API.
//
// This is the surface a Go program imports to embed axon. The CLI in
// cmd/axon is one consumer of this API; HTTP servers, orchestrators, and
// test harnesses are others. The runtime makes no assumptions about who
// is calling — no flags, no signals, no terminal, no os.Exit.
//
// Construction:    New(Config) (*Agent, error)
// Drive loop:      (*Agent).Run(ctx, InputFunc) error
// Single step:     (*Agent).Step(ctx, string) (StepResult, error)
// Cancel a turn:   (*Agent).Interrupt() bool
// Release:         (*Agent).Close() error
//
// Built-ins are unconditional in New: every agent has the "hands and
// legs" (read, write, exec, search, task, bash_output, kill_shell).
// Embedders who want a strictly custom toolset use NewBare, which omits
// them entirely. Builtins() exposes the built-in set for manual
// composition.

// Sentinel errors. Wrap with %w when returning from internals; check with
// errors.Is at the boundary.
var (
	ErrNoProvider    = errors.New("agent: no provider configured")
	ErrToolNotFound  = errors.New("agent: tool not found")
	ErrDuplicateTool = errors.New("agent: duplicate tool name")
	ErrInterrupted   = errors.New("agent: turn interrupted")
)

// Config is the contract for constructing an Agent. Provider is the only
// required field; every other field has a sensible zero-value default.
type Config struct {
	// Provider selects the LLM endpoint. Required.
	Provider Provider

	// SystemPrompt is the agent's role text. Empty means the runtime's
	// built-in default prompt is used. The runtime appends the tool
	// catalog and project orientation automatically — your prompt should
	// describe behavior, not enumerate the tools.
	SystemPrompt string

	// Tools are appended to the built-in tool set (read, write, exec,
	// search, task, bash_output, kill_shell). Names must not collide
	// with built-ins. Use NewBare if you want NO built-ins.
	Tools []Tool

	// Pruner, when non-nil, lets the runtime drop or park old messages
	// when context grows. nil disables pruning.
	Pruner *Pruner

	// Cwd is the working directory the agent operates against. Empty
	// means os.Getwd at New() time.
	Cwd string

	// Session, when non-nil, is reused (e.g., resuming an existing
	// conversation). nil means the runtime loads or creates the default
	// on-disk session at sessionPath().
	Session *Session

	// Handler receives observability events emitted by the runtime
	// (tokens, tool calls, turn boundaries, prune cycles). nil means
	// events are dropped. Compose multiple sinks with MultiHandler.
	Handler Handler
}

// InputFunc supplies user input to Run. It returns (line, true) for each
// turn and (_, false) when input is exhausted, at which point Run returns
// nil. Reading from a terminal, a channel, or an HTTP request body all
// satisfy this contract.
type InputFunc func() (string, bool)

// StepResult summarizes one Step call. Assistant holds the final
// assistant text emitted with no further tool calls; ToolCalls lists
// every tool invocation that happened on the way there (in order); Turn
// is the session turn counter after the step completes.
type StepResult struct {
	Assistant string
	ToolCalls []ToolCall
	Turn      int
}

// New constructs an Agent with the runtime's built-in tools plus the
// caller's Tools appended. Most embedders want this.
func New(cfg Config) (*Agent, error) {
	return newAgent(cfg, true)
}

// NewBare constructs an Agent with ONLY the caller's Tools — no
// built-ins. Use this for specialized agents whose capability surface
// must be strictly limited (e.g., a deploy-only agent that should never
// touch the filesystem). The caller is responsible for supplying every
// tool the agent will need.
func NewBare(cfg Config) (*Agent, error) {
	return newAgent(cfg, false)
}

// Builtins returns the runtime's built-in tool set bound to the given
// session. Useful for manual composition with NewBare:
//
//	tools := append(agent.Builtins(sess), myTool)
//	ag, _ := agent.NewBare(agent.Config{... Tools: tools ...})
func Builtins(s *Session) []Tool {
	return builtinTools(s)
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

func newAgent(cfg Config, withBuiltins bool) (*Agent, error) {
	if cfg.Provider.Name == "" && cfg.Provider.BaseURL == "" && cfg.Provider.Model == "" {
		return nil, ErrNoProvider
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

	// Compose the tool list. Built-ins (when enabled) first, then the
	// caller's tools. Reject collisions.
	var tools []Tool
	if withBuiltins {
		tools = builtinTools(session)
	}
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

	disabled := map[string]bool{}
	if !withBuiltins {
		for _, name := range builtinToolNames() {
			disabled[name] = true
		}
	}

	if len(session.Messages) == 0 {
		session.Messages = []Msg{{Role: "system", Content: buildSystemPrompt(session, cfg.SystemPrompt, disabled)}}
	}

	a := &Agent{
		client:           client,
		tools:            tools,
		session:          session,
		pruner:           cfg.Pruner,
		handler:          cfg.Handler,
		systemPrompt:     cfg.SystemPrompt,
		disabledBuiltins: disabled,
		withBuiltins:     withBuiltins,
		customTools:      cfg.Tools,
	}
	return a, nil
}

// Reset wipes the session and rebuilds the system prompt and tool set.
// Equivalent to /new in the CLI. Background shells are killed.
func (a *Agent) Reset() {
	bgReg.killAll()
	a.session.Reset()
	a.initSessionMessages()
	var tools []Tool
	if a.withBuiltins {
		tools = builtinTools(a.session)
	}
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

// Cd changes the agent's working directory. Returns the resolved absolute
// path on success.
func (a *Agent) Cd(target string) (string, error) {
	if err := a.session.SetCwd(target); err != nil {
		return "", err
	}
	_ = a.session.Save()
	return a.session.Cwd, nil
}

// builtinToolNames returns the names of the runtime's built-in tools.
// Used by NewBare's catalog suppression and by the slash-command Reset.
func builtinToolNames() []string {
	return []string{toolRead, toolWrite, toolExec, toolBashOutput, toolKillShell, toolSearch, toolTask}
}

// Step submits one user message and drives the loop until the assistant
// emits a text response with no further tool calls. Returns when the
// turn settles or ctx is cancelled. For a long-lived REPL-style loop,
// use Run instead.
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

// Run drives the loop using the supplied InputFunc until input is
// exhausted (returns false) or ctx is cancelled. Each line of input
// becomes one Step. Errors from Step are returned immediately; callers
// who want to keep going on per-turn errors should drive Step themselves.
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

// Close releases resources held by the agent: background shells spawned
// via the exec tool, file handles, etc. Idempotent.
func (a *Agent) Close() error {
	bgReg.killAll()
	return nil
}

// Session returns the agent's current Session. The returned pointer is
// the live session; mutations affect runtime state. Treat it as
// read-mostly; use Reset to wipe and Undo to revert edits.
func (a *Agent) Session() *Session {
	return a.session
}

// SessionPath returns the on-disk path of the current session file.
// Useful for embedders that want to persist or display where state lives.
func (a *Agent) SessionPath() string {
	if a.session == nil {
		return ""
	}
	return a.session.path
}

// ensure unused-import suppression in early builds (os is referenced
// transitively in cfg defaults that may move; keep this here to stop
// import churn during the transition).
var _ = os.Getwd
