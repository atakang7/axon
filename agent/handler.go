package agent

import (
	"context"
	"encoding/json"
	"time"
)

// handler.go — observability surface.
//
// Modeled on slog.Handler: a single-method interface the runtime calls
// at every interesting moment. Embedders write their own Handler to
// wire events to a TTY, JSONL log, metrics sink, HTTP stream, etc.
// HandlerFunc adapts a plain function to the interface. MultiHandler
// fans out to several handlers.

// Handler receives events emitted by the runtime. Implementations must
// be safe for concurrent calls — the runtime may emit from multiple
// goroutines (e.g. streaming tokens and a spinner ticker).
type Handler interface {
	Handle(ctx context.Context, e Event)
}

// HandlerFunc adapts an ordinary function to the Handler interface.
type HandlerFunc func(ctx context.Context, e Event)

// Handle implements Handler.
func (f HandlerFunc) Handle(ctx context.Context, e Event) { f(ctx, e) }

// MultiHandler returns a Handler that fans out to each h in order. Nil
// entries are skipped. Returns DiscardHandler when no handlers are given.
func MultiHandler(hs ...Handler) Handler {
	live := hs[:0]
	for _, h := range hs {
		if h != nil {
			live = append(live, h)
		}
	}
	if len(live) == 0 {
		return DiscardHandler
	}
	return multiHandler(live)
}

type multiHandler []Handler

func (m multiHandler) Handle(ctx context.Context, e Event) {
	for _, h := range m {
		h.Handle(ctx, e)
	}
}

// DiscardHandler drops every event. Useful as a default and in tests.
var DiscardHandler Handler = discardHandler{}

type discardHandler struct{}

func (discardHandler) Handle(context.Context, Event) {}

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
	KindAssistantEnd // final assistant text for this turn (no more tool calls)
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
//
// Tagged struct (not interface-per-kind) so consumers can switch
// cleanly and new kinds can be added without breaking signatures.
type Event struct {
	Kind Kind
	Turn int
	Time time.Time

	// Text payload — used by Token, Reasoning, AssistantEnd, UserInput,
	// Info, Error (error message text).
	Text string

	// Tool payload — used by ToolArgDelta, ToolCall, ToolResult, ToolError.
	Tool *ToolEvent

	// Prune payload — used by PruneStart, PruneEnd.
	Prune *PruneInfo

	// Err — used by Error and ToolError.
	Err error

	// Session payload — used by SessionStart, SessionEnd.
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

// emit is the runtime's internal helper for pushing events. Cheap
// when the handler is nil or DiscardHandler.
func (a *Agent) emit(ctx context.Context, e Event) {
	if a == nil || a.handler == nil {
		return
	}
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	if e.Turn == 0 && a.session != nil {
		e.Turn = a.session.Turn
	}
	a.handler.Handle(ctx, e)
}
