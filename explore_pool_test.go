package axon

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type namedModel struct {
	inner Model
	name  string
}

func (m namedModel) Complete(ctx context.Context, r Request) (*Msg, error) {
	return m.inner.Complete(ctx, r)
}
func (m namedModel) Model() string { return m.name }

func TestAFailingExplorerHandsTheQuestionToTheNext(t *testing.T) {
	down := namedModel{funcModel{fn: func(context.Context, Request) (*Msg, error) { return nil, &APIError{Status: 400, Body: "rate limited"} }}, "free/a"}
	up := namedModel{&scriptedExplorer{replies: []*Msg{reportCall(t, exploreReport{Facts: []exploreFact{fact("auth/token.go", 5, 5, "return exp > now", "compares")}})}}, "free/b"}
	a := exploreAgent(t, down)
	a.SetExplorerModels(down, up)
	out, err := a.explore(context.Background(), "where?", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "auth/token.go:5") || !strings.Contains(out, "free/a failed") || !strings.Contains(out, "explorer free/b") {
		t.Fatalf("out:\n%s", out)
	}
	if !hasTool(a.tools, toolExplore) {
		t.Fatal("explore not offered with a pool")
	}
	a.SetExplorerModels(down)
	if _, err := a.explore(context.Background(), "where?", nil); err == nil || !strings.Contains(err.Error(), "every explorer failed") {
		t.Fatalf("err %v", err)
	}
}

func TestExploreCallsSpreadOverThePool(t *testing.T) {
	var used []string
	pool := make([]Model, 3)
	for i, name := range []string{"x", "y", "z"} {
		name := name
		pool[i] = namedModel{funcModel{fn: func(context.Context, Request) (*Msg, error) {
			used = append(used, name)
			raw, _ := json.Marshal(exploreReport{Facts: []exploreFact{fact("auth/token.go", 5, 5, "return exp > now", "c")}})
			return toolCallMsg("r", toolReport, string(raw)), nil
		}}, name}
	}
	a := exploreAgent(t, pool[0])
	a.SetExplorerModels(pool...)
	for i := 0; i < 3; i++ {
		if _, err := a.explore(context.Background(), "where?", nil); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Join(used, "") != "xyz" {
		t.Fatalf("calls went to %v, want one per model", used)
	}
}
