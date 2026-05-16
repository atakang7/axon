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

// agent.go — Agent struct, Run loop, chat/tool dispatch.

type Agent struct {
	client  *Client
	tools   []Tool
	session *Session
	input   func() (string, bool)
	pruner  *Pruner

	// lastPruneTokens is the projected context size at the previous successful
	// pruner fire. Pruner.ShouldFire compares the current size against this to
	// avoid re-firing on small growth.
	lastPruneTokens int

	// turnCancel, when non-nil, cancels the currently in-flight chat call.
	// Set by Run() at the start of each chat(), cleared after. The signal
	// handler in main() reads this to implement "Ctrl-C interrupts a turn,
	// stays in REPL"; if nil at SIGINT time, the handler exits the process.
	turnCancel atomic.Pointer[context.CancelFunc]

	// logger is optional; when non-nil, the agent emits structured JSONL events
	// for non-interactive observation (e.g. benchmarking). Kept during the
	// transition to the public Handler interface; commit that finalises
	// observability will remove it.
	logger *jsonlLogger

	// handler is the public event sink. When non-nil, the runtime emits
	// structured Events at every interesting moment (tokens, tool calls,
	// turn boundaries, prune cycles). nil means events are discarded.
	handler Handler

	// cfg is the agent personality + custom tooling, loaded once at startup.
	// nil means "built-in default agent". Used by /reset to rebuild tools
	// against the same config after the session is wiped.
	cfg *AgentConfig
}

// InterruptTurn cancels the in-flight chat if there is one, returning true.
// Returns false when no turn is active (caller can decide to exit instead).
func (a *Agent) InterruptTurn() bool {
	cf := a.turnCancel.Load()
	if cf == nil {
		return false
	}
	(*cf)()
	return true
}

func (a *Agent) initSessionMessages() {
	a.session.Messages = []Msg{{Role: "system", Content: buildSystemPrompt(a.session, a.cfg)}}
}

func (a *Agent) Run(ctx context.Context) error {
	if len(a.session.Messages) == 0 {
		a.initSessionMessages()
	}
	readInput := true
	for {
		if readInput {
			uiPrompt()
			line, ok := a.input()
			if !ok {
				return nil
			}
			uiAfterInput()
			if a.handleSlash(strings.TrimSpace(line)) {
				continue
			}
			a.session.Turn++
			a.session.Append(Msg{Role: "user", Content: line})
			a.session.Save()
			a.logger.Emit("user", map[string]any{"turn": a.session.Turn, "text": line})
			a.emit(ctx, Event{Kind: KindUserInput, Text: line})
		}

		if a.pruner != nil && a.pruner.ShouldFire(a.session, a.lastPruneTokens) {
			uiInfo("pruning context...")
			before := a.lastPruneTokens
			a.emit(ctx, Event{Kind: KindPruneStart, Prune: &PruneInfo{Before: before}})
			next, err := a.pruner.Prune(ctx, a.session, a.lastPruneTokens)
			if err != nil {
				uiError(fmt.Errorf("prune skipped: %w", err))
				a.emit(ctx, Event{Kind: KindError, Err: err, Text: "prune skipped"})
			} else {
				a.lastPruneTokens = next
				a.emit(ctx, Event{Kind: KindPruneEnd, Prune: &PruneInfo{Before: before, After: next}})
			}
		}

		turnCtx, turnCancel := context.WithCancel(ctx)
		cf := context.CancelFunc(turnCancel)
		a.turnCancel.Store(&cf)
		msg, err := a.chat(turnCtx, a.tools)
		if err != nil {
			a.turnCancel.Store(nil)
			turnCancel()
			uiError(err)
			readInput = true
			continue
		}
		a.session.Append(*msg)
		a.session.Save()
		if msg.Content != "" {
			a.logger.Emit("assistant_text", map[string]any{"turn": a.session.Turn, "text": msg.Content})
			a.emit(ctx, Event{Kind: KindAssistantEnd, Text: msg.Content})
		}
		for _, tc := range msg.ToolCalls {
			a.logger.Emit("tool_call", map[string]any{
				"turn": a.session.Turn,
				"id":   tc.ID,
				"name": tc.Function.Name,
				"args": tc.Function.Arguments,
			})
			a.emit(ctx, Event{Kind: KindToolCall, Tool: &ToolEvent{
				ID:   tc.ID,
				Name: tc.Function.Name,
				Args: json.RawMessage(tc.Function.Arguments),
			}})
		}

		if len(msg.ToolCalls) == 0 {
			a.turnCancel.Store(nil)
			turnCancel()
			a.logger.Emit("turn_end", map[string]any{"turn": a.session.Turn})
			a.emit(ctx, Event{Kind: KindTurnEnd})
			readInput = true
			continue
		}

		readInput = false

		// Keep turnCtx live across tool execution so Ctrl-C cancels the
		// running tool (foreground exec, search) instead of just the chat
		// stream that already returned.
		for _, tc := range msg.ToolCalls {
			result := a.runTool(turnCtx, tc)
			a.session.Append(result)
			a.logger.Emit("tool_result", map[string]any{
				"turn":    a.session.Turn,
				"id":      tc.ID,
				"name":    tc.Function.Name,
				"content": result.Content,
			})
			// Stamp the assigned block ID into the content so the LLM can see
			// its own handle (#mN) and act on it immediately with park/forget.
			blockID := a.session.Messages[len(a.session.Messages)-1].ID
			if blockID != "" {
				a.session.Messages[len(a.session.Messages)-1].Content = "[#" + blockID + "]\n" + result.Content
			}
			a.session.Save()
			a.emit(ctx, Event{Kind: KindToolResult, Tool: &ToolEvent{
				ID: tc.ID, Name: tc.Function.Name, Result: result.Content, BlockID: blockID,
			}})
		}
		a.turnCancel.Store(nil)
		turnCancel()

		// Every tool batch keeps the loop running — its result needs reasoning.
		// The user only speaks again when the assistant emits content with no
		// tool calls (handled above).
	}
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
			uiInfo(fmt.Sprintf("retry %d/%d in %ds", attempt+1, maxAttempts, backoff))
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(backoff) * time.Second):
			}
		}
		tokens := make(chan string, 4096)
		done := make(chan struct{})
		go func() {
			defer close(done)
			started := false
			for t := range tokens {
				if !started {
					started = true
					uiStartResponse()
				}
				uiToken(t)
			}
			if started {
				uiResponse()
			}
		}()
		uiInfo("calling API...")
		stop := uiSpinner()
		first := true
		stopOnFirst := func() {
			if first {
				first = false
				stop()
			}
		}
		msg, err := a.client.ChatStream(ctx, a.session.ContextMessages(), tools,
			func(t string) {
				stopOnFirst()
				tokens <- t
				a.emit(ctx, Event{Kind: KindToken, Text: t})
			},
			func(t string) {
				stopOnFirst()
				uiReasoning(t)
				a.emit(ctx, Event{Kind: KindReasoning, Text: t})
			},
			func(name, delta string) {
				stopOnFirst()
				uiToolArgDelta(name, delta)
				a.emit(ctx, Event{Kind: KindToolArgDelta, Tool: &ToolEvent{Name: name, ArgsDelta: delta}})
			},
			nil,
			func(phase string, duration time.Duration) {
				// Phase callback for tracking API phases
			})
		stop()
		close(tokens)
		<-done
		if err == nil {
			if msg != nil && strings.TrimSpace(msg.Content) == "" && len(msg.ToolCalls) == 0 {
				lastErr = fmt.Errorf("empty response from model")
				uiError(lastErr)
				// Reasoning models (DeepSeek etc.) sometimes emit only thinking tokens
				// and no actual content or tool calls. Retrying the identical context
				// will produce the same result — fail fast after one retry rather than
				// burning all attempts.
				if attempt >= 1 {
					return nil, lastErr
				}
				continue
			}
			return msg, nil
		}
		uiError(err)
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
		uiTool(tc.Function.Name, input)
		out, err := t.Fn(ctx, input)
		if err != nil {
			uiToolError(err)
			a.emit(ctx, Event{Kind: KindToolError, Tool: &ToolEvent{ID: tc.ID, Name: tc.Function.Name}, Err: err})
			return Msg{Role: "tool", ToolCallID: tc.ID, ToolName: tc.Function.Name, Content: err.Error()}
		}
		uiToolResult(out)
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
