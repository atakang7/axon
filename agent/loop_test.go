package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// scriptedModel returns canned replies in order and records what it was asked.
// It is the entire reason Model is an interface: before that, none of the turn
// loop below could be exercised without a network and an API key.
type scriptedModel struct {
	replies  []Msg
	requests []Request
}

func (m *scriptedModel) Complete(_ context.Context, req Request) (*Msg, error) {
	m.requests = append(m.requests, req)
	if len(m.replies) == 0 {
		return nil, errors.New("scriptedModel: ran out of replies")
	}
	reply := m.replies[0]
	m.replies = m.replies[1:]
	if reply.Content != "" && req.Stream.Token != nil {
		req.Stream.Token(reply.Content)
	}
	return &reply, nil
}

func toolCall(id, name, args string) ToolCall {
	tc := ToolCall{ID: id, Type: "function"}
	tc.Function.Name = name
	tc.Function.Arguments = args
	return tc
}

// newTestAgent builds an agent with no network and no startup probes, in a
// throwaway directory.
func newTestAgent(t *testing.T, model Model, tools ...Tool) *Agent {
	t.Helper()
	dir := t.TempDir()

	probes := filepath.Join(dir, "probes.json")
	if err := os.WriteFile(probes, []byte("[]"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AXON_CONTEXT_PROBES_PATH", probes)
	t.Setenv("AXON_SESSION_PATH", filepath.Join(dir, "session.json"))

	ag, err := New(Config{
		Model:        model,
		SystemPrompt: "You are a test agent.",
		Tools:        tools,
		Cwd:          dir,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { ag.Close() })
	return ag
}

// The core contract: Step keeps calling the model until it stops asking for
// tools, runs each tool it does ask for, and returns the final text.
func TestStepRunsToolsUntilTheModelStops(t *testing.T) {
	var got string
	echo := Tool{
		Name:        "echo",
		Description: "echo the text back",
		Schema:      map[string]any{"type": "object"},
		Fn: func(_ context.Context, args json.RawMessage) (string, error) {
			var p struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return "", err
			}
			got = p.Text
			return "echoed: " + p.Text, nil
		},
	}

	model := &scriptedModel{replies: []Msg{
		{Role: "assistant", ToolCalls: []ToolCall{toolCall("c1", "echo", `{"text":"hi"}`)}},
		{Role: "assistant", Content: "all done"},
	}}
	ag := newTestAgent(t, model, echo)

	res, err := ag.Step(context.Background(), "say hi")
	if err != nil {
		t.Fatalf("Step: %v", err)
	}
	if got != "hi" {
		t.Fatalf("tool did not run with the model's arguments: got %q", got)
	}
	if res.Assistant != "all done" {
		t.Fatalf("Assistant = %q, want %q", res.Assistant, "all done")
	}
	if len(res.ToolCalls) != 1 || res.ToolCalls[0].Function.Name != "echo" {
		t.Fatalf("ToolCalls = %+v, want one call to echo", res.ToolCalls)
	}
	if len(model.requests) != 2 {
		t.Fatalf("model called %d times, want 2 (one per assistant turn)", len(model.requests))
	}

	// The second request must carry the tool result, or the model is being
	// asked to continue from a conversation it cannot see the outcome of.
	var sawResult bool
	for _, m := range model.requests[1].Messages {
		if m.Role == "tool" && m.ToolCallID == "c1" {
			sawResult = true
		}
	}
	if !sawResult {
		t.Fatal("second request did not include the tool result")
	}
}

// The model must be offered tool schemas and never tool implementations. This
// is the llm/tools boundary, asserted from the outside.
func TestModelReceivesSchemasNotImplementations(t *testing.T) {
	model := &scriptedModel{replies: []Msg{{Role: "assistant", Content: "ok"}}}
	ag := newTestAgent(t, model)

	if _, err := ag.Step(context.Background(), "hello"); err != nil {
		t.Fatalf("Step: %v", err)
	}

	specs := model.requests[0].Tools
	if len(specs) == 0 {
		t.Fatal("model was offered no tools; built-ins should always be present")
	}
	for _, s := range specs {
		if s.Name == "" || s.Schema == nil {
			t.Fatalf("tool spec is incomplete: %+v", s)
		}
	}
	// ToolSpec has no Fn field at all, so this is enforced by the type system;
	// the assertion that matters is that every built-in made it across.
	for _, want := range []string{"read", "write", "exec", "search", "task"} {
		found := false
		for _, s := range specs {
			if s.Name == want {
				found = true
			}
		}
		if !found {
			t.Errorf("built-in %q was not offered to the model", want)
		}
	}
}

// Events are the only way an embedder sees inside a turn, so the important
// ones must fire in a sane order.
func TestStepEmitsEventsInOrder(t *testing.T) {
	model := &scriptedModel{replies: []Msg{
		{Role: "assistant", ToolCalls: []ToolCall{toolCall("c1", "task", `{"action":"advance"}`)}},
		{Role: "assistant", Content: "finished"},
	}}

	var kinds []Kind
	dir := t.TempDir()
	probes := filepath.Join(dir, "probes.json")
	os.WriteFile(probes, []byte("[]"), 0644)
	t.Setenv("AXON_CONTEXT_PROBES_PATH", probes)
	t.Setenv("AXON_SESSION_PATH", filepath.Join(dir, "session.json"))

	ag, err := New(Config{
		Model:        model,
		SystemPrompt: "test",
		Cwd:          dir,
		OnEvent: func(_ context.Context, e Event) {
			kinds = append(kinds, e.Kind)
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer ag.Close()

	if _, err := ag.Step(context.Background(), "go"); err != nil {
		t.Fatalf("Step: %v", err)
	}

	first, last := kinds[0], kinds[len(kinds)-1]
	if first != KindUserInput {
		t.Errorf("first event = %v, want KindUserInput", first)
	}
	if last != KindTurnEnd {
		t.Errorf("last event = %v, want KindTurnEnd", last)
	}
	// The task tool fails here (no task registered), which must surface as a
	// tool error rather than failing the turn.
	if !contains(kinds, KindToolCall) || !contains(kinds, KindToolError) {
		t.Errorf("expected a tool call and a tool error, got %v", kinds)
	}
}

// A model that errors must fail the step rather than looping or hanging.
func TestStepSurfacesModelErrors(t *testing.T) {
	ag := newTestAgent(t, &scriptedModel{}) // no replies scripted
	if _, err := ag.Step(context.Background(), "hello"); err == nil {
		t.Fatal("Step returned no error when the model failed")
	}
}

// Config.Model is required; constructing without one must say so plainly.
func TestNewRequiresAModel(t *testing.T) {
	if _, err := New(Config{SystemPrompt: "x"}); !errors.Is(err, ErrNoModel) {
		t.Fatalf("New without a Model returned %v, want ErrNoModel", err)
	}
}

func contains(kinds []Kind, want Kind) bool {
	for _, k := range kinds {
		if k == want {
			return true
		}
	}
	return false
}

// Interrupting mid-batch must end the step. Before this was fixed the loop
// finished the turn, opened a fresh context and called the model again, so
// Ctrl-C during a tool call was silently ignored and the agent kept working.
func TestInterruptDuringToolCallEndsTheStep(t *testing.T) {
	var ag *Agent
	var secondRan bool

	stop := Tool{
		Name: "stop", Description: "interrupts", Schema: map[string]any{"type": "object"},
		Fn: func(context.Context, json.RawMessage) (string, error) {
			ag.Interrupt()
			return "interrupted", nil
		},
	}
	after := Tool{
		Name: "after", Description: "should not run", Schema: map[string]any{"type": "object"},
		Fn: func(context.Context, json.RawMessage) (string, error) {
			secondRan = true
			return "ran", nil
		},
	}

	model := &scriptedModel{replies: []Msg{
		{Role: "assistant", ToolCalls: []ToolCall{
			toolCall("c1", "stop", `{}`),
			toolCall("c2", "after", `{}`),
		}},
		{Role: "assistant", Content: "should never be reached"},
	}}
	ag = newTestAgent(t, model, stop, after)

	_, err := ag.Step(context.Background(), "go")
	if !errors.Is(err, ErrInterrupted) {
		t.Fatalf("Step returned %v, want ErrInterrupted", err)
	}
	if secondRan {
		t.Error("a queued tool ran after the turn was interrupted")
	}
	if len(model.requests) != 1 {
		t.Errorf("model was called %d times; an interrupted turn must not start another request", len(model.requests))
	}
}
