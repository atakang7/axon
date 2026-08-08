package axon

import (
	"strings"
	"testing"
)

// The task block is appended at the tail, never woven into the middle of the
// log, so the prefix the provider sees is byte-identical turn over turn —
// that stability is what lets a provider's prompt cache actually hit. And
// with no task registered, the block must be entirely absent rather than an
// empty trailing message.
func TestContextMessagesTaskBlockIsTrailingAndCacheStable(t *testing.T) {
	s := newAt(t, "s.json")
	s.Append(Msg{Role: "user", Content: "hello"})
	s.Append(Msg{Role: "assistant", Content: "hi"})

	noTask := s.ContextMessages()
	if len(noTask) != 2 {
		t.Fatalf("with no task registered, got %d messages, want 2 (no trailing block)", len(noTask))
	}

	if err := s.RegisterTask("goal", []TaskStep{{Description: "step"}}); err != nil {
		t.Fatal(err)
	}
	withTask := s.ContextMessages()
	if len(withTask) != 3 {
		t.Fatalf("with a task registered, got %d messages, want 3", len(withTask))
	}
	last := withTask[len(withTask)-1]
	if last.Role != "system" || !strings.Contains(last.Content, "[task]") {
		t.Fatalf("trailing message = %+v, want the task block", last)
	}

	// The prefix (everything before the task block) must be byte-identical
	// across two calls, and across a change to the task itself — only the
	// tail should move.
	for i := 0; i < len(noTask); i++ {
		if !sameMsg(withTask[i], noTask[i]) {
			t.Fatalf("prefix changed once a task was registered at index %d: %+v vs %+v", i, withTask[i], noTask[i])
		}
	}
	again := s.ContextMessages()
	for i := 0; i < len(withTask)-1; i++ {
		if !sameMsg(again[i], withTask[i]) {
			t.Fatalf("prefix is not stable across calls at index %d", i)
		}
	}
}

// sameMsg compares the fields that matter for prompt-cache stability. Msg
// carries a slice field, so it is not comparable with ==.
func sameMsg(a, b Msg) bool {
	return a.Role == b.Role && a.Content == b.Content &&
		a.ToolCallID == b.ToolCallID && a.ToolName == b.ToolName &&
		len(a.ToolCalls) == len(b.ToolCalls)
}

// ContextMessages is the boundary between the append-only log and the wire:
// only role/content/tool-call fields may cross it. ID and park bookkeeping
// are internal state the provider has no schema for and must never see.
func TestContextMessagesStripsInternalFields(t *testing.T) {
	s := newAt(t, "s.json")
	s.Append(Msg{Role: "user", Content: "hello"})
	s.Append(Msg{Role: "assistant", Content: "a long answer"})
	id := s.Messages[1].ID
	if err := s.Park(id, "gist", "pruner: reason"); err != nil {
		t.Fatal(err)
	}

	for _, m := range s.ContextMessages() {
		if m.ID != "" {
			t.Fatalf("ContextMessages leaked ID: %+v", m)
		}
		if m.Parked {
			t.Fatalf("ContextMessages leaked Parked: %+v", m)
		}
		if m.ParkSummary != "" || m.ParkReason != "" {
			t.Fatalf("ContextMessages leaked park metadata: %+v", m)
		}
	}
}

// A parked assistant message that carried tool_calls must present as
// content-only (a large tool call's arguments would otherwise still cost
// tokens even though the block was parked to save them) — but the
// append-only log underneath must still hold the original tool_calls, since
// Park is a projection change, not an edit.
func TestParkedAssistantMessageLosesToolCallsOnlyInProjection(t *testing.T) {
	s := newAt(t, "s.json")
	s.Append(Msg{Role: "user", Content: "write the file"})
	call := ToolCall{ID: "call_1"}
	call.Function.Name = "write"
	call.Function.Arguments = `{"path":"big.go","content":"..."}`
	s.Append(Msg{Role: "assistant", Content: "writing", ToolCalls: []ToolCall{call}})

	id := s.Messages[1].ID
	if err := s.Park(id, "wrote big.go", "pruner: large"); err != nil {
		t.Fatalf("Park: %v", err)
	}

	projected := s.ContextMessages()
	if len(projected[1].ToolCalls) != 0 {
		t.Fatalf("projection still carries tool_calls: %+v", projected[1].ToolCalls)
	}

	if len(s.Messages[1].ToolCalls) != 1 {
		t.Fatalf("underlying log lost its tool_calls after Park: %+v", s.Messages[1].ToolCalls)
	}
	if s.Messages[1].ToolCalls[0].ID != "call_1" {
		t.Fatalf("underlying tool_calls changed shape: %+v", s.Messages[1].ToolCalls)
	}
}

// Park must fail loudly on an unknown ID (a pruner bug or stale ID should
// never be silently ignored), and re-parking an already-parked block must be
// accepted — the breadcrumb-refresh path TestParkSubstitutesBreadcrumb
// exercises, restated here as the "has content by being parked" rule from
// the reason-check itself.
func TestParkUnknownIDAndReParkAlreadyParked(t *testing.T) {
	s := newAt(t, "s.json")
	if err := s.Park("m404", "gist", "reason"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("Park on an unknown ID = %v, want an error naming \"not found\"", err)
	}

	s.Append(Msg{Role: "assistant", Content: "answer"})
	id := s.Messages[0].ID
	if err := s.Park(id, "first gist", "reason one"); err != nil {
		t.Fatalf("first Park: %v", err)
	}
	if err := s.Park(id, "second gist", "reason two"); err != nil {
		t.Fatalf("re-Park of an already-parked block was rejected: %v", err)
	}
}
