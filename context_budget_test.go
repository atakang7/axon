package axon

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func resetMeter(t *testing.T) {
	old := meter
	meter = &tokenMeter{ratio: map[string]float64{}}
	t.Cleanup(func() { meter = old })
}

func TestTokenMeterCalibratesPerModelFromProviderUsage(t *testing.T) {
	resetMeter(t)
	if got := meter.tokens("m", 30000); got < 10000 {
		t.Fatalf("uncalibrated estimate must be conservative, got %d", got)
	}
	// Provider reports 4 bytes/token for m: estimates for m drop accordingly.
	for range 20 {
		meter.observe("m:free", 40000, 10000)
	}
	if got := meter.tokens("m", 40000); got < 10000 || got > 11500 {
		t.Fatalf("calibrated estimate %d, want ~10000 plus safety margin", got)
	}
	// An unobserved model never gets a laxer ratio than the default.
	if got := meter.tokens("other", 30000); got < 10000 {
		t.Fatalf("unknown model estimate %d is not conservative", got)
	}
	// Tiny payloads are template-dominated noise and do not calibrate.
	meter.observe("tiny", 100, 90)
	if _, ok := meter.ratio["tiny"]; ok {
		t.Fatal("tiny request calibrated the meter")
	}
}

func TestCuratorRequestsGetTheirOwnLargerBudget(t *testing.T) {
	resetMeter(t)
	big := Request{Messages: []Msg{{Role: "user", Content: strings.Repeat("func x() {}\n", 25000)}}}
	if err := checkRequestBudget(big, ""); err == nil {
		t.Fatal("main-agent request over 70K accepted")
	}
	big.InputLimit = PrunerInputLimit
	if err := checkRequestBudget(big, ""); err != nil {
		t.Fatal("curator could not see a context the main agent overflowed:", err)
	}
}

func TestInputBudgetBlocksNetworkIncludingToolSchema(t *testing.T) {
	resetMeter(t)
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	defer server.Close()
	c, _ := NewClient(ClientConfig{Provider: Provider{BaseURL: server.URL, Model: "test"}})
	for _, req := range []Request{
		{Messages: []Msg{{Role: "user", Content: strings.Repeat("界", 70000)}}},
		{Tools: []ToolSpec{{Name: "big", Schema: map[string]any{"description": strings.Repeat("x", 250000)}}}},
	} {
		if _, err := c.Complete(context.Background(), req); err == nil {
			t.Fatal("oversized request accepted")
		}
	}
	if calls != 0 {
		t.Fatal("oversized request reached network")
	}
}

func TestCategoryPagingPreservesEveryByte(t *testing.T) {
	resetMeter(t)
	s, p, _ := categoryFixture(t)
	// Beyond PrunerInputLimit: the only case that still pages.
	s.Messages[1].Content = strings.Repeat("source 日本語\n", 40000)
	candidates := p.semanticCandidates(s)
	pages, err := categoryPages(s, candidates, PruneExtreme)
	if err != nil {
		t.Fatal(err)
	}
	joined := map[string]string{}
	for _, page := range pages {
		if err := checkRequestBudget(categoryRequest(page.session, page.candidates, PruneExtreme), ""); err != nil {
			t.Fatal(err)
		}
		for _, m := range page.session.Messages {
			joined[m.ID] += m.Content
		}
	}
	for _, m := range s.Messages {
		if joined[m.ID] != m.Content {
			t.Fatalf("lost content in %s", m.ID)
		}
	}
}

func TestOverBudgetAgentReviewsWholeContextInOneRequest(t *testing.T) {
	resetMeter(t)
	s, p, _ := categoryFixture(t)
	// Over the main agent's 70K budget, well under the curator's.
	s.Messages[1].Content = strings.Repeat("obsolete source 日本語\n", 12000)
	candidates := p.semanticCandidates(s)
	pruneCalls := 0
	p.model = funcModel{fn: func(ctx context.Context, r Request) (*Msg, error) {
		if err := checkRequestBudget(r, ""); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(r.Messages[0].Content, "Extract a compact memory") {
			t.Fatal("context that fits the curator was paged")
		}
		pruneCalls++
		for _, m := range s.Messages {
			if !m.Parked && !strings.Contains(r.Messages[1].Content, m.Content) {
				t.Fatalf("review request is missing block %s", m.ID)
			}
		}
		return &Msg{Content: categoryJSON(candidates, false, nil)}, nil
	}}
	mainCalls := 0
	a := &Agent{session: s, pruner: p, settings: Settings{Pruner: PrunerSettings{Semantic: true}}, model: funcModel{fn: func(ctx context.Context, r Request) (*Msg, error) {
		mainCalls++
		if err := checkRequestBudget(r, ""); err != nil {
			t.Fatal("main call sent before compaction", err)
		}
		return &Msg{Role: "assistant", Content: "done"}, nil
	}}}
	if _, err := a.chat(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if mainCalls != 1 || pruneCalls != 1 || !s.Messages[1].Parked {
		t.Fatalf("main=%d prune=%d parked=%v", mainCalls, pruneCalls, s.Messages[1].Parked)
	}
}
