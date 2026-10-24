package axon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPrunerFallbackOnlyOnAvailabilityAndRetriesFree(t *testing.T) {
	paidCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			fmt.Fprint(w, `{"data":{"endpoints":[]}}`)
			return
		}
		paidCalls++
		var body struct {
			Provider map[string]any `json:"provider"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		completion(w, "paid decision")
	}))
	defer server.Close()
	paid, _ := NewClient(ClientConfig{Provider: Provider{BaseURL: server.URL, Model: "fallback"}})
	freeCalls := 0
	p := &prunerFallback{paid: paid, metadata: server.Client(), freeTimeout: time.Second, free: funcModel{fn: func(context.Context, Request) (*Msg, error) {
		freeCalls++
		if freeCalls == 1 {
			return nil, &APIError{Status: 429}
		}
		return &Msg{Content: "free decision"}, nil
	}}}
	for i := 0; i < 2; i++ {
		if _, err := p.Complete(context.Background(), Request{}); err != nil {
			t.Fatal(err)
		}
	}
	if freeCalls != 2 || paidCalls != 1 {
		t.Fatal(freeCalls, paidCalls)
	}
	p.free = funcModel{fn: func(context.Context, Request) (*Msg, error) { return nil, &APIError{Status: 401} }}
	if _, err := p.Complete(context.Background(), Request{}); err == nil || paidCalls != 1 {
		t.Fatal("auth error spent paid tokens")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Complete(ctx, Request{}); err != context.Canceled || paidCalls != 1 {
		t.Fatal("cancelled request spent paid tokens")
	}
}

func TestPrunerProviderRankingWeightsInputAndFiltersCapabilities(t *testing.T) {
	endpoint := func(tag, in, out string) prunerEndpoint {
		return prunerEndpoint{Tag: tag, Context: 200000, Parameters: []string{"response_format"}, Pricing: map[string]string{"prompt": in, "completion": out}}
	}
	endpoints := []prunerEndpoint{endpoint("cheap-output", "0.00000019", "0.00000001"), endpoint("cheap-input", "0.00000002", "0.0000005"), endpoint("expensive", "0.000001", "0.0000001")}
	noJSON := endpoint("no-json", "0", "0")
	noJSON.Parameters = nil
	endpoints = append(endpoints, noJSON)
	r := Request{Messages: []Msg{{Role: "user", Content: string(make([]byte, 8000))}}, MaxTokens: 100, ResponseFormat: json.RawMessage(`{"type":"json_object"}`)}
	tags := rankPrunerEndpoints(endpoints, r)
	if len(tags) != 2 || tags[0] != "cheap-input" {
		t.Fatal(tags)
	}
}

func TestPrunerFallbackFactoryCapsPriceAndSeparatesReviewers(t *testing.T) {
	s := DefaultSettings()
	s.Providers = map[string]Endpoint{"router": {BaseURL: "https://openrouter.ai/api/v1", Models: map[string]ModelOptions{"vendor/model:free": {}}}}
	a, err := s.NewPrunerClient("router", "vendor/model:free", 0)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.NewPrunerClient("router", "vendor/model:free", 1)
	if err != nil {
		t.Fatal(err)
	}
	if a.(*prunerFallback).paid.Model() == b.(*prunerFallback).paid.Model() {
		t.Fatal("same fallback counted twice")
	}
	var extra struct {
		MaxPrice map[string]float64 `json:"max_price"`
	}
	json.Unmarshal(a.(*prunerFallback).paid.cfg.Provider.Extra, &extra)
	if extra.MaxPrice["prompt"] != 0.2 || extra.MaxPrice["completion"] != 0.6 || extra.MaxPrice["request"] != 0 {
		t.Fatal(extra)
	}
}
