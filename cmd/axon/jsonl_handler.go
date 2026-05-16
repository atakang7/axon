package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"sync"
	"time"

	"github.com/atakang7/axon/agent"
)

// jsonlLogger writes one JSON object per line.
type jsonlLogger struct {
	mu sync.Mutex
	w  io.WriteCloser
}

func newJSONLLogger(path string) (*jsonlLogger, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	return &jsonlLogger{w: f}, nil
}

func (l *jsonlLogger) Emit(kind string, fields map[string]any) {
	if l == nil {
		return
	}
	if fields == nil {
		fields = map[string]any{}
	}
	fields["ts"] = time.Now().UTC().Format(time.RFC3339Nano)
	fields["kind"] = kind
	l.mu.Lock()
	defer l.mu.Unlock()
	enc := json.NewEncoder(l.w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(fields)
}

func (l *jsonlLogger) Close() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_ = l.w.Close()
}

// jsonlHandler adapts jsonlLogger to the agent.Handler interface so the
// CLI can install it as one of MultiHandler's sinks.
type jsonlHandler struct{ l *jsonlLogger }

func newJSONLHandler(path string) (*jsonlHandler, error) {
	l, err := newJSONLLogger(path)
	if err != nil {
		return nil, err
	}
	return &jsonlHandler{l: l}, nil
}

func (h *jsonlHandler) Handle(_ context.Context, e agent.Event) {
	if h == nil || h.l == nil {
		return
	}
	fields := map[string]any{"turn": e.Turn}
	kind := ""
	switch e.Kind {
	case agent.KindUserInput:
		kind = "user"
		fields["text"] = e.Text
	case agent.KindAssistantEnd:
		kind = "assistant_text"
		fields["text"] = e.Text
	case agent.KindToolCall:
		kind = "tool_call"
		if e.Tool != nil {
			fields["id"] = e.Tool.ID
			fields["name"] = e.Tool.Name
			fields["args"] = string(e.Tool.Args)
		}
	case agent.KindToolResult:
		kind = "tool_result"
		if e.Tool != nil {
			fields["id"] = e.Tool.ID
			fields["name"] = e.Tool.Name
			fields["content"] = e.Tool.Result
		}
	case agent.KindToolError:
		kind = "tool_error"
		if e.Tool != nil {
			fields["name"] = e.Tool.Name
		}
		if e.Err != nil {
			fields["error"] = e.Err.Error()
		}
	case agent.KindTurnEnd:
		kind = "turn_end"
	case agent.KindPruneStart:
		kind = "prune_start"
	case agent.KindPruneEnd:
		kind = "prune_end"
		if e.Prune != nil {
			fields["before"] = e.Prune.Before
			fields["after"] = e.Prune.After
		}
	case agent.KindError:
		kind = "error"
		if e.Err != nil {
			fields["error"] = e.Err.Error()
		}
	default:
		return
	}
	h.l.Emit(kind, fields)
}

func (h *jsonlHandler) Close() { h.l.Close() }
