package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"time"
)

// agent.go — Agent struct, chat/tool dispatch, retry policy.
//
// The public driving loops (Step, Run) live in api.go. This file owns
// the internal mechanics: streaming a chat completion, retrying
// retryable errors, executing a single tool call.

type Agent struct {
	client  *Client
	tools   []Tool
	session *Session
	pruner  *Pruner

	// lastPruneTokens is the projected context size at the previous successful
	// pruner fire. Pruner.ShouldFire compares the current size against this to
	// avoid re-firing on small growth.
	lastPruneTokens int

	// turnCancel, when non-nil, cancels the currently in-flight chat call.
	// Set at the start of each chat(), cleared after. Embedders call
	// Interrupt() to fire it (e.g. from a SIGINT handler).
	turnCancel atomic.Pointer[context.CancelFunc]

	// handler is the public event sink. nil means events are discarded.
	handler Handler

	// systemPrompt is the agent's role text, captured at construction so
	// Reset can rebuild the system message after the session is wiped.
	systemPrompt string

	// disabledBuiltins names built-ins suppressed from the tool catalog.
	// Used by NewBare-style callers; nil for New() agents.
	disabledBuiltins map[string]bool

	// withBuiltins records whether the agent was built with New (true) or
	// NewBare (false). Reset uses this to decide whether to re-bind the
	// session-aware built-ins after a session wipe.
	withBuiltins bool

	// customTools holds the caller-supplied tools (Config.Tools). Reset
	// preserves these across session wipes; only built-ins are rebound.
	customTools []Tool
}

// Interrupt cancels the in-flight chat if there is one, returning true.
// Returns false when no turn is active.
func (a *Agent) Interrupt() bool {
	cf := a.turnCancel.Load()
	if cf == nil {
		return false
	}
	(*cf)()
	return true
}

func (a *Agent) initSessionMessages() {
	a.session.Messages = []Msg{{Role: "system", Content: buildSystemPrompt(a.session, a.systemPrompt, a.disabledBuiltins)}}
}

func (a *Agent) chat(ctx context.Context, tools []Tool) (*Msg, error) {
	const maxAttempts = 10
	var lastErr error
	for attempt := range maxAttempts {
		if attempt > 0 {
			backoff := 1 << attempt
			if backoff > 60 {
				backoff = 60
			}
			a.emit(ctx, Event{Kind: KindInfo, Text: fmt.Sprintf("retry %d/%d in %ds", attempt+1, maxAttempts, backoff)})
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(backoff) * time.Second):
			}
		}
		a.emit(ctx, Event{Kind: KindAPICall})
		msg, err := a.client.ChatStream(ctx, a.session.ContextMessages(), tools,
			func(t string) {
				a.emit(ctx, Event{Kind: KindToken, Text: t})
			},
			func(t string) {
				a.emit(ctx, Event{Kind: KindReasoning, Text: t})
			},
			func(name, delta string) {
				a.emit(ctx, Event{Kind: KindToolArgDelta, Tool: &ToolEvent{Name: name, ArgsDelta: delta}})
			},
			nil,
			func(phase string, duration time.Duration) {
				// Phase callback for tracking API phases.
			})
		if err == nil {
			if msg != nil && strings.TrimSpace(msg.Content) == "" && len(msg.ToolCalls) == 0 {
				lastErr = fmt.Errorf("empty response from model")
				a.emit(ctx, Event{Kind: KindError, Err: lastErr})
				// Reasoning models sometimes emit only thinking tokens.
				// Retry once, then fail.
				if attempt >= 1 {
					return nil, lastErr
				}
				continue
			}
			return msg, nil
		}
		a.emit(ctx, Event{Kind: KindError, Err: err})
		lastErr = err
		if !retryable(err) {
			return nil, err
		}
	}
	return nil, lastErr
}

func (a *Agent) runTool(ctx context.Context, tc ToolCall) Msg {
	for _, t := range a.tools {
		if t.Name != tc.Function.Name {
			continue
		}
		input := json.RawMessage(tc.Function.Arguments)
		out, err := t.Fn(ctx, input)
		if err != nil {
			a.emit(ctx, Event{Kind: KindToolError, Tool: &ToolEvent{ID: tc.ID, Name: tc.Function.Name}, Err: err})
			return Msg{Role: "tool", ToolCallID: tc.ID, ToolName: tc.Function.Name, Content: err.Error()}
		}
		return Msg{Role: "tool", ToolCallID: tc.ID, ToolName: tc.Function.Name, Content: out}
	}
	return Msg{Role: "tool", ToolCallID: tc.ID, ToolName: tc.Function.Name, Content: "tool not found"}
}

func retryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var nerr net.Error
	if errors.As(err, &nerr) && nerr.Timeout() {
		return true
	}
	m := err.Error()
	for _, code := range []string{"API error 429", "API error 500", "API error 502", "API error 503", "API error 504"} {
		if strings.Contains(m, code) {
			return true
		}
	}
	for _, s := range []string{"connection reset", "connection refused", "no such host"} {
		if strings.Contains(m, s) {
			return true
		}
	}
	return false
}
