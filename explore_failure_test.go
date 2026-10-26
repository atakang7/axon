package axon

import (
	"context"
	"strings"
	"testing"
	"time"
)

// An explorer that read a file and then went down still hands on where it
// looked, so the deciding model does not locate the same places again.
func TestAStoppedExplorationKeepsWhereItLooked(t *testing.T) {
	calls := 0
	reader := namedModel{funcModel{fn: func(context.Context, Request) (*Msg, error) {
		calls++
		if calls == 1 {
			return toolCallMsg("r1", "read", `{"path":"auth/token.go"}`), nil
		}
		return nil, &APIError{Status: 400, Body: "rate limited"}
	}}, "free/a"}
	a := exploreAgent(t, reader)
	out, err := a.explore(context.Background(), "where is expiry checked?", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "stopped before reporting") || !strings.Contains(out, `read {"path":"auth/token.go"}`) {
		t.Fatalf("out:\n%s", out)
	}
}

// Once every explorer failed in a request, explore answers at once: the
// deciding model is not sent back to wait on the same pool.
func TestExploreIsNotRetriedAfterThePoolFailed(t *testing.T) {
	calls := 0
	down := funcModel{fn: func(context.Context, Request) (*Msg, error) {
		calls++
		return nil, &APIError{Status: 400, Body: "bad"}
	}}
	a := exploreAgent(t, down)
	if _, err := a.explore(context.Background(), "q", nil); err == nil {
		t.Fatal("want an error")
	}
	before := calls
	if _, err := a.explore(context.Background(), "q again", nil); err == nil || !strings.Contains(err.Error(), "already failed") {
		t.Fatalf("err %v", err)
	}
	if calls != before {
		t.Fatal("the failed pool was called again")
	}
	a.exploreFailed.Store(false) // a new request resets it
	if _, err := a.explore(context.Background(), "q", nil); err == nil || strings.Contains(err.Error(), "already failed") {
		t.Fatalf("a new request should try the pool again: %v", err)
	}
}

// A rate-limited explorer gives up after one short wait, not the main
// model's long backoff.
func TestARateLimitedExplorerFailsFast(t *testing.T) {
	busy := funcModel{fn: func(context.Context, Request) (*Msg, error) {
		return nil, &APIError{Status: 429, Body: "rate limited"}
	}}
	a := exploreAgent(t, busy)
	start := time.Now()
	if _, err := a.explore(context.Background(), "q", nil); err == nil {
		t.Fatal("want an error")
	}
	if d := time.Since(start); d > 15*time.Second {
		t.Fatalf("explorer spent %s on retries", d)
	}
}

func TestCheckpointSurvivesUnreadableNotes(t *testing.T) {
	for _, raw := range []string{
		`{"keep":["m3"],"notes":"learned that x"}`,
		`{"keep":["m3"],"notes":["no id here"]}`,
		`{"keep":["m3"],"notes":"m4: the build uses make"}`,
	} {
		var args checkpointArgs
		if err := args.UnmarshalJSON([]byte(raw)); err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		if len(args.Keep) != 1 {
			t.Fatalf("%s: keep lost", raw)
		}
	}
	var args checkpointArgs
	_ = args.UnmarshalJSON([]byte(`{"notes":"m4: the build uses make"}`))
	if len(args.Notes) != 1 || args.Notes[0].ID != "m4" {
		t.Fatalf("notes %+v", args.Notes)
	}
}
