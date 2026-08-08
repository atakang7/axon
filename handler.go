package axon

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

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

// kindNames backs Kind.String. Indexed by Kind so the switch that would
// otherwise drift out of sync with the const block above cannot exist.
var kindNames = [...]string{
	KindUnknown:      "unknown",
	KindSessionStart: "session_start",
	KindUserInput:    "user_input",
	KindTurnStart:    "turn_start",
	KindAPICall:      "api_call",
	KindToken:        "token",
	KindReasoning:    "reasoning",
	KindAssistantEnd: "assistant_end",
	KindToolArgDelta: "tool_arg_delta",
	KindToolCall:     "tool_call",
	KindToolResult:   "tool_result",
	KindToolError:    "tool_error",
	KindTurnEnd:      "turn_end",
	KindPruneStart:   "prune_start",
	KindPruneEnd:     "prune_end",
	KindInfo:         "info",
	KindError:        "error",
	KindSessionEnd:   "session_end",
}

// String names a Kind for logs and traces. An out-of-range value (there
// should never be one) prints as its number rather than panicking.
func (k Kind) String() string {
	if int(k) < 0 || int(k) >= len(kindNames) {
		return fmt.Sprintf("kind(%d)", int(k))
	}
	return kindNames[k]
}

// Event is the unit the runtime emits. Only the fields relevant to Kind
// are populated; consumers should switch on Kind and read the matching
// payload field.
type Event struct {
	Kind Kind
	Turn int
	Time time.Time

	// Text — used by Token, Reasoning, AssistantEnd, UserInput, Info, Error.
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
	BlockID   string
}

// PruneInfo carries metrics from a pruner run.
type PruneInfo struct {
	Before int
	After  int

	// Rejected lists block ids the curator named that could not be parked:
	// protected, already parked, or invented. Not a failure — the pass still
	// parked everything it legitimately could — but a persistent stream of
	// them means the curator is naming ids it was never shown.
	Rejected []string
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
