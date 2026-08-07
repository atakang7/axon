package agent

import (
	"fmt"

	"github.com/atakang7/axon/internal/config"
	"github.com/atakang7/axon/internal/tools"
)

// setup.go — construction and lifecycle: New, Reset, Undo, Cd, Close.
//
// Everything an Agent owns is created here and released in Close. Nothing is
// process-global, so two Agents in one process share no mutable state.

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
	client.MaxTokens = cfg.MaxTokens
	client.ReasoningEffort = cfg.ReasoningEffort
	client.ExcludeReasoning = cfg.ExcludeReasoning

	sess := cfg.Session
	if sess == nil {
		sess = LoadOrCreateSession()
	}
	if cfg.Cwd != "" {
		if err := sess.SetCwd(cfg.Cwd); err != nil {
			return nil, fmt.Errorf("agent: set cwd: %w", err)
		}
	}

	// One background-shell registry per Agent, created here and terminated in
	// Close. This used to be a package global, which meant two Agents in one
	// process shared shells and either one's Close killed the other's servers.
	shells := tools.NewBackgroundShells()

	// Caps are resolved once, here, and travel with the agent. No tool reads
	// the environment at call depth, so two agents in one process can be tuned
	// independently and a test can vary a cap without touching os.Environ.
	limits := config.LoadLimits()

	toolset := builtinTools(sess, shells, limits)
	seen := map[string]bool{}
	for _, t := range toolset {
		seen[t.Name] = true
	}
	for _, t := range cfg.Tools {
		if seen[t.Name] {
			return nil, fmt.Errorf("%w: %q", ErrDuplicateTool, t.Name)
		}
		seen[t.Name] = true
		toolset = append(toolset, t)
	}

	if len(sess.Messages) == 0 {
		sess.Messages = []Msg{{Role: "system", Content: buildSystemPrompt(sess, cfg.SystemPrompt)}}
	}

	return &Agent{
		client:       client,
		tools:        toolset,
		session:      sess,
		shells:       shells,
		limits:       limits,
		pruner:       cfg.Pruner,
		onEvent:      cfg.OnEvent,
		systemPrompt: cfg.SystemPrompt,
		customTools:  cfg.Tools,
	}, nil
}

// builtinTools binds the built-in tool set to this agent's capabilities. Each
// tool receives only what it needs: a Workspace for the filesystem tools, the
// shell registry for the process tools, a Plan for the task tool, and the caps
// it must obey. None of them gets the Session itself, and none of them reads
// the environment — this is the single place those decisions are made.
func builtinTools(ws *Session, shells *tools.BackgroundShells, lim config.Limits) []Tool {
	return []Tool{
		tools.ReadTool(ws, lim),
		tools.WriteTool(ws),
		tools.ExecTool(ws, shells, lim),
		tools.BashOutputTool(shells, lim),
		tools.KillShellTool(shells),
		tools.SearchTool(ws, lim),
		tools.TaskTool(ws),
	}
}

// Reset wipes the session and rebuilds the system prompt and tool set.
// Background shells are killed.
func (a *Agent) Reset() {
	a.shells.KillAll()
	a.session.Reset()
	a.initSessionMessages()
	a.tools = append(builtinTools(a.session, a.shells, a.limits), a.customTools...)
}

// Undo reverts the last recorded edit (atomic file write). Returns the
// path that was restored and true, or ("", false) if nothing to undo.
func (a *Agent) Undo() (string, bool) {
	e, ok := a.session.Undo()
	if !ok {
		return "", false
	}
	if err := tools.WriteFileAtomic(e.Path, []byte(e.Before)); err != nil {
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

// Close releases resources held by the agent: background shells, file
// handles, etc. Idempotent.
func (a *Agent) Close() error {
	a.shells.KillAll()
	return nil
}
