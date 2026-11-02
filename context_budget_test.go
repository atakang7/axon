package axon

import (
	"context"
	"fmt"
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

func TestCheckpointRequestsGetTheirOwnLargerBudget(t *testing.T) {
	resetMeter(t)
	big := Request{Messages: []Msg{{Role: "user", Content: strings.Repeat("func x() {}\n", 25000)}}}
	if err := checkRequestBudget(big, ""); err == nil {
		t.Fatal("main-agent request over 70K accepted")
	}
	big.InputLimit = PrunerInputLimit
	if err := checkRequestBudget(big, ""); err != nil {
		t.Fatal("a checkpoint could not see a context the main agent overflowed:", err)
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

// Over the limit, stale copies go first, then the oldest results; the newest
// messages stay, and clearing stops at the low-water mark.
func TestClearingTakesStaleCopiesFirstThenTheOldestAndKeepsTheTail(t *testing.T) {
	b := newRunBuilder(t)
	b.user("work")
	big := strings.Repeat("x", 40000)
	b.call(toolRead, map[string]any{"path": "a.go"}, "a v1 "+big, false)
	oldA := b.s.Messages[len(b.s.Messages)-1].ID
	b.call(toolRead, map[string]any{"path": "b.go"}, "b "+big, false)
	oldB := b.s.Messages[len(b.s.Messages)-1].ID
	b.call(toolRead, map[string]any{"path": "a.go"}, "a v2 "+big, false)
	newA := b.s.Messages[len(b.s.Messages)-1].ID
	for i := 0; i < tailKeep; i++ {
		b.call(toolExec, map[string]any{"command": fmt.Sprintf("echo %d", i)}, "ok "+big, false)
	}
	a := &Agent{session: b.s}
	// Clearing one big result brings this total to the low-water mark.
	if n := a.clearOldResults(contextLowWater + contextTokens(30000)); n != 1 {
		t.Fatalf("cleared %d results, want 1", n)
	}
	if !msgByID(b.s, oldA).Parked || msgByID(b.s, oldB).Parked || msgByID(b.s, newA).Parked {
		t.Fatal("the stale copy of a.go must go first, before older fresh results")
	}
	a.clearOldResults(1 << 30)
	if !msgByID(b.s, oldB).Parked || !msgByID(b.s, newA).Parked {
		t.Fatal("past the stale copies, the oldest results go")
	}
	for _, m := range b.s.Messages[len(b.s.Messages)-tailKeep:] {
		if m.Parked {
			t.Fatalf("a result in the newest %d messages was cleared", tailKeep)
		}
	}
}
