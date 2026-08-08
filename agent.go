package axon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

type Agent struct {
	model   Model
	tools   []Tool
	session *Session
	pruner  *Pruner

	// Cancels the currently in-flight chat call; set by chat(), cleared after.
	turnCancel atomic.Pointer[context.CancelFunc]

	// Public event sink; nil discards events.
	onEvent func(ctx context.Context, e Event)

	// Agent's role text, used by Reset to rebuild system message.
	systemPrompt string

	// Caller-supplied tools, preserved by Reset across session wipes.
	customTools []Tool

	// Config.ExcludeBuiltins, kept so Reset rebinds the same tool set.
	excludeBuiltins []string

	// Background-process registry; per-agent so Close/Reset don't affect others.
	shells *BackgroundShells

	// Tool limits resolved at construction; let Reset rebind built-ins without re-reading env.
	limits Limits

	// MCP clients managed by this agent.
	mcpClients []*mcpClient
}

// Interrupt cancels the in-flight chat call, or false if no turn is active.
func (a *Agent) Interrupt() bool {
	cf := a.turnCancel.Load()
	if cf == nil {
		return false
	}
	(*cf)()
	return true
}

func (a *Agent) initSessionMessages() {
	a.session.Messages = []Msg{{Role: "system", Content: buildSystemPrompt(a.systemPrompt, a.tools)}}
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
		msg, err := a.model.Complete(ctx, Request{
			Messages: a.session.ContextMessages(),
			Tools:    toolSpecs(tools),
			Stream: Stream{
				Token: func(t string) {
					a.emit(ctx, Event{Kind: KindToken, Text: t})
				},
				Reasoning: func(t string) {
					a.emit(ctx, Event{Kind: KindReasoning, Text: t})
				},
				ToolArgs: func(name, delta string) {
					a.emit(ctx, Event{Kind: KindToolArgDelta, Tool: &ToolEvent{Name: name, ArgsDelta: delta}})
				},
			},
		})

		if err == nil {
			if msg != nil && strings.TrimSpace(msg.Content) == "" && len(msg.ToolCalls) == 0 {
				lastErr = fmt.Errorf("empty response from model")
				a.emit(ctx, Event{Kind: KindError, Err: lastErr})
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

	if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ECONNRESET) {
		return true
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}

	return false
}
