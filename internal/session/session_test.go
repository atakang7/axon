package session

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/atakang7/axon/internal/llm"
)

// newAt builds a session backed by a real file in t's temp dir. Tests run
// against the actual save/load path rather than a mock, so what they prove is
// what production does.
func newAt(t *testing.T, name string) *Session {
	t.Helper()
	s := &Session{path: filepath.Join(t.TempDir(), name)}
	s.ensure()
	return s
}

// Reset must not relocate a session an embedder placed deliberately. Before
// this was fixed, Reset re-resolved the default per-cwd path and every later
// Save silently wrote somewhere the embedder never asked for.
func TestResetKeepsCustomPath(t *testing.T) {
	s := newAt(t, "custom.json")
	want := s.Path()

	s.Append(llm.Msg{Role: "user", Content: "hello"})
	s.Reset()

	if got := s.Path(); got != want {
		t.Fatalf("Reset relocated storage: got %q, want %q", got, want)
	}
	if len(s.Messages) != 0 {
		t.Fatalf("Reset left %d messages behind", len(s.Messages))
	}
	if err := s.Save(); err != nil {
		t.Fatalf("Save after Reset: %v", err)
	}
}

// A parked assistant message that carried tool_calls must take its tool
// results with it. Emitting a `tool` message whose matching tool_call is gone
// is a protocol violation that providers reject outright, so this is the
// sharpest edge in the projection.
func TestContextMessagesDropsOrphanedToolResults(t *testing.T) {
	s := newAt(t, "s.json")

	s.Append(llm.Msg{Role: "user", Content: "read the file"})
	call := llm.ToolCall{ID: "call_1"}
	call.Function.Name = "read"
	call.Function.Arguments = `{"path":"x.go"}`
	s.Append(llm.Msg{Role: "assistant", Content: "reading", ToolCalls: []llm.ToolCall{call}})
	s.Append(llm.Msg{Role: "tool", ToolCallID: "call_1", ToolName: "read", Content: "file body"})

	assistantID := s.Messages[1].ID
	if err := s.Park(assistantID, "read x.go", "pruner: not needed"); err != nil {
		t.Fatalf("Park: %v", err)
	}

	for _, m := range s.ContextMessages() {
		if m.Role == "tool" && m.ToolCallID == "call_1" {
			t.Fatal("orphaned tool result survived into context after its tool_call was parked")
		}
		if len(m.ToolCalls) > 0 {
			t.Fatalf("parked assistant message still carries %d tool_calls", len(m.ToolCalls))
		}
	}

	// The log itself is append-only: nothing may be deleted by parking.
	if len(s.Messages) != 3 {
		t.Fatalf("park mutated the log: %d messages, want 3", len(s.Messages))
	}
	if s.Messages[1].Content != "reading" {
		t.Fatalf("park overwrote stored content: %q", s.Messages[1].Content)
	}
}

// Parking substitutes a breadcrumb; forgetting removes the block outright.
func TestParkBreadcrumbAndForget(t *testing.T) {
	s := newAt(t, "s.json")
	s.Append(llm.Msg{Role: "user", Content: "keep me"})
	s.Append(llm.Msg{Role: "assistant", Content: "a very long answer"})

	id := s.Messages[1].ID
	if err := s.Park(id, "the answer", "pruner: stale"); err != nil {
		t.Fatalf("Park: %v", err)
	}
	out := s.ContextMessages()
	if len(out) != 2 {
		t.Fatalf("parked block should be replaced, not removed: got %d", len(out))
	}
	if !strings.Contains(out[1].Content, "parked") || !strings.Contains(out[1].Content, "the answer") {
		t.Fatalf("breadcrumb missing summary or marker: %q", out[1].Content)
	}

	if err := s.Forget(id, "pruner: irrelevant"); err != nil {
		t.Fatalf("Forget: %v", err)
	}
	if out := s.ContextMessages(); len(out) != 1 {
		t.Fatalf("forgotten block should leave no trace in context: got %d", len(out))
	}
	if len(s.Messages) != 2 {
		t.Fatalf("forget deleted from the audit log: %d messages, want 2", len(s.Messages))
	}
}

// Park requires a reason, and the system prompt is never removable — losing
// either would let the curator quietly destroy context it cannot restore.
func TestParkRefusesReasonlessAndSystem(t *testing.T) {
	s := newAt(t, "s.json")
	s.Messages = []llm.Msg{{Role: "system", Content: "you are an agent"}}
	s.Append(llm.Msg{Role: "user", Content: "hi"})
	s.ensure()

	if err := s.Park(s.Messages[1].ID, "gist", "  "); err == nil {
		t.Fatal("Park accepted a blank reason")
	}
	// The system message is skipped by ensure() and never gets an ID, so it is
	// unreachable by ID; assert it survives a forget attempt on every ID.
	for _, m := range s.Messages {
		_ = s.Forget(m.ID, "test")
	}
	if s.ContextMessages()[0].Role != "system" {
		t.Fatal("system prompt was removed from context")
	}
}
