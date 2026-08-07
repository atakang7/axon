package session

import (
	"fmt"
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

// Parking replaces a block with a breadcrumb rather than removing it, and
// never touches the append-only log.
func TestParkSubstitutesBreadcrumb(t *testing.T) {
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

	// Parking twice is idempotent and re-parking refreshes the breadcrumb
	// rather than stacking state.
	if err := s.Park(id, "revised gist", "pruner: still stale"); err != nil {
		t.Fatalf("re-Park: %v", err)
	}
	if out := s.ContextMessages(); !strings.Contains(out[1].Content, "revised gist") {
		t.Fatalf("re-park did not refresh the breadcrumb: %q", out[1].Content)
	}
	if len(s.Messages) != 2 {
		t.Fatalf("park mutated the audit log: %d messages, want 2", len(s.Messages))
	}
	if s.Messages[1].Content != "a very long answer" {
		t.Fatalf("park overwrote stored content: %q", s.Messages[1].Content)
	}
}

// Appending one message must cost the same on a long log as on a short one.
// ensure() used to re-scan and fmt.Sscanf every existing ID on every Append,
// Save and ContextMessages, which made a single turn quadratic in its own
// length. Allocation is the tell: a scan of the log allocates per message,
// a constant-time append does not.
func TestAppendDoesNotScanTheLog(t *testing.T) {
	s := newAt(t, "s.json")
	for range 5000 {
		s.Append(llm.Msg{Role: "assistant", Content: "a previous block"})
	}

	perAppend := testing.AllocsPerRun(100, func() {
		s.Append(llm.Msg{Role: "assistant", Content: "one more"})
	})

	// Generous: the point is that this is a small constant rather than
	// something that grows with the 5000 blocks already recorded.
	if perAppend > 8 {
		t.Fatalf("Append made %.0f allocations on a 5000-block log; it is scanning the log", perAppend)
	}
}

// A log loaded from disk still gets IDs backfilled and its high-water mark
// re-derived — that work moved out of ensure(), it did not disappear.
func TestLoadedLogGetsIDsBackfilled(t *testing.T) {
	s := newAt(t, "s.json")
	s.Messages = []llm.Msg{
		{Role: "system", Content: "you are an agent"},
		{Role: "user", Content: "no id yet"},
		{Role: "assistant", Content: "already stamped", ID: "m7"},
	}
	s.assignIDs()

	if s.Messages[0].ID != "" {
		t.Fatalf("system message was given an ID: %q", s.Messages[0].ID)
	}
	if s.Messages[1].ID == "" {
		t.Fatal("message loaded without an ID was not backfilled")
	}
	if s.NextBlockID < 7 {
		t.Fatalf("high-water mark %d ignores the existing m7; the next append would collide", s.NextBlockID)
	}
	s.Append(llm.Msg{Role: "user", Content: "fresh"})
	if got := s.Messages[3].ID; got == "m7" {
		t.Fatalf("new block reused an existing ID: %q", got)
	}
}

// The undo ledger is part of the session, and the session is re-marshalled in
// full on every save — so an unbounded ledger is paid for on every later tool
// call. Twenty edits of a 550KB file used to produce a 23MB session file.
func TestEditLedgerIsBounded(t *testing.T) {
	s := newAt(t, "s.json")
	chunk := strings.Repeat("x", 1<<20) // 1MB per edit

	for i := range 40 {
		s.RecordEdit(fmt.Sprintf("/tmp/f%d.go", i), chunk)
	}

	total := 0
	for _, e := range s.Edits {
		total += len(e.Before)
	}
	if total > maxUndoBytes {
		t.Fatalf("ledger holds %d bytes, over the %d budget", total, maxUndoBytes)
	}
	if len(s.Edits) == 0 {
		t.Fatal("eviction emptied the ledger; the newest edit must survive")
	}

	// Eviction drops the oldest, so the most recent edit is the one Undo gets.
	e, ok := s.Undo()
	if !ok {
		t.Fatal("nothing to undo after 40 edits")
	}
	if e.Path != "/tmp/f39.go" {
		t.Fatalf("Undo returned %s, want the most recent edit", e.Path)
	}
}

// A single edit larger than the whole budget must still be undoable — it is the
// one a user is most likely to want back.
func TestOversizedEditSurvivesEviction(t *testing.T) {
	s := newAt(t, "s.json")
	s.RecordEdit("/tmp/small.go", "before")
	s.RecordEdit("/tmp/huge.go", strings.Repeat("y", maxUndoBytes+1))

	e, ok := s.Undo()
	if !ok || e.Path != "/tmp/huge.go" {
		t.Fatalf("Undo returned (%v, %v), want the oversized edit", e.Path, ok)
	}
}

// Park requires a reason, and the system prompt can never be parked — losing
// either would let the curator quietly destroy context nothing can restore.
func TestParkRefusesReasonlessAndSystem(t *testing.T) {
	s := newAt(t, "s.json")
	s.Messages = []llm.Msg{{Role: "system", Content: "you are an agent"}}
	s.Append(llm.Msg{Role: "user", Content: "hi"})
	s.assignIDs()

	if err := s.Park(s.Messages[1].ID, "gist", "  "); err == nil {
		t.Fatal("Park accepted a blank reason")
	}
	// The system message is skipped by assignIDs and never gets an ID, so it is
	// unreachable by ID; assert it survives a park attempt on every ID.
	for _, m := range s.Messages {
		_ = s.Park(m.ID, "gist", "test")
	}
	if s.ContextMessages()[0].Role != "system" {
		t.Fatal("system prompt was removed from context")
	}
	if s.ContextMessages()[0].Content != "you are an agent" {
		t.Fatal("system prompt was replaced by a breadcrumb")
	}
}
