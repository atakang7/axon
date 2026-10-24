package axon

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestCompactRubricAndEvidence(t *testing.T) {
	s, _ := semanticFixture(t)
	valid := `{"closed_unit":true,"summarizable":true,"new_progress":true,"not_stuck":true,"state":"Located parser; no edits yet. Next: fix parser.","sources":["m2"]}`
	tool := CompactTool(s)
	before, _ := json.Marshal(s)
	for _, raw := range []string{
		strings.Replace(valid, `"not_stuck":true`, `"not_stuck":false`, 1),
		strings.Replace(valid, `"closed_unit":true`, `"closed_unit":false`, 1),
		strings.Replace(valid, `"summarizable":true`, `"summarizable":false`, 1),
		strings.Replace(valid, `"new_progress":true`, `"new_progress":false`, 1),
		strings.Replace(valid, `"m2"`, `"m999"`, 1),
		strings.Replace(valid, `["m2"]`, `[]`, 1),
		`{}`,
	} {
		if _, err := tool.Fn(context.Background(), json.RawMessage(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	out, err := tool.Fn(context.Background(), json.RawMessage(valid))
	if err != nil || !strings.Contains(out, "Evidence: m2") {
		t.Fatalf("out=%s err=%v", out, err)
	}
	after, _ := json.Marshal(s)
	if string(before) != string(after) {
		t.Fatal("tool mutated history")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := tool.Fn(ctx, json.RawMessage(valid)); err == nil {
		t.Fatal("ignored cancellation")
	}
}

func TestCheckpointTriggersReviewOnceAndSurvivesParking(t *testing.T) {
	s, p := semanticFixture(t)
	s.Append(Msg{Role: "tool", ToolName: "compact", Content: strings.Repeat("Current verified state. ", 40)})
	id := s.Messages[len(s.Messages)-1].ID
	for range 4 {
		s.Append(Msg{Role: "assistant", Content: "continue"})
	}
	p.lastFire = p.ContextTokens(s)
	p.growth = 100000
	if !p.ShouldFire(s) {
		t.Fatal("checkpoint did not bypass growth threshold")
	}
	p.model = funcModel{fn: func(_ context.Context, r Request) (*Msg, error) {
		if !strings.Contains(r.Messages[1].Content, "LATEST WORKING CHECKPOINT") {
			t.Fatal("reviewers missed old checkpoint")
		}
		return &Msg{Content: `{"park":[2,6]}`}, nil
	}}
	if _, err := p.Prune(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if p.ShouldFire(s) {
		t.Fatal("checkpoint retriggered without growth")
	}
	loaded := LoadOrCreateSessionAt(s.path)
	checkpoint := latestCheckpoint(loaded)
	if checkpoint == nil || checkpoint.ID != id || checkpoint.Parked {
		t.Fatal("checkpoint lost")
	}
	if !loaded.Messages[1].Parked {
		t.Fatal("old output not parked")
	}
	if !strings.Contains(loaded.ContextMessages(-1)[5].Content, "Current verified state") {
		t.Fatal("checkpoint absent from model context")
	}
}

func TestFailedCheckpointCannotTriggerReview(t *testing.T) {
	s, p := semanticFixture(t)
	p.lastFire = p.ContextTokens(s)
	p.growth = 100000
	s.Append(Msg{Role: "tool", ToolName: "compact", Content: "invalid", ToolFailed: true})
	if p.ShouldFire(s) {
		t.Fatal("failed checkpoint triggered review")
	}
}
