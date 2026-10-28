package axon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func freeCatalog(present bool) string {
	if !present {
		return `{"data":[{"id":"vendor/model","canonical_slug":"same"}]}`
	}
	return `{"data":[{"id":"vendor/model","canonical_slug":"same"},{"id":"vendor/model:free","canonical_slug":"same","pricing":{"prompt":"0","completion":"0"},"supported_parameters":["tools","response_format"]}]}`
}

func freeFirstHarness(t *testing.T, handler http.HandlerFunc) *freeFirstClient {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	paid, err := NewClient(ClientConfig{Provider: Provider{BaseURL: srv.URL + "/v1", Model: "vendor/model", Extra: json.RawMessage(`{"data_collection":"deny"}`)}, RequestTimeout: time.Second, IdleTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return &freeFirstClient{paid: paid, catalogURL: srv.URL + "/v1/models", catalogHTTP: srv.Client(), firstOutputTimeout: time.Second}
}

func completion(w http.ResponseWriter, text string) {
	w.Header().Set("Content-Type", "text/event-stream")
	b, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]string{"content": text}, "finish_reason": "stop"}}})
	fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", b)
}

func TestFreeFirstRetriesFreeOnEveryRequestAfterRateLimit(t *testing.T) {
	var mu sync.Mutex
	lookups := 0
	freeCalls := 0
	paidCalls := 0
	f := freeFirstHarness(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == http.MethodGet {
			lookups++
			fmt.Fprint(w, freeCatalog(true))
			return
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["model"] == "vendor/model:free" {
			freeCalls++
			provider := body["provider"].(map[string]any)
			if provider["data_collection"] != "deny" || provider["require_parameters"] != true {
				t.Error("lost route constraints")
			}
			price := provider["max_price"].(map[string]any)
			if price["prompt"] != float64(0) || price["completion"] != float64(0) || price["request"] != float64(0) {
				t.Error("not free constrained")
			}
			if freeCalls == 1 {
				w.WriteHeader(429)
				return
			}
			completion(w, "FREE")
			return
		}
		paidCalls++
		completion(w, "PAID")
	})
	for _, want := range []string{"PAID", "FREE"} {
		m, err := f.Complete(context.Background(), Request{})
		if err != nil || m.Content != want {
			t.Fatalf("got %+v, %v; want %s", m, err, want)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if lookups != 1 || freeCalls != 2 || paidCalls != 1 {
		t.Fatalf("counts: %d %d %d", lookups, freeCalls, paidCalls)
	}
}

func TestFreeFirstCatalogChangesAreObservedAfterTTL(t *testing.T) {
	lookups := 0
	f := freeFirstHarness(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			lookups++
			fmt.Fprint(w, freeCatalog(lookups > 1))
			return
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		completion(w, body["model"].(string))
	})
	for _, want := range []string{"vendor/model", "vendor/model:free"} {
		m, err := f.Complete(context.Background(), Request{})
		if err != nil || m.Content != want {
			t.Fatalf("%+v %v", m, err)
		}
		f.fetchedAt = f.fetchedAt.Add(-catalogTTL) // the cached catalog expires
	}
}

func TestFreeFirstNeverLeaksPartialFreeOutputIntoPaidAnswer(t *testing.T) {
	for _, ending := range []string{"", `data: {"error":{"code":503,"message":"unavailable"}}`} {
		t.Run(fmt.Sprintf("ending-%d", len(ending)), func(t *testing.T) {
			f := freeFirstHarness(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					fmt.Fprint(w, freeCatalog(true))
					return
				}
				var body map[string]any
				json.NewDecoder(r.Body).Decode(&body)
				if body["model"] == "vendor/model:free" {
					fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"DISCARD\"}}]}\n\n%s\n\n", ending)
					return
				}
				completion(w, "PAID")
			})
			var shown strings.Builder
			m, err := f.Complete(context.Background(), Request{Stream: Stream{Token: func(s string) { shown.WriteString(s) }}})
			if err != nil || m.Content != "PAID" || shown.String() != "PAID" {
				t.Fatalf("%+v %v shown=%s", m, err, shown.String())
			}
		})
	}
}

func TestFreeFirstChecksIdentityPriceAndCapabilities(t *testing.T) {
	for _, catalog := range []string{
		strings.ReplaceAll(freeCatalog(true), `"prompt":"0"`, `"prompt":"1"`),
		strings.Replace(freeCatalog(true), `"id":"vendor/model:free","canonical_slug":"same"`, `"id":"vendor/model:free","canonical_slug":"other"`, 1),
		strings.ReplaceAll(freeCatalog(true), `["tools","response_format"]`, `[]`),
		`not JSON`,
	} {
		f := freeFirstHarness(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet {
				fmt.Fprint(w, catalog)
				return
			}
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if body["model"] != "vendor/model" {
				t.Error("unexpected free attempt")
			}
			completion(w, "PAID")
		})
		_, err := f.Complete(context.Background(), Request{Tools: []ToolSpec{{Name: "read"}}})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestFreeFirstCancellationDoesNotStartPaidRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	paidCalls := 0
	f := freeFirstHarness(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			fmt.Fprint(w, freeCatalog(true))
			return
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["model"] == "vendor/model:free" {
			cancel()
			<-r.Context().Done()
			return
		}
		paidCalls++
		completion(w, "PAID")
	})
	if _, err := f.Complete(ctx, Request{}); err != context.Canceled {
		t.Fatalf("cancel: %v", err)
	}
	if paidCalls != 0 {
		t.Fatal("paid request after cancellation")
	}
}

func TestFreeFirstExplicitFreeAndOtherProvidersAreUnchanged(t *testing.T) {
	for _, pair := range [][2]string{{"https://openrouter.ai/api/v1", "vendor/model:free"}, {"https://example.com/v1", "vendor/model"}, {"https://openrouter.ai/api/v1", "vendor/model:nitro"}} {
		s := DefaultSettings()
		s.Providers = map[string]Endpoint{"p": {BaseURL: pair[0], Models: map[string]ModelOptions{pair[1]: {}}}}
		m, err := s.NewFreeFirstClient("p", pair[1])
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := m.(*Client); !ok {
			t.Fatal("unexpected wrapper")
		}
	}
}

func TestFreeFirstCatalogDecodesPricingOverridesAndIsFetchedOnce(t *testing.T) {
	var fetches atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetches.Add(1)
		fmt.Fprint(w, `{"data":[{"id":"vendor/model","pricing":{"prompt":"0.000001","completion":"0.000002","overrides":[{"min_prompt_tokens":1000,"prompt":"0.000002"}]}}]}`)
	}))
	defer srv.Close()
	f := &freeFirstClient{catalogURL: srv.URL, catalogHTTP: srv.Client()}
	for i := 0; i < 3; i++ {
		models, ok := f.models(context.Background())
		if !ok || len(models) != 1 {
			t.Fatalf("catalog = %v, %v; want one model", models, ok)
		}
	}
	if n := fetches.Load(); n != 1 {
		t.Fatalf("catalog fetched %d times, want once within the TTL", n)
	}
	if v, present, ok := (&catalogModel{Pricing: map[string]json.RawMessage{"prompt": json.RawMessage(`"0"`)}}).price("prompt"); v != 0 || !present || !ok {
		t.Fatalf("price = %v %v %v", v, present, ok)
	}
}
