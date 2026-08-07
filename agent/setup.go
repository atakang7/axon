package agent

import (
	"fmt"
)

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

// Close releases resources held by the agent: background shells, file
// handles, etc. Idempotent.
func (a *Agent) Close() error {
	bgReg.killAll()
	return nil
}
