package axon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestFailedToolOnlyReleasedAfterSameCallSucceeds(t *testing.T) {
	s := newPrunerSession(t)
	appendRun := func(id, command string, code int) {
		var tc ToolCall
		tc.ID = id
		tc.Function.Name = "exec"
		tc.Function.Arguments = command
		s.Append(Msg{Role: "assistant", Content: "running", ToolCalls: []ToolCall{tc}})
		s.Append(Msg{Role: "tool", ToolName: "exec", ToolCallID: id, Content: fmt.Sprintf("$ command\nexit_code: %d\n", code) + strings.Repeat("output ", 100)})
	}
	appendRun("a", `{"command":"check parser"}`, 1)
	failed := s.Messages[1].ID
	appendRun("b", `{"command":"check other"}`, 0)
	if !unresolvedToolFailures(s)[failed] {
		t.Fatal("unrelated success erased failure")
	}
	appendRun("c", `{"command":"check parser"}`, 0)
	if unresolvedToolFailures(s)[failed] {
		t.Fatal("identical successful rerun did not release failure")
	}
}

func TestConsensusRunsInParallelAndRequiresEveryVote(t *testing.T) {
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	reviewer := func(reply string) Model {
		return funcModel{fn: func(ctx context.Context, r Request) (*Msg, error) {
			started <- struct{}{}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-release:
			}
			return &Msg{Content: reply}, nil
		}}
	}
	p, _ := NewConsensusPruner(reviewer(`{"park":[2,2,3]}`), reviewer(`{"park":[3,4]}`))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan *Msg, 1)
	go func() { m, _ := p.Complete(ctx, Request{}); done <- m }()
	for range 2 {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("reviewers were not concurrent")
		}
	}
	close(release)
	m := <-done
	if m == nil || m.Content != `{"park":[3]}` {
		t.Fatalf("consensus = %+v", m)
	}
}

func TestConsensusFailureIsNotConsent(t *testing.T) {
	good := funcModel{fn: func(context.Context, Request) (*Msg, error) { return &Msg{Content: `{"park":[1]}`}, nil }}
	bad := funcModel{fn: func(context.Context, Request) (*Msg, error) { return nil, errors.New("429") }}
	p, _ := NewConsensusPruner(good, bad)
	if _, err := p.Complete(context.Background(), Request{}); err == nil {
		t.Fatal("missing vote accepted")
	}
	if _, err := p.Complete(context.Background(), Request{}); err == nil {
		t.Fatal("cooldown not applied")
	}
}

func TestStrictParkListRejectsMalformedVotes(t *testing.T) {
	for _, raw := range []string{`{}`, `{"park":null}`, `{"park":[-1]}`, `{"park":[1.2]}`, `{"park":["1"]}`, `{"park":[],"execute":"x"}`, `{"park":[1]} trailing`} {
		if _, err := strictParkList(raw); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
}

func semanticFixture(t *testing.T) (*Session, *Pruner) {
	s := newPrunerSession(t)
	s.Append(Msg{Role: "user", Content: "Preserve the public API. " + strings.Repeat("constraint ", 100)})
	s.Append(Msg{Role: "tool", ToolName: "read", ToolCallID: "call1", Content: strings.Repeat("old source 日本語\n", 500)})
	s.Append(Msg{Role: "assistant", Content: strings.Repeat("old investigation ", 100)})
	s.Append(Msg{Role: "tool", ToolName: "read", Content: strings.Repeat("recent source ", 100)})
	s.Append(Msg{Role: "assistant", Content: "Current direction: implement the fix."})
	m := funcModel{fn: func(context.Context, Request) (*Msg, error) { return &Msg{Content: `{"park":[1,2,4,999]}`}, nil }}
	p := NewPruner(PrunerConfig{Model: m, Settings: PrunerSettings{Semantic: true, Mode: PruneExtreme, WindowBlocks: 2, FloorTokens: 1, GrowthTokens: 1}})
	return s, p
}

func TestSemanticPrunePreservesConstraintsRecentContextAndRecall(t *testing.T) {
	s, p := semanticFixture(t)
	before := p.ContextTokens(s)
	if !p.ShouldFire(s) {
		t.Fatal("semantic pruning did not fire despite reclaimable context")
	}
	after, err := p.Prune(context.Background(), s)
	if err != nil || after >= before {
		t.Fatalf("prune before=%d after=%d err=%v", before, after, err)
	}
	for i, m := range s.Messages {
		if m.Parked != (i == 1) {
			t.Fatalf("unexpected park state at %d", i)
		}
	}
	if len(p.Rejected()) != 3 {
		t.Fatal("did not reject protected/recent/invented IDs")
	}
	if !strings.Contains(s.ContextMessages(-1)[2].Content, "old investigation") {
		t.Fatal("kept context was masked by age")
	}
	loaded := LoadOrCreateSessionAt(s.Path())
	if !loaded.Messages[1].Parked || loaded.Messages[1].Content != s.Messages[1].Content {
		t.Fatal("archive not persisted")
	}
	out, err := RecallTool(loaded).Fn(context.Background(), json.RawMessage(`{"id":"m2","offset":0}`))
	if err != nil || !strings.Contains(out, "日本語") {
		t.Fatalf("recall: %s %v", out, err)
	}
	if _, err := RecallTool(loaded).Fn(context.Background(), json.RawMessage(`{"id":"m2","offset":-1}`)); err == nil {
		t.Fatal("negative offset accepted")
	}
}

func TestSemanticFailureLeavesSessionUnchanged(t *testing.T) {
	s, p := semanticFixture(t)
	p.model = funcModel{fn: func(context.Context, Request) (*Msg, error) { return &Msg{Content: `{"park":null}`}, nil }}
	if _, err := p.Prune(context.Background(), s); err == nil {
		t.Fatal("invalid response accepted")
	}
	for _, m := range s.Messages {
		if m.Parked {
			t.Fatal("parked after failure")
		}
	}
	if p.ShouldFire(s) {
		t.Fatal("failed pass immediately retried")
	}
}

func TestSemanticSaveFailureRollsBack(t *testing.T) {
	s, p := semanticFixture(t)
	s.path = t.TempDir() // writing to a directory must fail
	if _, err := p.Prune(context.Background(), s); err == nil {
		t.Fatal("expected persistence error")
	}
	for _, m := range s.Messages {
		if m.Parked {
			t.Fatal("metadata not rolled back")
		}
	}
}

func TestSemanticRequestContainsDirectionAndTailEvidence(t *testing.T) {
	s, p := semanticFixture(t)
	s.Messages[1].Content = strings.Repeat("x", 5000) + "UNRESOLVED FAILURE"
	r := semanticPrunerRequest(s, p.semanticCandidates(s), 2)
	for _, want := range []string{"UNRESOLVED FAILURE", "Current direction", "Preserve the public API", "middle omitted"} {
		if !strings.Contains(r, want) {
			t.Fatalf("missing %q", want)
		}
	}
}
