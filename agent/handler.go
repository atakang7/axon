package agent

import (
	"context"
	"encoding/json"
	"time"
)

// handler.go — event types.
//
// The runtime emits observability events at every meaningful moment in
// a turn. Embedders consume them by setting Config.OnEvent — a plain
// function field, not an interface. Composition is one line of closure;
// no helpers needed.

// Kind discriminates Event payloads. New kinds may be added in minor
// versions; embedders should treat unknown kinds as no-ops.
type Kind int

const (
	KindUnknown Kind = iota
	KindSessionStart
	KindUserInput
	KindTurnStart
	KindAPICall      // model API request begins
	KindToken        // assistant content token
	KindReasoning    // reasoning/thinking token
	KindAssistantEnd // final assistant text for this turn
	KindToolArgDelta // streaming partial tool-call args
	KindToolCall     // tool call resolved with full args
	KindToolResult   // tool returned successfully
	KindToolError    // tool returned an error
	KindTurnEnd
	KindPruneStart
	KindPruneEnd
	KindInfo
	KindError
	KindSessionEnd
)

// Event is the unit the runtime emits. Only the fields relevant to Kind
// are populated; consumers should switch on Kind and read the matching
// payload field.
type Event struct {
	Kind Kind
	Turn int
	Time time.Time

	// Text — used by Token, Reasoning, AssistantEnd, UserInput, Info,
	// Error.
	Text string

	// Tool — used by ToolArgDelta, ToolCall, ToolResult, ToolError.
	Tool *ToolEvent

	// Prune — used by PruneStart, PruneEnd.
	Prune *PruneInfo

	// Err — used by Error and ToolError.
	Err error

	// Session — used by SessionStart, SessionEnd.
	Session *SessionInfo
}

// ToolEvent carries tool-call payloads. Result is populated only for
// KindToolResult; ArgsDelta only for KindToolArgDelta; everything else
// is filled at KindToolCall time.
type ToolEvent struct {
	ID        string
	Name      string
	Args      json.RawMessage
	ArgsDelta string
	Result    string
	BlockID   string // the #mN handle stamped on the tool message
}

// PruneInfo carries metrics from a pruner run.
type PruneInfo struct {
	Before int
	After  int
}

// SessionInfo carries session-start/end metadata.
type SessionInfo struct {
	ID       string
	Cwd      string
	Provider string
	Model    string
	Path     string
	PrunerOn bool
}

// emit is the runtime's internal helper. Cheap when OnEvent is nil.
func (a *Agent) emit(ctx context.Context, e Event) {
	if a == nil || a.onEvent == nil {
		return
	}
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	if e.Turn == 0 && a.session != nil {
		e.Turn = a.session.Turn
	}
	a.onEvent(ctx, e)
}
