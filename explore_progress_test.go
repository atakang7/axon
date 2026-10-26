package axon

import (
	"context"
	"strings"
	"testing"
)

// Each explorer step reaches the user as progress on the explore call, so the
// explore row can show what the explorer is doing while it runs.
func TestExploreReportsItsStepsOnTheCall(t *testing.T) {
	explorer := &scriptedExplorer{replies: []*Msg{
		toolCallMsg("r1", toolRead, `{"path":"auth/token.go"}`),
		reportCall(t, exploreReport{Facts: []exploreFact{fact("auth/token.go", 5, 5, "return exp > now", "compares")}}),
	}}
	a, _, events := scoutAgent(t, explorer)
	if _, err := a.Step(context.Background(), "the token expiry check looks wrong, fix it"); err != nil {
		t.Fatal(err)
	}
	var callID string
	var steps []string
	for _, e := range *events {
		if e.Kind == KindToolCall && e.Tool.Name == toolExplore {
			callID = e.Tool.ID
		}
		if e.Kind == KindToolProgress {
			if e.Tool == nil || e.Tool.ID != callID || e.Tool.Name != toolExplore {
				t.Fatalf("progress not tied to the explore call: %+v", e.Tool)
			}
			steps = append(steps, e.Text)
		}
	}
	got := strings.Join(steps, " | ")
	for _, want := range []string{"is exploring", "Read auth/token.go", "Reported 1 facts"} {
		if !strings.Contains(got, want) {
			t.Fatalf("steps %q lack %q", got, want)
		}
	}
}

func TestExploreStepLines(t *testing.T) {
	for args, want := range map[string]string{
		`{"files":[{"path":"a.go"},{"path":"b.go"}]}`: "Read a.go, b.go",
		`{"path":"a.go","offset":40,"limit":20}`:      "Read a.go (from line 40)",
	} {
		tc := ToolCall{}
		tc.Function.Name, tc.Function.Arguments = toolRead, args
		if got := exploreStep(tc); got != want {
			t.Fatalf("%s: got %q want %q", args, got, want)
		}
	}
	tc := ToolCall{}
	tc.Function.Name, tc.Function.Arguments = toolSearch, `{"query":"expiry","path":"auth"}`
	if got := exploreStep(tc); got != `Search "expiry" in auth` {
		t.Fatalf("got %q", got)
	}
}
