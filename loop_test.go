package axon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Test doubles
//
// Config.Model is an interface for exactly this reason: the whole turn loop
// can be driven with no network by queuing up the replies a model would have
// streamed back.
// ---------------------------------------------------------------------------

// scriptedReply is one queued response: either a message or an error.
type scriptedReply struct {
	msg *Msg
	err error
}

// queueModel returns queued replies in order, falling back to a plain "ok"
// once the queue is drained, and records every Request it receives so a
// test can assert what actually crossed into the model layer (L9).
type queueModel struct {
	mu       sync.Mutex
	replies  []scriptedReply
	requests []Request
}

func newQueueModel(replies ...scriptedReply) *queueModel {
	return &queueModel{replies: replies}
}

func (m *queueModel) Complete(_ context.Context, req Request) (*Msg, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests = append(m.requests, req)
	if len(m.replies) == 0 {
		return &Msg{Role: "assistant", Content: "ok"}, nil
	}
	r := m.replies[0]
	m.replies = m.replies[1:]
	return r.msg, r.err
}

func (m *queueModel) reqs() []Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Request, len(m.requests))
	copy(out, m.requests)
	return out
}

// funcModel wraps an arbitrary Complete implementation — used where a test
// needs to act (e.g. call Interrupt) at the moment the model is invoked,
// rather than just return a canned value.
type funcModel struct {
	fn func(ctx context.Context, req Request) (*Msg, error)
}

func (m funcModel) Complete(ctx context.Context, req Request) (*Msg, error) { return m.fn(ctx, req) }

// eventLog collects emitted events under a mutex so it is safe to read after
// Step returns even though nothing in these tests actually emits
// concurrently.
type eventLog struct {
	mu     sync.Mutex
	events []Event
}

func (l *eventLog) record(_ context.Context, e Event) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, e)
}

func (l *eventLog) all() []Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Event, len(l.events))
	copy(out, l.events)
	return out
}

func (l *eventLog) kinds() []Kind {
	all := l.all()
	out := make([]Kind, len(all))
	for i, e := range all {
		out[i] = e.Kind
	}
	return out
}

// toolCallMsg builds an assistant message that calls one tool.
func toolCallMsg(id, name, args string) *Msg {
	var tc ToolCall
	tc.ID = id
	tc.Function.Name = name
	tc.Function.Arguments = args
	return &Msg{Role: "assistant", ToolCalls: []ToolCall{tc}}
}

// finalMsg builds a plain assistant reply with no tool calls — the signal
// that ends a Step.
func finalMsg(content string) *Msg {
	return &Msg{Role: "assistant", Content: content}
}

// fnTool builds a minimal, schema-complete Tool around fn, for tests that
// want full control over tool behaviour without going through the built-ins.
func fnTool(name string, fn func(context.Context, json.RawMessage) (string, error)) Tool {
	return Tool{Name: name, Description: name, Schema: okSchema, Fn: fn}
}

// allBuiltins is every built-in name — excluding all of them isolates a
// loop test from the filesystem/process side effects the real tools have.
var allBuiltins = []string{"read", "write", "exec", "bash_output", "kill_shell", "search", "task"}

// noToolsConfig is baseConfig with every built-in excluded, so a test's own
// cfg.Tools are the only tools the model can call.
func noToolsConfig(t *testing.T) Config {
	t.Helper()
	cfg := baseConfig(t)
	cfg.ExcludeBuiltins = allBuiltins
	return cfg
}

// ---------------------------------------------------------------------------
// L1-L3: basic Step contract
// ---------------------------------------------------------------------------

// Empty input must be rejected before anything reaches the log — otherwise a
// caller's stray empty submit silently burns a turn and a save.
func TestStepRejectsEmptyInput(t *testing.T) {
	cfg := noToolsConfig(t)
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Close()

	before := len(a.session.Messages)
	if _, err := a.Step(context.Background(), ""); err == nil {
		t.Fatal("Step accepted empty input")
	}
	if len(a.session.Messages) != before {
		t.Fatalf("empty input appended to the log: %d -> %d messages", before, len(a.session.Messages))
	}
}

// A one-shot reply (no tool calls) must append both sides of the exchange,
// persist, and hand back the assistant text and turn number.
func TestStepOneShotReply(t *testing.T) {
	cfg := noToolsConfig(t)
	cfg.Model = newQueueModel(scriptedReply{msg: finalMsg("hello there")})
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Close()

	before := len(a.session.Messages)
	res, err := a.Step(context.Background(), "hi")
	if err != nil {
		t.Fatalf("Step: %v", err)
	}
	if res.Assistant != "hello there" {
		t.Fatalf("Assistant = %q, want %q", res.Assistant, "hello there")
	}
	if res.Turn != 1 {
		t.Fatalf("Turn = %d, want 1", res.Turn)
	}
	if len(a.session.Messages) != before+2 {
		t.Fatalf("log grew by %d messages, want 2 (user, assistant)", len(a.session.Messages)-before)
	}
	last2 := a.session.Messages[len(a.session.Messages)-2:]
	if last2[0].Role != "user" || last2[0].Content != "hi" {
		t.Fatalf("appended user message = %+v", last2[0])
	}
	if last2[1].Role != "assistant" || last2[1].Content != "hello there" {
		t.Fatalf("appended assistant message = %+v", last2[1])
	}

	// Save landed on disk, not just in memory.
	data, err := os.ReadFile(a.SessionPath())
	if err != nil || len(data) == 0 {
		t.Fatalf("session was not persisted: %v", err)
	}
}

// Turn increments exactly once per Step, regardless of how many tool calls
// happen inside it — it is a step counter, not a model-call counter.
func TestTurnIncrementsOncePerStep(t *testing.T) {
	cfg := noToolsConfig(t)
	echo := fnTool("echo", func(context.Context, json.RawMessage) (string, error) { return "done", nil })
	cfg.Tools = []Tool{echo}
	cfg.Model = newQueueModel(
		scriptedReply{msg: toolCallMsg("c1", "echo", "{}")},
		scriptedReply{msg: finalMsg("turn one done")},
		scriptedReply{msg: finalMsg("turn two done")},
	)
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Close()

	res1, err := a.Step(context.Background(), "go")
	if err != nil {
		t.Fatalf("Step 1: %v", err)
	}
	if res1.Turn != 1 {
		t.Fatalf("Turn after step 1 = %d, want 1", res1.Turn)
	}

	res2, err := a.Step(context.Background(), "go again")
	if err != nil {
		t.Fatalf("Step 2: %v", err)
	}
	if res2.Turn != 2 {
		t.Fatalf("Turn after step 2 = %d, want 2", res2.Turn)
	}
}

// ---------------------------------------------------------------------------
// L4-L10: tool dispatch and the request/response contract with the model
// ---------------------------------------------------------------------------

// Tool calls must run until the model stops asking, and every result must be
// fed back as a `tool` message with the matching ToolCallID, visible in the
// *next* request's Messages — this is how the model ever finds out what its
// tool call returned.
func TestToolResultsFeedIntoNextRequest(t *testing.T) {
	cfg := noToolsConfig(t)
	echo := fnTool("echo", func(context.Context, json.RawMessage) (string, error) { return "the tool result", nil })
	cfg.Tools = []Tool{echo}
	model := newQueueModel(
		scriptedReply{msg: toolCallMsg("call_1", "echo", `{"x":1}`)},
		scriptedReply{msg: finalMsg("all done")},
	)
	cfg.Model = model
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Close()

	res, err := a.Step(context.Background(), "please echo")
	if err != nil {
		t.Fatalf("Step: %v", err)
	}
	if res.Assistant != "all done" {
		t.Fatalf("Assistant = %q", res.Assistant)
	}
	if len(res.ToolCalls) != 1 || res.ToolCalls[0].ID != "call_1" {
		t.Fatalf("StepResult.ToolCalls = %+v", res.ToolCalls)
	}

	reqs := model.reqs()
	if len(reqs) != 2 {
		t.Fatalf("model saw %d requests, want 2", len(reqs))
	}
	// The second request must carry a `tool` message answering call_1.
	found := false
	for _, m := range reqs[1].Messages {
		if m.Role == "tool" && m.ToolCallID == "call_1" {
			found = true
			if !strings.Contains(m.Content, "the tool result") {
				t.Fatalf("tool result message content = %q, want it to contain the tool's output", m.Content)
			}
		}
	}
	if !found {
		t.Fatal("the tool's result never appeared in the next request's Messages")
	}
}

// Multiple tool calls inside one assistant message must run in the order the
// model listed them — callers that chain tool calls (write then verify)
// depend on this.
func TestMultipleToolCallsRunInOrder(t *testing.T) {
	cfg := noToolsConfig(t)
	var order []string
	var mu sync.Mutex
	record := func(name string) func(context.Context, json.RawMessage) (string, error) {
		return func(context.Context, json.RawMessage) (string, error) {
			mu.Lock()
			order = append(order, name)
			mu.Unlock()
			return name + "-result", nil
		}
	}
	cfg.Tools = []Tool{fnTool("first", record("first")), fnTool("second", record("second")), fnTool("third", record("third"))}

	msg := &Msg{Role: "assistant"}
	for i, name := range []string{"first", "second", "third"} {
		var tc ToolCall
		tc.ID = fmt.Sprintf("c%d", i)
		tc.Function.Name = name
		tc.Function.Arguments = "{}"
		msg.ToolCalls = append(msg.ToolCalls, tc)
	}
	cfg.Model = newQueueModel(scriptedReply{msg: msg}, scriptedReply{msg: finalMsg("ok")})

	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Close()

	if _, err := a.Step(context.Background(), "go"); err != nil {
		t.Fatalf("Step: %v", err)
	}
	want := []string{"first", "second", "third"}
	if len(order) != len(want) {
		t.Fatalf("call order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("call order = %v, want %v", order, want)
		}
	}
}

// A tool returning a Go error must not abort the turn: the error text
// becomes the tool result content, and the model gets to react to it on the
// next round.
func TestToolErrorBecomesResultContentNotGoError(t *testing.T) {
	cfg := noToolsConfig(t)
	boom := fnTool("boom", func(context.Context, json.RawMessage) (string, error) {
		return "", errors.New("disk is on fire")
	})
	cfg.Tools = []Tool{boom}
	cfg.Model = newQueueModel(
		scriptedReply{msg: toolCallMsg("c1", "boom", "{}")},
		scriptedReply{msg: finalMsg("recovered")},
	)
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Close()

	res, err := a.Step(context.Background(), "go")
	if err != nil {
		t.Fatalf("Step returned a Go error for a tool failure: %v", err)
	}
	if res.Assistant != "recovered" {
		t.Fatalf("loop did not continue past the tool error: %+v", res)
	}
	var toolMsg *Msg
	for i := range a.session.Messages {
		if a.session.Messages[i].Role == "tool" {
			toolMsg = &a.session.Messages[i]
		}
	}
	if toolMsg == nil || !strings.Contains(toolMsg.Content, "disk is on fire") {
		t.Fatalf("tool error did not land in the tool message content: %+v", toolMsg)
	}
}

// An unknown tool name must produce a result the model can read, not a
// panic — a model can hallucinate a tool name at any time.
func TestUnknownToolNameYieldsResultString(t *testing.T) {
	cfg := noToolsConfig(t)
	cfg.Model = newQueueModel(
		scriptedReply{msg: toolCallMsg("c1", "nonexistent", "{}")},
		scriptedReply{msg: finalMsg("ok")},
	)
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Close()

	if _, err := a.Step(context.Background(), "go"); err != nil {
		t.Fatalf("Step: %v", err)
	}
	var toolMsg *Msg
	for i := range a.session.Messages {
		if a.session.Messages[i].Role == "tool" {
			toolMsg = &a.session.Messages[i]
		}
	}
	if toolMsg == nil {
		t.Fatal("no tool message recorded for the unknown call")
	}
	if !strings.Contains(toolMsg.Content, "tool not found") {
		t.Fatalf("unknown-tool result = %q, want it to contain %q", toolMsg.Content, "tool not found")
	}
}

// Every tool result is stamped with its [#mN] block-id prefix, and the
// KindToolResult event's BlockID must match — this is the handle the pruner
// later parks by.
func TestToolResultStampedWithBlockID(t *testing.T) {
	cfg := noToolsConfig(t)
	echo := fnTool("echo", func(context.Context, json.RawMessage) (string, error) { return "payload", nil })
	cfg.Tools = []Tool{echo}
	cfg.Model = newQueueModel(
		scriptedReply{msg: toolCallMsg("c1", "echo", "{}")},
		scriptedReply{msg: finalMsg("ok")},
	)
	var log eventLog
	cfg.OnEvent = log.record
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Close()

	if _, err := a.Step(context.Background(), "go"); err != nil {
		t.Fatalf("Step: %v", err)
	}

	var toolMsg *Msg
	for i := range a.session.Messages {
		if a.session.Messages[i].Role == "tool" {
			toolMsg = &a.session.Messages[i]
		}
	}
	if toolMsg == nil {
		t.Fatal("no tool message recorded")
	}
	wantPrefix := "[#" + toolMsg.ID + "]\n"
	if !strings.HasPrefix(toolMsg.Content, wantPrefix) {
		t.Fatalf("tool message content = %q, want prefix %q", toolMsg.Content, wantPrefix)
	}

	var sawResult bool
	for _, e := range log.all() {
		if e.Kind == KindToolResult {
			sawResult = true
			if e.Tool.BlockID != toolMsg.ID {
				t.Fatalf("KindToolResult.BlockID = %q, want %q", e.Tool.BlockID, toolMsg.ID)
			}
		}
	}
	if !sawResult {
		t.Fatal("no KindToolResult event emitted")
	}
}

// Only ToolSpecs may reach the model — never a tool's Fn. This is the whole
// point of projecting Tool down to ToolSpec at the request boundary.
func TestOnlyToolSpecsReachTheModel(t *testing.T) {
	cfg := noToolsConfig(t)
	echo := fnTool("echo", func(context.Context, json.RawMessage) (string, error) { return "x", nil })
	cfg.Tools = []Tool{echo}
	model := newQueueModel(scriptedReply{msg: finalMsg("ok")})
	cfg.Model = model
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Close()

	if _, err := a.Step(context.Background(), "go"); err != nil {
		t.Fatalf("Step: %v", err)
	}

	reqs := model.reqs()
	if len(reqs) != 1 {
		t.Fatalf("got %d requests, want 1", len(reqs))
	}
	if len(reqs[0].Tools) != len(a.tools) {
		t.Fatalf("model saw %d tool specs, agent has %d tools — not a 1:1 projection", len(reqs[0].Tools), len(a.tools))
	}
	names := map[string]bool{}
	for _, spec := range reqs[0].Tools {
		names[spec.Name] = true
	}
	for _, tl := range a.tools {
		if !names[tl.Name] {
			t.Fatalf("tool %q missing from the request's ToolSpecs", tl.Name)
		}
	}
	// ToolSpec has no Fn field at all, so there is structurally nothing
	// further to assert here beyond the type itself (see
	// TestToolSpecShapeIsAnAllowlist in setup_test.go).
}

// Event bracketing for a tool-using turn must be
// UserInput -> APICall -> (ToolCall -> ToolResult)* -> AssistantEnd -> TurnEnd,
// with Token/Reasoning passed through and Turn stamped on every event.
func TestEventBracketingForToolUsingTurn(t *testing.T) {
	cfg := noToolsConfig(t)
	echo := fnTool("echo", func(context.Context, json.RawMessage) (string, error) { return "result", nil })
	cfg.Tools = []Tool{echo}
	calls := 0
	cfg.Model = funcModel{fn: func(ctx context.Context, req Request) (*Msg, error) {
		calls++
		if calls == 1 {
			if req.Stream.Token != nil {
				req.Stream.Token("tok")
			}
			if req.Stream.Reasoning != nil {
				req.Stream.Reasoning("think")
			}
			return toolCallMsg("c1", "echo", "{}"), nil
		}
		return finalMsg("final answer"), nil
	}}
	var log eventLog
	cfg.OnEvent = log.record
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Close()

	if _, err := a.Step(context.Background(), "go"); err != nil {
		t.Fatalf("Step: %v", err)
	}

	kinds := log.kinds()
	want := []Kind{
		KindSessionStart, KindTurnStart, KindUserInput, KindAPICall, KindToken, KindReasoning,
		KindToolCall, KindToolResult,
		KindAPICall, KindAssistantEnd, KindTurnEnd,
	}
	if len(kinds) != len(want) {
		t.Fatalf("event kinds = %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("event[%d] = %v, want %v (full: %v)", i, kinds[i], want[i], kinds)
		}
	}
	for _, e := range log.all() {
		// KindSessionStart fires from New, before any turn exists.
		if e.Kind == KindSessionStart {
			continue
		}
		if e.Turn != 1 {
			t.Fatalf("event %v has Turn=%d, want 1", e.Kind, e.Turn)
		}
		if e.Time.IsZero() {
			t.Fatalf("event %v has a zero Time", e.Kind)
		}
	}
}

// ---------------------------------------------------------------------------
// L11-L13: Interrupt
// ---------------------------------------------------------------------------

// Interrupt() while the model call is in flight must end the step with
// ErrInterrupted and whatever ToolCalls already happened, rather than racing
// a sleep to land the cancel at the right moment.
func TestInterruptDuringChatEndsStepInterrupted(t *testing.T) {
	cfg := noToolsConfig(t)
	echo := fnTool("echo", func(context.Context, json.RawMessage) (string, error) { return "ran", nil })
	cfg.Tools = []Tool{echo}

	var agentRef *Agent
	calls := 0
	cfg.Model = funcModel{fn: func(ctx context.Context, req Request) (*Msg, error) {
		calls++
		if calls == 1 {
			return toolCallMsg("c1", "echo", "{}"), nil
		}
		// Second model call: fire the interrupt from inside Complete, then
		// block until the loop's context observes it.
		agentRef.Interrupt()
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Close()
	agentRef = a

	res, err := a.Step(context.Background(), "go")
	if !errors.Is(err, ErrInterrupted) {
		t.Fatalf("Step error = %v, want ErrInterrupted", err)
	}
	if len(res.ToolCalls) != 1 || res.ToolCalls[0].ID != "c1" {
		t.Fatalf("StepResult.ToolCalls = %+v, want the one tool call that ran before the interrupt", res.ToolCalls)
	}
}

// Interrupt() mid tool-batch must stop the *remaining* calls in that batch —
// they must not be recorded as failed tools — and the step must return
// ErrInterrupted instead of looping into another model call.
func TestInterruptMidToolBatchStopsRemainingCalls(t *testing.T) {
	cfg := noToolsConfig(t)
	var agentRef *Agent
	secondRan := false
	first := fnTool("first", func(context.Context, json.RawMessage) (string, error) {
		agentRef.Interrupt()
		return "first ran", nil
	})
	second := fnTool("second", func(context.Context, json.RawMessage) (string, error) {
		secondRan = true
		return "second ran", nil
	})
	cfg.Tools = []Tool{first, second}

	msg := &Msg{Role: "assistant"}
	var tc1, tc2 ToolCall
	tc1.ID, tc1.Function.Name, tc1.Function.Arguments = "c1", "first", "{}"
	tc2.ID, tc2.Function.Name, tc2.Function.Arguments = "c2", "second", "{}"
	msg.ToolCalls = []ToolCall{tc1, tc2}
	cfg.Model = newQueueModel(scriptedReply{msg: msg})

	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Close()
	agentRef = a

	res, err := a.Step(context.Background(), "go")
	if !errors.Is(err, ErrInterrupted) {
		t.Fatalf("Step error = %v, want ErrInterrupted", err)
	}
	if secondRan {
		t.Fatal("the second tool call ran after Interrupt() fired mid-batch")
	}
	if len(res.ToolCalls) != 1 || res.ToolCalls[0].ID != "c1" {
		t.Fatalf("StepResult.ToolCalls = %+v, want only the first call recorded", res.ToolCalls)
	}
	for _, m := range a.session.Messages {
		if m.Role == "tool" && m.ToolCallID == "c2" {
			t.Fatal("the interrupted second call was recorded as a tool result")
		}
	}
}

// Interrupt() with no turn in flight must report false rather than pretend
// it cancelled something.
func TestInterruptWithNoTurnInFlight(t *testing.T) {
	cfg := noToolsConfig(t)
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Close()

	if a.Interrupt() {
		t.Fatal("Interrupt() returned true with no turn in flight")
	}
}

// ---------------------------------------------------------------------------
// L14: save failures
// ---------------------------------------------------------------------------

// A save failure on the *initial* user-message append must abort the turn:
// there is nothing safe to build on top of an unpersisted user message.
func TestSaveFailureOnInitialAppendAbortsStep(t *testing.T) {
	dir := t.TempDir()
	// A directory at the session path: os.WriteFile onto it always fails,
	// so every Save() fails, starting with the very first one.
	sessionPath := filepath.Join(dir, "session.json")
	if err := os.MkdirAll(sessionPath, 0755); err != nil {
		t.Fatalf("seed unwritable session path: %v", err)
	}
	t.Setenv("AXON_SESSION_PATH", sessionPath)

	cfg := Config{Model: scriptedModel{}, SystemPrompt: "you are a test agent", ExcludeBuiltins: allBuiltins}
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Close()

	if _, err := a.Step(context.Background(), "hi"); err == nil {
		t.Fatal("Step succeeded despite the session being unwritable")
	}
}

// A save failure *after* the initial append (e.g. mid-turn, once a tool has
// run) must not abort the turn — it is reported via KindError and the model
// still gets its final answer. Losing durability is bad; losing the turn on
// top of it would be worse.
func TestSaveFailureMidTurnContinues(t *testing.T) {
	dir := t.TempDir()
	sessDir := filepath.Join(dir, "sessdir")
	sessionPath := filepath.Join(sessDir, "session.json")
	t.Setenv("AXON_SESSION_PATH", sessionPath)

	breaker := fnTool("breaker", func(context.Context, json.RawMessage) (string, error) {
		// Make the session directory unwritable so the Save() right after
		// this tool's result (and every one after) fails.
		// Zero permissions on the directory blocks path traversal into it
		// entirely (not just creating new entries), so the existing
		// session.json becomes unreachable even though its own mode is 0600.
		if err := os.Chmod(sessDir, 0); err != nil {
			return "", err
		}
		return "broke it", nil
	})
	cfg := Config{
		Model:           newQueueModel(scriptedReply{msg: toolCallMsg("c1", "breaker", "{}")}, scriptedReply{msg: finalMsg("final answer")}),
		SystemPrompt:    "you are a test agent",
		ExcludeBuiltins: allBuiltins,
		Tools:           []Tool{breaker},
	}
	var log eventLog
	cfg.OnEvent = log.record

	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(sessDir, 0o755) }) // let TempDir clean up
	defer a.Close()

	res, err := a.Step(context.Background(), "go")
	if err != nil {
		t.Fatalf("Step returned an error for a mid-turn save failure: %v", err)
	}
	if res.Assistant != "final answer" {
		t.Fatalf("turn did not complete: %+v", res)
	}

	sawSaveError := false
	for _, e := range log.all() {
		if e.Kind == KindError && strings.Contains(e.Text, "session not persisted") {
			sawSaveError = true
		}
	}
	if !sawSaveError {
		t.Fatal("no KindError 'session not persisted' event emitted for the mid-turn save failure")
	}
}

// ---------------------------------------------------------------------------
// L15-L17: chat() retry policy
// ---------------------------------------------------------------------------

// A retryable error must be retried with backoff and succeed on a later
// attempt; a non-retryable error must return immediately with no retry at
// all. Only a single retry is exercised here — chat() allows up to 10 real
// time.After backoffs, and driving that whole ladder would make the suite
// slow without proving anything the retryable() table test (L17) does not
// already cover in isolation.
func TestChatRetriesRetryableErrorThenSucceeds(t *testing.T) {
	cfg := noToolsConfig(t)
	model := newQueueModel(
		scriptedReply{err: io.EOF}, // retryable
		scriptedReply{msg: finalMsg("recovered")},
	)
	cfg.Model = model
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Close()

	start := time.Now()
	msg, err := a.chat(context.Background(), nil)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if msg.Content != "recovered" {
		t.Fatalf("chat returned %+v, want the post-retry reply", msg)
	}
	if len(model.reqs()) != 2 {
		t.Fatalf("model saw %d requests, want 2 (initial + one retry)", len(model.reqs()))
	}
	// One backoff (2s at attempt 1) must have elapsed, but the whole ladder
	// (up to 60s x10) must not have.
	if elapsed > 10*time.Second {
		t.Fatalf("chat took %v to recover from one retryable error — backoff ladder did not stop at one retry", elapsed)
	}
}

// A non-retryable error must return immediately: no backoff, no second
// request.
func TestChatDoesNotRetryNonRetryableError(t *testing.T) {
	cfg := noToolsConfig(t)
	model := newQueueModel(scriptedReply{err: errors.New("API error 400: bad request")})
	cfg.Model = model
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Close()

	start := time.Now()
	_, err = a.chat(context.Background(), nil)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("chat swallowed a non-retryable error")
	}
	if len(model.reqs()) != 1 {
		t.Fatalf("model saw %d requests, want exactly 1 (no retry)", len(model.reqs()))
	}
	if elapsed > time.Second {
		t.Fatalf("chat took %v for a non-retryable error, want near-instant", elapsed)
	}
}

// An empty reply (no content, no tool calls) is retried exactly once — some
// reasoning models occasionally emit only thinking tokens — and then fails
// with a clear error rather than silently returning nothing.
func TestChatRetriesEmptyReplyOnceThenFails(t *testing.T) {
	cfg := noToolsConfig(t)
	model := newQueueModel(
		scriptedReply{msg: &Msg{Role: "assistant", Content: ""}},
		scriptedReply{msg: &Msg{Role: "assistant", Content: "  "}}, // whitespace-only still counts as empty
	)
	cfg.Model = model
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Close()

	_, err = a.chat(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "empty response from model") {
		t.Fatalf("chat error = %v, want it to name an empty response", err)
	}
	if len(model.reqs()) != 2 {
		t.Fatalf("model saw %d requests, want exactly 2 (one retry, then fail)", len(model.reqs()))
	}
}

// retryable is the pure decision behind the entire backoff ladder, and is
// tested directly rather than through chat()'s real time.After sleeps —
// driving every attempt through chat() would make the suite hang for minutes.
//
// The HTTP cases go through *APIError rather than an error string. That is the
// point of the type: a retry loop that matched message text would keep passing
// this test while breaking the moment a provider reworded its errors.
func TestRetryable(t *testing.T) {
	agent := &Agent{settings: DefaultSettings()}

	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"EOF", io.EOF, true},
		{"unexpected EOF", io.ErrUnexpectedEOF, true},
		{"net timeout", timeoutErr{}, true},
		{"DNS error", &net.DNSError{Err: "no such host", Name: "example.invalid"}, true},
		{"ECONNREFUSED", syscall.ECONNREFUSED, true},
		{"ECONNRESET", syscall.ECONNRESET, true},
		{"429 rate limited", &APIError{Status: 429, Body: "rate limited"}, true},
		{"500 internal", &APIError{Status: 500}, true},
		{"502 bad gateway", &APIError{Status: 502}, true},
		{"503 unavailable", &APIError{Status: 503}, true},
		{"504 timeout", &APIError{Status: 504}, true},
		{"wrapped 429", fmt.Errorf("chat: %w", &APIError{Status: 429}), true},
		{"context.Canceled", context.Canceled, false},
		{"context.DeadlineExceeded", context.DeadlineExceeded, false},
		{"400 bad request", &APIError{Status: 400, Body: "bad model"}, false},
		{"401 unauthorized", &APIError{Status: 401}, false},
		{"plain error", errors.New("something else"), false},

		// An error that merely reads like a rate limit is not one. This is the
		// case the old string-matching implementation got wrong.
		{"text that looks like 429", errors.New("API error 429: rate limited"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := agent.retryable(tc.err); got != tc.want {
				t.Fatalf("retryable(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// The retry policy is configuration, so a config that declines to retry rate
// limits must be obeyed.
func TestRetryablePolicyIsConfigurable(t *testing.T) {
	settings := DefaultSettings()
	settings.Retry.OnStatus = []int{503}
	agent := &Agent{settings: settings}

	if agent.retryable(&APIError{Status: 429}) {
		t.Error("429 retried although the configured policy lists only 503")
	}

	if !agent.retryable(&APIError{Status: 503}) {
		t.Error("503 not retried although the configured policy lists it")
	}
}

// timeoutErr is a minimal net.Error whose Timeout() is true.
type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

// ---------------------------------------------------------------------------
// L18-L19: Run
// ---------------------------------------------------------------------------

// Run must call Step once per input line, stop cleanly when input is
// exhausted, continue past an interrupted step rather than aborting the
// whole session, and propagate any other error.
func TestRunDrivesStepPerInputLine(t *testing.T) {
	cfg := noToolsConfig(t)
	cfg.Model = newQueueModel(
		scriptedReply{msg: finalMsg("one")},
		scriptedReply{msg: finalMsg("two")},
		scriptedReply{msg: finalMsg("three")},
	)
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Close()

	lines := []string{"a", "b", "c"}
	i := 0
	input := func() (string, bool) {
		if i >= len(lines) {
			return "", false
		}
		l := lines[i]
		i++
		return l, true
	}

	if err := a.Run(context.Background(), input); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if a.session.Turn != 3 {
		t.Fatalf("Turn = %d, want 3 (one per input line)", a.session.Turn)
	}
}

// An interrupted step must not stop Run — only real errors do.
func TestRunContinuesPastInterruptedStep(t *testing.T) {
	cfg := noToolsConfig(t)
	calls := 0
	cfg.Model = funcModel{fn: func(ctx context.Context, req Request) (*Msg, error) {
		calls++
		if calls == 1 {
			return nil, context.Canceled // Step 1 will be treated as interrupted
		}
		return finalMsg("recovered"), nil
	}}
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Close()

	lines := []string{"a", "b"}
	i := 0
	input := func() (string, bool) {
		if i >= len(lines) {
			return "", false
		}
		l := lines[i]
		i++
		return l, true
	}

	if err := a.Run(context.Background(), input); err != nil {
		t.Fatalf("Run returned an error instead of continuing past ErrInterrupted: %v", err)
	}
	if a.session.Turn != 2 {
		t.Fatalf("Turn = %d, want 2 — Run must have kept going after the interrupted first step", a.session.Turn)
	}
}

// A real (non-interrupt) error from Step must propagate out of Run rather
// than being swallowed.
func TestRunPropagatesNonInterruptError(t *testing.T) {
	cfg := noToolsConfig(t)
	wantErr := errors.New("API error 400: bad request")
	cfg.Model = newQueueModel(scriptedReply{err: wantErr})
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Close()

	input := func() (string, bool) { return "hi", true }
	err = a.Run(context.Background(), input)
	if err == nil || !strings.Contains(err.Error(), "400") {
		t.Fatalf("Run error = %v, want the underlying model error to propagate", err)
	}
}

// Run must return ctx.Err() once the context is cancelled between steps.
func TestRunReturnsContextError(t *testing.T) {
	cfg := noToolsConfig(t)
	cfg.Model = newQueueModel(scriptedReply{msg: finalMsg("ok")})
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Close()

	ctx, cancel := context.WithCancel(context.Background())
	first := true
	input := func() (string, bool) {
		if first {
			first = false
			cancel() // cancel after supplying exactly one line
			return "hi", true
		}
		return "", false
	}

	err = a.Run(ctx, input)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %v, want context.Canceled", err)
	}
}

// ---------------------------------------------------------------------------
// L20: emit
// ---------------------------------------------------------------------------

// emit with a nil OnEvent must be a cheap no-op — the hot path for every
// embedder that does not care about observability.
// New emits KindSessionStart with the session's identity before anything
// else happens, and Close emits KindSessionEnd — so an embedder building a
// trace of the whole run, not just its turns, has clean bookends to key off.
func TestSessionLifecycleEventsBracketTheRun(t *testing.T) {
	var log eventLog
	cfg := noToolsConfig(t)
	cfg.OnEvent = log.record

	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	all := log.all()
	if len(all) != 1 || all[0].Kind != KindSessionStart {
		t.Fatalf("events after New = %v, want exactly one KindSessionStart", log.kinds())
	}
	start := all[0].Session
	if start == nil || start.ID == "" || start.Path == "" {
		t.Fatalf("KindSessionStart.Session = %+v, want ID and Path populated", start)
	}

	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	kinds := log.kinds()
	last := kinds[len(kinds)-1]
	if last != KindSessionEnd {
		t.Fatalf("last event after Close = %v, want KindSessionEnd", last)
	}
}

func TestEmitWithNilOnEventIsNoOp(t *testing.T) {
	cfg := noToolsConfig(t)
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Close()

	// Must not panic.
	a.emit(context.Background(), Event{Kind: KindInfo, Text: "hello"})
}

// A zero Event.Time is filled in by emit — callers should not have to stamp
// every event they build by hand.
func TestEmitFillsZeroTime(t *testing.T) {
	var log eventLog
	cfg := noToolsConfig(t)
	cfg.OnEvent = log.record
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Close()

	before := len(log.all())
	a.emit(context.Background(), Event{Kind: KindInfo})
	all := log.all()
	if len(all) != before+1 {
		t.Fatalf("got %d events, want %d", len(all), before+1)
	}
	if last := all[len(all)-1]; last.Time.IsZero() {
		t.Fatal("emit left Event.Time zero")
	}
}
