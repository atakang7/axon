package axon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
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

	// Settings this agent was constructed with, already defaulted.
	settings Settings
}

// SetModel changes the model this agent talks to.
func (a *Agent) SetModel(m Model) {
	a.model = m
}

// SetPrunerModel changes the model the pruner uses. If the pruner was not initialized, it initializes it.
func (a *Agent) SetPrunerModel(m Model) {
	if a.pruner == nil {
		a.pruner = NewPruner(PrunerConfig{
			Model:    m,
			Settings: a.settings.Pruner,
		})
	} else {
		a.pruner.model = m
	}
}

// SetPruneMode changes how aggressively the curator parks, mid-session. An
// invalid mode is ignored rather than applied, so a bad value cannot quietly
// change pruning behaviour; Load rejects one from a config file outright.
func (a *Agent) SetPruneMode(mode PruneMode) {
	if !mode.Valid() {
		return
	}

	a.settings.Pruner.Mode = mode
	if a.pruner != nil {
		a.pruner.mode = mode
	}
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
	policy := a.settings.Retry
	if policy.MaxAttempts < 1 {
		policy = DefaultSettings().Retry
	}

	var lastErr error

	for attempt := range policy.MaxAttempts {
		if attempt > 0 {
			backoff := backoffFor(attempt, policy.BackoffCap.Std())
			a.emit(ctx, Event{Kind: KindInfo, Text: fmt.Sprintf("retry %d/%d in %s", attempt+1, policy.MaxAttempts, backoff)})
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		a.emit(ctx, Event{Kind: KindAPICall})
		msg, err := a.model.Complete(ctx, Request{
			Messages: a.session.ContextMessages(a.settings.Pruner.WindowBlocks),
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
			if reason := unusableReply(msg); reason != "" {
				lastErr = errors.New(reason)
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
		if !a.retryable(err) {
			return nil, err
		}
	}

	return nil, lastErr
}

// toolMarkupMarkers are fragments that appear only inside a model's native
// tool-call syntax. Providers are meant to parse that syntax into the
// tool_calls field and never show it to a client; some routes leak it as
// ordinary content instead.
//
// deepseek emits a DSML block, mistral [TOOL_CALLS], qwen and others a
// <tool_call> element. The list is fragments rather than whole tags because
// the leak is often a fragment — a block's closing tags with the opening ones
// already consumed.
var toolMarkupMarkers = []string{
	"DSML",
	"tool_calls",
	"tool▁calls",
	"tool_call",
	"function_call",
	"TOOL_CALLS",
}

// markupTag matches one angle-bracketed tag, including the full-width
// delimiters deepseek wraps its own in.
var markupTag = regexp.MustCompile(`<[^>]*>`)

// unusableReply names why a reply cannot be treated as an answer, or "" when
// it can. Both cases it catches mean the same thing to the turn loop: there is
// nothing here to act on and nothing here to show the user.
//
// The loop ends a turn when an assistant message carries no tool calls, so a
// provider that drops a tool call on the floor reads as "the model is done".
// Treating that as an answer is how the agent stops mid-task and prints raw
// template tokens as its reply.
func unusableReply(msg *Msg) string {
	if msg == nil {
		return "empty response from model"
	}
	if len(msg.ToolCalls) > 0 {
		return "" // it asked for a tool; whatever else it said does not matter
	}

	content := strings.TrimSpace(msg.Content)
	if content == "" {
		return "empty response from model"
	}
	if isOnlyToolMarkup(content) {
		return "provider returned unparsed tool-call markup instead of tool calls"
	}

	return ""
}

// isOnlyToolMarkup reports whether content is a provider's leaked tool-call
// block and nothing else.
//
// The test is deliberately narrow: the content must name a tool-call marker
// AND consist of nothing but tags. A model that merely writes about tool_calls
// in prose — explaining itself, quoting an error, documenting this very
// behaviour — leaves words behind once the tags are stripped, and keeps its
// answer.
func isOnlyToolMarkup(content string) bool {
	var named bool
	for _, marker := range toolMarkupMarkers {
		if strings.Contains(content, marker) {
			named = true
			break
		}
	}
	if !named {
		return false
	}

	return strings.TrimSpace(markupTag.ReplaceAllString(content, "")) == ""
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

// backoffFor returns how long to wait before the given attempt: exponential,
// ceilinged by the configured cap.
func backoffFor(attempt int, cap time.Duration) time.Duration {
	if cap <= 0 {
		cap = DefaultSettings().Retry.BackoffCap.Std()
	}

	// Shift on a duration rather than a second count so the cap can be any
	// duration, not just a whole number of seconds.
	wait := time.Second << attempt
	if wait <= 0 || wait > cap {
		return cap
	}

	return wait
}

// retryable reports whether a failed request is worth another attempt.
//
// HTTP failures are decided by status code against the configured policy, not
// by matching the text of an error message. That distinction matters: an error
// string is a presentation detail, and a retry loop that depends on one breaks
// silently the moment the wording changes.
//
// Transport failures — a dropped connection, a DNS blip, a truncated stream —
// are always retried and are not configurable, because they say nothing about
// whether the request itself was acceptable.
func (a *Agent) retryable(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	var apiErr *APIError
	if errors.As(err, &apiErr) {
		policy := a.settings.Retry
		if len(policy.OnStatus) == 0 {
			policy = DefaultSettings().Retry
		}

		return policy.Retryable(apiErr.Status)
	}

	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}

	var nerr net.Error
	if errors.As(err, &nerr) && nerr.Timeout() {
		return true
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
