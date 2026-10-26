package axon

import (
	"context"
	"strings"
	"testing"
)

// scoutAgent is an agent over exploreRepo with an explorer and a main model
// that answers at once.
func scoutAgent(t *testing.T, explorer Model) (*Agent, *queueModel, *[]Event) {
	t.Helper()
	repo := exploreRepo(t)
	cfg := noToolsConfig(t)
	cfg.Cwd = repo.Dir()
	model := newQueueModel(scriptedReply{msg: finalMsg("done")}, scriptedReply{msg: finalMsg("done again")})
	cfg.Model = model
	cfg.Explorer = explorer
	events := &[]Event{}
	cfg.OnEvent = func(_ context.Context, e Event) { *events = append(*events, e) }
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	return a, model, events
}

func scoutBrief(r Request) (call, brief string) {
	for _, m := range r.Messages {
		for _, tc := range m.ToolCalls {
			if strings.HasPrefix(tc.ID, "scout-") {
				call = tc.Function.Arguments
			}
		}
		if m.Role == "tool" && strings.Contains(m.Content, "scout, before your first reply") {
			brief = m.Content
		}
	}
	return call, brief
}

func TestEveryRequestStartsWithTheScout(t *testing.T) {
	explorer := &scriptedExplorer{replies: []*Msg{reportCall(t, exploreReport{Facts: []exploreFact{fact("auth/token.go", 5, 5, "return exp > now", "expiry is compared with now")}})}}
	a, model, events := scoutAgent(t, explorer)
	if _, err := a.Step(context.Background(), "the token expiry check looks wrong, fix it"); err != nil {
		t.Fatal(err)
	}
	if len(explorer.reqs) == 0 {
		t.Fatal("the scout did not run")
	}
	call, brief := scoutBrief(model.requests[0])
	if !strings.Contains(call, "the token expiry check looks wrong") {
		t.Fatalf("the scout was not asked about the request: %s", call)
	}
	if !strings.Contains(brief, "auth/token.go:5") || !strings.Contains(brief, "return exp > now") {
		t.Fatalf("the main model's first request lacks the verified brief:\n%s", brief)
	}
	sawCall, sawResult := false, false
	for _, e := range *events {
		sawCall = sawCall || (e.Kind == KindToolCall && e.Tool.Name == toolExplore)
		sawResult = sawResult || (e.Kind == KindToolResult && e.Tool.Name == toolExplore)
	}
	if !sawCall || !sawResult {
		t.Fatal("the scout is not visible as an explore call")
	}

	// A follow-up is scouted with the earlier request for context.
	explorer.replies = []*Msg{reportCall(t, exploreReport{})}
	if _, err := a.Step(context.Background(), "do the same for the refresh path"); err != nil {
		t.Fatal(err)
	}
	last := explorer.reqs[len(explorer.reqs)-1].Messages[1].Content
	if !strings.Contains(last, "do the same for the refresh path") || !strings.Contains(last, "the token expiry check looks wrong") || !strings.Contains(last, "done") {
		t.Fatalf("the follow-up was scouted without its context:\n%s", last)
	}
}

func TestTheScoutStaysOutOfShortMessagesAndWhenOff(t *testing.T) {
	explorer := &scriptedExplorer{}
	a, model, _ := scoutAgent(t, explorer)
	if _, err := a.Step(context.Background(), "push it"); err != nil {
		t.Fatal(err)
	}
	a.settings.Explorer.NoScout = true
	if _, err := a.Step(context.Background(), "now look at the whole auth package please"); err != nil {
		t.Fatal(err)
	}
	if len(explorer.reqs) != 0 {
		t.Fatalf("the scout ran %d times", len(explorer.reqs))
	}
	for _, r := range model.requests {
		if call, _ := scoutBrief(r); call != "" {
			t.Fatal("a scout call was recorded")
		}
	}
}

func TestAFailedScoutLeavesNoTraceAndTheTurnGoesOn(t *testing.T) {
	down := funcModel{fn: func(context.Context, Request) (*Msg, error) { return nil, &APIError{Status: 400, Body: "rate limited"} }}
	a, model, events := scoutAgent(t, down)
	res, err := a.Step(context.Background(), "the token expiry check looks wrong, fix it")
	if err != nil || res.Assistant != "done" {
		t.Fatalf("res %+v err %v", res, err)
	}
	if call, brief := scoutBrief(model.requests[0]); call != "" || brief != "" {
		t.Fatal("a failed scout left a call or a brief in the context")
	}
	skipped := false
	for _, e := range *events {
		skipped = skipped || (e.Kind == KindInfo && strings.HasPrefix(e.Text, "scout skipped"))
	}
	if !skipped {
		t.Fatal("the failed scout was not reported")
	}
}

func TestTheScoutCanBeInterrupted(t *testing.T) {
	started := make(chan struct{})
	slow := funcModel{fn: func(ctx context.Context, _ Request) (*Msg, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	a, model, _ := scoutAgent(t, slow)
	done := make(chan error)
	go func() { _, err := a.Step(context.Background(), "the token expiry check looks wrong, fix it"); done <- err }()
	<-started
	if !a.Interrupt() {
		t.Fatal("nothing to interrupt during the scout")
	}
	if err := <-done; err != ErrInterrupted {
		t.Fatalf("err %v", err)
	}
	if len(model.requests) != 0 {
		t.Fatal("the main model was called after an interrupted scout")
	}
}
