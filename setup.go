package axon

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// New constructs an Agent. Built-in tools are always present; cfg.Tools
// are appended.
func New(cfg Config) (*Agent, error) {
	if cfg.Model == nil {
		return nil, ErrNoModel
	}
	if cfg.SystemPrompt == "" {
		return nil, ErrNoSystemPrompt
	}

	// Defaults are applied once, here, and the result is what every part of
	// this agent uses from now on. Nothing below re-reads configuration, so an
	// agent's behaviour is fixed at construction and two agents in one process
	// can differ.
	settings := cfg.Settings.WithDefaults()

	sess := cfg.Session
	if sess == nil {
		sess = LoadOrCreateSessionAt(settings.Session.SessionFile())
	}
	if cfg.Cwd != "" {
		if err := sess.SetCwd(cfg.Cwd); err != nil {
			return nil, fmt.Errorf("agent: set cwd: %w", err)
		}
	}

	shells := NewBackgroundShellsAt(settings.Session.BackgroundLogDir(os.Getpid()))
	limits := settings.Tools.limits()

	var mcpClients []*mcpClient
	for _, mc := range cfg.MCPServers {
		client, mcpTools, err := startMCP(mc)
		if err != nil {
			for _, c := range mcpClients {
				c.Close()
			}
			return nil, fmt.Errorf("agent: mcp start %q: %w", mc.Command, err)
		}
		mcpClients = append(mcpClients, client)
		cfg.Tools = append(cfg.Tools, mcpTools...)
	}

	a := &Agent{
		model:           cfg.Model,
		session:         sess,
		shells:          shells,
		limits:          limits,
		prunerModel:     cfg.Pruner,
		explorer:        cfg.Explorer,
		onEvent:         cfg.OnEvent,
		systemPrompt:    cfg.SystemPrompt,
		excludeBuiltins: cfg.ExcludeBuiltins,
		mcpClients:      mcpClients,
		settings:        settings,
		titleModel:      cfg.TitleModel,
		maxIterations:   cfg.MaxIterations,
	}
	toolset := a.ownTools()
	seen := map[string]bool{}
	for _, t := range toolset {
		seen[t.Name] = true
	}

	for _, t := range cfg.Tools {
		if missing := missingToolFields(t); missing != "" {
			closeMCP(mcpClients)
			return nil, fmt.Errorf("%w: tool %q has no %s", ErrInvalidTool, t.Name, missing)
		}
		if seen[t.Name] {
			closeMCP(mcpClients)
			return nil, fmt.Errorf("%w: %q", ErrDuplicateTool, t.Name)
		}
		seen[t.Name] = true
		toolset = append(toolset, t)
	}
	a.customTools = cfg.Tools
	a.tools = toolset

	if len(sess.Messages) == 0 {
		sess.Messages = []Msg{{Role: "system", Content: buildSystemPrompt(cfg.SystemPrompt, toolset)}}
	}

	// Model and Provider are left blank: Model is an interface an embedder can
	// implement with anything, so the runtime has no generic way to ask one
	// what it is. The embedder already knows — cortex, for instance, names it
	// in its own config — and can log that alongside this event itself.
	a.emit(context.Background(), Event{Kind: KindSessionStart, Session: &SessionInfo{
		ID:       sess.ID,
		Cwd:      sess.Cwd,
		Path:     sess.Path(),
		PrunerOn: true,
	}})

	return a, nil
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

func closeMCP(clients []*mcpClient) {
	for _, c := range clients {
		c.Close()
	}
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
		KillShellTool(shells, lim),
		SearchTool(ws, lim),
		TaskTool(ws),
		GoalTool(ws),
		RecallTool(ws),
	}
	return withoutExcluded(all, exclude)
}

func withoutExcluded(tools []Tool, exclude []string) []Tool {
	if len(exclude) == 0 {
		return tools
	}

	skip := make(map[string]bool, len(exclude))
	for _, name := range exclude {
		skip[name] = true
	}

	kept := tools[:0]
	for _, t := range tools {
		if !skip[t.Name] {
			kept = append(kept, t)
		}
	}

	return kept
}

// ownTools is every tool the runtime provides: the built-ins, plus checkpoint
// and, when an explorer model is set, explore. Those two are bound to the
// agent rather than the session, because they act on its context and models.
func (a *Agent) ownTools() []Tool {
	tools := builtinTools(a.session, a.shells, a.limits, nil)
	tools = append(tools, a.checkpointTool())
	if a.explorer != nil {
		tools = append(tools, a.exploreTool())
	}
	return withoutExcluded(tools, a.excludeBuiltins)
}

// rebindTools rebuilds the tool list after the session or the explorer
// changed. Custom tools were validated by New and are appended as they are.
func (a *Agent) rebindTools() {
	a.tools = append(a.ownTools(), a.customTools...)
}

// Reset wipes the session and rebuilds the system prompt and tool set.
// Background shells are killed.
func (a *Agent) Reset() {
	a.shells.KillAll()

	a.session.Reset()
	a.keptTokens = 0

	a.initSessionMessages()

	a.rebindTools()
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
	a.emit(context.Background(), Event{Kind: KindSessionEnd, Session: &SessionInfo{
		ID:       a.session.ID,
		Cwd:      a.session.Cwd,
		Path:     a.session.Path(),
		PrunerOn: true,
	}})

	a.shells.KillAll()
	for _, c := range a.mcpClients {
		c.Close()
	}

	return nil
}
