package axon

import (
	"context"
	"testing"
	"time"
)

func TestBackoffDoublesFromFiveSecondsUpToTheCap(t *testing.T) {
	var got []time.Duration
	for attempt := 1; attempt <= 6; attempt++ {
		got = append(got, backoffFor(attempt, time.Minute))
	}
	want := []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second, 40 * time.Second, time.Minute, time.Minute}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("backoff %v, want %v", got, want)
		}
	}
	if d := DefaultSettings().Retry; d.MaxAttempts != 11 {
		t.Fatalf("default attempts %d, want one try and ten retries", d.MaxAttempts)
	}
}

func TestRequestTimeoutsAreRetriedButACancelledTurnIsNot(t *testing.T) {
	calls := 0
	s := newTestSession(t)
	s.Append(Msg{Role: "user", Content: "hi"})
	a := &Agent{session: s, settings: Settings{Retry: RetryConfig{MaxAttempts: 4, BackoffCap: Duration(time.Millisecond), OnStatus: []int{429}}},
		model: funcModel{fn: func(ctx context.Context, r Request) (*Msg, error) {
			calls++
			if calls < 3 {
				return nil, context.DeadlineExceeded // the HTTP client's own timeout
			}
			return &Msg{Role: "assistant", Content: "ok"}, nil
		}}}
	if _, err := a.chat(context.Background(), nil); err != nil || calls != 3 {
		t.Fatalf("err %v after %d calls; timeouts must be retried", err, calls)
	}
	calls = 0
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a.model = funcModel{fn: func(context.Context, Request) (*Msg, error) { calls++; return nil, context.Canceled }}
	if _, err := a.chat(ctx, nil); err == nil || calls != 1 {
		t.Fatalf("a cancelled turn was retried: %d calls", calls)
	}
}

func TestOpenRouterAcceptsAnyCatalogModelAndARoute(t *testing.T) {
	s := Settings{Providers: map[string]Endpoint{
		"openrouter": {BaseURL: "https://openrouter.ai/api/v1", Models: map[string]ModelOptions{"a/b": {}}},
		"local":      {BaseURL: "http://localhost:8080/v1", Models: map[string]ModelOptions{"x": {}}},
	}}
	if _, err := s.Provider("openrouter", "z-ai/glm-5.2"); err != nil {
		t.Fatalf("an unlisted OpenRouter model was rejected: %v", err)
	}
	if _, err := s.Provider("local", "y"); err == nil {
		t.Fatal("an unlisted model on a non-OpenRouter endpoint was accepted")
	}
	routed := s.WithRoute("openrouter", "z-ai/glm-5.2", "wafer/us")
	p, err := routed.Provider("openrouter", "z-ai/glm-5.2")
	if err != nil || string(p.Extra) != `{"order":["wafer/us"],"allow_fallbacks":true}` {
		t.Fatalf("route not applied: %v %s", err, p.Extra)
	}
	if _, ok := s.Providers["openrouter"].Models["z-ai/glm-5.2"]; ok {
		t.Fatal("WithRoute modified the receiver")
	}
}
