package axon

import (
	"context"
	"strings"
	"testing"
)

func TestPageSummaryRecoversWithoutInventingEvidence(t *testing.T) {
	page := categoryPage{session: &Session{Messages: []Msg{{ID: "m1", Role: "user", Content: "Preserve public API."}, {ID: "m2", Role: "tool", Content: "Unicode check remains unresolved."}}}}
	for _, bad := range []string{`{"quotes":["invented success"]}`, `{"quotes":[]}`, `not json`} {
		calls := 0
		model := funcModel{fn: func(context.Context, Request) (*Msg, error) { calls++; return &Msg{Content: bad}, nil }}
		out, err := summarizeCategoryPage(context.Background(), model, page, 0)
		if err != nil || calls != 2 || !strings.Contains(out, "automatic fallback") || !strings.Contains(out, "Preserve public API.") || strings.Contains(out, "invented success") {
			t.Fatalf("calls=%d out=%s err=%v", calls, out, err)
		}
	}
}

func TestPageSummaryRetryCanRecoverAndCancellationStops(t *testing.T) {
	page := categoryPage{session: &Session{Messages: []Msg{{ID: "m1", Role: "user", Content: "Preserve public API."}}}}
	calls := 0
	model := funcModel{fn: func(context.Context, Request) (*Msg, error) {
		calls++
		if calls == 1 {
			return &Msg{Content: `{"quotes":["invented"]}`}, nil
		}
		return &Msg{Content: `{"quotes":["Preserve public API."]}`}, nil
	}}
	out, err := summarizeCategoryPage(context.Background(), model, page, 0)
	if err != nil || strings.Contains(out, "fallback") || calls != 2 {
		t.Fatal(out, err, calls)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = summarizeCategoryPage(ctx, funcModel{fn: func(ctx context.Context, _ Request) (*Msg, error) { return nil, ctx.Err() }}, page, 0)
	if err != context.Canceled {
		t.Fatal("cancel was converted to a fallback", err)
	}
}
