package axon

import (
	"fmt"
	"strings"
)

// setup.go — construction and lifecycle: New, Reset, Undo, Cd, Close.
//
// Everything an Agent owns is created here and released in Close. Nothing is
// process-global, so two Agents in one process share no mutable state.

// New constructs an Agent. Built-in tools are always present; cfg.Tools
// are appended.
func New(cfg Config) (*Agent, error) {
	if cfg.Model == nil {
		return nil, ErrNoModel
	}
	if cfg.SystemPrompt == "" {
		return nil, ErrNoSystemPrompt
	}

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
	shells := NewBackgroundShells()

	// Caps are resolved once, here, and travel with the agent. No tool reads
	// the environment at call depth, so two agents in one process can be tuned
	// independently and a test can vary a cap without touching os.Environ.
	limits := LoadLimits()

	toolset := builtinTools(sess, shells, limits, cfg.ExcludeBuiltins)
	seen := map[string]bool{}
	for _, t := range toolset {
		seen[t.Name] = true
	}
	for _, t := range cfg.Tools {
		// A tool is checked here rather than where it is called: a nil Fn
		// discovered mid-turn panics inside the embedder, after the model has
		// already committed to the call and the user has already paid for it.
		if missing := missingToolFields(t); missing != "" {
			return nil, fmt.Errorf("%w: tool %q has no %s", ErrInvalidTool, t.Name, missing)
		}
		if seen[t.Name] {
			return nil, fmt.Errorf("%w: %q", ErrDuplicateTool, t.Name)
		}
		seen[t.Name] = true
		toolset = append(toolset, t)
	}

	if len(sess.Messages) == 0 {
		sess.Messages = []Msg{{Role: "system", Content: buildSystemPrompt(cfg.SystemPrompt, toolset)}}
	}

	return &Agent{
		model:           cfg.Model,
		tools:           toolset,
		session:         sess,
		shells:          shells,
		limits:          limits,
		pruner:          cfg.Pruner,
		onEvent:         cfg.OnEvent,
		systemPrompt:    cfg.SystemPrompt,
		customTools:     cfg.Tools,
		excludeBuiltins: cfg.ExcludeBuiltins,
	}, nil
}

// missingToolFields names the first required field a tool leaves unset, or ""
// when it is complete. Schema is required even for a tool that takes no
// arguments (use map[string]any{"type": "object"}) because providers disagree
// about what a null parameter schema means, and the disagreement surfaces as a
// rejected request rather than as a clear error here.
func missingToolFields(t Tool) string {
	switch {
	case strings.TrimSpace(t.Name) == "":
		return "Name"
	case t.Schema == nil:
		return "Schema"
	case t.Fn == nil:
		return "Fn"
	}
	return ""
}

// builtinTools binds the built-in tool set to this agent's capabilities. Each
// tool receives only what it needs: a Workspace for the filesystem tools, the
// shell registry for the process tools, a Plan for the task tool, and the caps
// it must obey. None of them gets the Session itself, and none of them reads
// the environment — this is the single place those decisions are made.
func builtinTools(ws *Session, shells *BackgroundShells, lim Limits, exclude []string) []Tool {
	all := []Tool{
		ReadTool(ws, lim),
		WriteTool(ws),
		ExecTool(ws, shells, lim),
		BashOutputTool(shells, lim),
		KillShellTool(shells),
		SearchTool(ws, lim),
		TaskTool(ws),
	}
	if len(exclude) == 0 {
		return all
	}
	skip := make(map[string]bool, len(exclude))
	for _, name := range exclude {
		skip[name] = true
	}
	kept := all[:0]
	for _, t := range all {
		if !skip[t.Name] {
			kept = append(kept, t)
		}
	}
	return kept
}

// Reset wipes the session and rebuilds the system prompt and tool set.
// Background shells are killed.
func (a *Agent) Reset() {
	a.shells.KillAll()
	a.session.Reset()
	a.initSessionMessages()
	a.tools = append(builtinTools(a.session, a.shells, a.limits, a.excludeBuiltins), a.customTools...)
}

// Undo reverts the last recorded file edit and tells the model it happened.
// Returns the path that was restored and true, or ("", false) if there was
// nothing to undo.
//
// Not safe to call while Step is running.
func (a *Agent) Undo() (string, bool) {
	e, ok := a.session.Undo()
	if !ok {
		return "", false
	}
	if err := WriteFileAtomic(e.Path, []byte(e.Before)); err != nil {
		return "", false
	}
	// The log still holds the write tool-call and its success result, so
	// without this note the model would keep reasoning about contents the file
	// no longer has -- editing around a change that is no longer there, or
	// reporting work it did not keep. The log is append-only, so the revert is
	// recorded rather than erased.
	a.session.Append(Msg{
		Role:    "system",
		Content: fmt.Sprintf("[the edit to %s was reverted; the file is back to its previous contents]", e.Path),
	})
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
