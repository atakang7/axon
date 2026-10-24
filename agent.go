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
	"sync"
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

	// Optional model used to name the session from the first user message.
	// nil means deriveTitle's truncation is used instead.
	titleModel Model

	// Config.MaxIterations; zero means the loop is unbounded.
	maxIterations int

	// One-shot planning note for the next model request, set by the loop when
	// the agent is fetching context one item per round; see gather.go.
	nudge string

	// One-shot note appended as a user message to the next request, for when
	// the last message is the assistant's own answer; see goalTurnNote.
	turnNote string

	// Background notes for finished runs; see run_notes.go.
	notesMu     sync.Mutex
	notes       []runNoteResult
	notesCancel context.CancelFunc
	notesCtx    context.Context
	notesAsked  map[string]bool
	// The enricher; see enricher.go. Guarded by notesMu.
	enriching   bool
	enrichments []enrichResult
}

// SetModel changes the model this agent talks to.
func (a *Agent) SetModel(m Model) {
	a.model = m
}

// SetPrunerModel changes the model the pruner uses. If the pruner was not initialized, it initializes it.
func (a *Agent) SetPrunerModel(m Model) {
	a.pruner.stopCategoryJob()
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
	a.pruner.stopCategoryJob()

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
	sys := Msg{Role: "system", Content: buildSystemPrompt(a.systemPrompt, a.tools)}

	// SwitchSession loads the full conversation from disk before calling
	// here; resetting the slice would throw that history away and the model
	// would see a fresh session. Rebuild the leading system message in place
	// and keep everything after it.
	//
	// Reset() calls here after wiping the session itself, so there is no
	// tail to preserve in that case.
	if len(a.session.Messages) == 0 {
		a.session.Messages = []Msg{sys}
		return
	}
	a.session.Messages[0] = sys
}

func (a *Agent) chat(ctx context.Context, tools []Tool) (*Msg, error) {
	if err := a.ensureContextBudget(ctx, tools); err != nil {
		return nil, err
	}
	policy := a.settings.Retry
	if policy.MaxAttempts < 1 {
		policy = DefaultSettings().Retry
	}

	var lastErr error

	for attempt := range policy.MaxAttempts {
		if attempt > 0 {
			backoff := backoffFor(attempt, policy.BackoffCap.Std())

			// A 429 with a Retry-After hint carries the server's own
			// estimate of how long the congestion will last. When that hint
			// exceeds our computed backoff, trust it — it knows its queue
			// better than we do. When it's shorter, our tripling policy is
			// already waiting longer than the server asked, which is fine:
			// the hint is a minimum, not a cap.
			if server := retryAfterHint(lastErr); server > backoff {
				backoff = server
				if cap := policy.BackoffCap.Std(); cap > 0 && backoff > cap {
					backoff = cap
				}
			}

			a.emit(ctx, Event{Kind: KindInfo, Text: fmt.Sprintf("retry %d/%d in %s", attempt+1, policy.MaxAttempts, backoff)})
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		a.emit(ctx, Event{Kind: KindAPICall})
		msg, err := a.model.Complete(ctx, Request{
			Messages: withTurnNote(withNudge(a.session.ContextMessages(a.settings.Pruner.ContextWindow()), a.nudge), a.turnNote),
			Tools:    toolSpecs(tools),
			Stream: Stream{
				Route: func(model, reason string) {
					a.emit(ctx, Event{Kind: KindInfo, Text: "model route: " + model + " (" + reason + ")"})
				},
				Token: func(t string) {
					a.emit(ctx, Event{Kind: KindToken, Text: t})
				},
				Reasoning: func(t string) {
					a.emit(ctx, Event{Kind: KindReasoning, Text: t})
				},
				ToolArgs: func(name, delta string) {
					a.emit(ctx, Event{Kind: KindToolArgDelta, Tool: &ToolEvent{Name: name, ArgsDelta: delta}})
				},
				Usage: func(prompt, completion int, cost float64) {
					a.session.PromptTokens += prompt
					a.session.CompletionTokens += completion
					a.session.Cost += cost
					a.emit(ctx, Event{Kind: KindUsage, Usage: &UsageInfo{PromptTokens: prompt, CompletionTokens: completion, Cost: cost}})
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
	return a.runToolGroup(ctx, []ToolCall{tc})[0]
}

// execTool runs one call without emitting events, so it is safe to call from
// several goroutines at once. A tool error comes back both as the failed
// result the model sees and as err for the caller to report.
func (a *Agent) execTool(ctx context.Context, tc ToolCall) (Msg, error) {
	for _, t := range a.tools {
		if t.Name != tc.Function.Name {
			continue
		}

		input := json.RawMessage(tc.Function.Arguments)
		out, err := t.Fn(ctx, input)
		if err != nil {
			return Msg{Role: "tool", ToolCallID: tc.ID, ToolName: tc.Function.Name, Content: err.Error(), ToolFailed: true}, err
		}

		return Msg{Role: "tool", ToolCallID: tc.ID, ToolName: tc.Function.Name, Content: out}, nil
	}

	return Msg{Role: "tool", ToolCallID: tc.ID, ToolName: tc.Function.Name, Content: "tool not found", ToolFailed: true}, nil
}

// backoffFor returns how long to wait before the given attempt: exponential,
// ceilinged by the configured cap.
//
// The base triples on each attempt rather than doubling. A steeper curve is
// what shared, rate-limited pools — OpenRouter's free tier among them — call
// for: the next open slot is usually minutes away, and a backoff that crawls
// up one second at a time spends its attempts before it reaches the wait that
// would succeed. Tripling reaches a minute inside four attempts and the cap
// soon after, so the policy still fails fast on a genuinely broken endpoint
// while giving a congested free pool a real chance to clear.
func backoffFor(attempt int, cap time.Duration) time.Duration {
	if cap <= 0 {
		cap = DefaultSettings().Retry.BackoffCap.Std()
	}

	// Multiply out 3^attempt on a duration rather than a second count so the
	// cap can be any duration, not just a whole number of seconds.
	wait := time.Second
	for range attempt {
		wait *= 3
	}
	if wait <= 0 || wait > cap {
		return cap
	}

	return wait
}

// retryAfterHint extracts a server-provided Retry-After wait from an error, if
// present. Returns zero when the error carries no hint, so the caller falls
// back to its own backoff.
func retryAfterHint(err error) time.Duration {
	if err == nil {
		return 0
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.RetryAfter
	}
	return 0
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
