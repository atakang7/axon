package axon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// NewFreeFirstClient checks the live OpenRouter catalog for the exact model's
// :free sibling before Complete calls; the catalog is fetched at most once
// per catalogTTL (per failureTTL after a failed fetch). Explicit free models and other hosts
// are unchanged. This is opt-in; NewClient and free-only pruners stay unchanged.
func (s Settings) NewFreeFirstClient(endpoint, model string) (Model, error) {
	m, err := s.NewClient(endpoint, model)
	if err != nil {
		return nil, err
	}
	paid := m.(*Client)
	u, err := url.Parse(paid.baseURL)
	if err != nil || u.Scheme != "https" || u.Host != "openrouter.ai" || strings.Contains(model, ":") {
		return paid, nil
	}
	return &freeFirstClient{paid: paid, catalogURL: paid.baseURL + "/models", catalogHTTP: &http.Client{Timeout: 3 * time.Second}, firstOutputTimeout: 8 * time.Second}, nil
}

type freeFirstClient struct {
	paid               *Client
	catalogURL         string
	catalogHTTP        *http.Client
	firstOutputTimeout time.Duration

	mu        sync.Mutex
	catalog   []catalogModel
	catalogOK bool
	fetchedAt time.Time
}

// The catalog is 770 KB and changes rarely; fetching it before every request
// cost a download per model call.
const (
	catalogTTL = 10 * time.Minute
	failureTTL = time.Minute
)

type catalogModel struct {
	ID         string   `json:"id"`
	Canonical  string   `json:"canonical_slug"`
	Parameters []string `json:"supported_parameters"`
	// Pricing holds strings for each kind and other shapes besides them
	// (e.g. "overrides", a list), so values are decoded one by one.
	Pricing map[string]json.RawMessage `json:"pricing"`
}

// price is the pricing value for kind, and whether it is a number string.
func (m *catalogModel) price(kind string) (float64, bool, bool) {
	raw, ok := m.Pricing[kind]
	if !ok {
		return 0, false, false
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return 0, true, false
	}
	v, err := strconv.ParseFloat(s, 64)
	return v, true, err == nil
}

// models returns the catalog, fetching it when the cached copy is too old.
func (f *freeFirstClient) models(ctx context.Context) ([]catalogModel, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ttl := catalogTTL
	if !f.catalogOK {
		ttl = failureTTL
	}
	if !f.fetchedAt.IsZero() && time.Since(f.fetchedAt) < ttl {
		return f.catalog, f.catalogOK
	}
	f.catalog, f.catalogOK = f.fetchCatalog(ctx)
	f.fetchedAt = time.Now()
	return f.catalog, f.catalogOK
}

func (f *freeFirstClient) fetchCatalog(ctx context.Context) ([]catalogModel, bool) {
	lookupCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	r, err := http.NewRequestWithContext(lookupCtx, http.MethodGet, f.catalogURL, nil)
	if err != nil {
		return nil, false
	}
	r.Header.Set("Cache-Control", "no-cache")
	resp, err := f.catalogHTTP.Do(r)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}
	var catalog struct {
		Data []catalogModel `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&catalog); err != nil {
		return nil, false
	}
	return catalog.Data, true
}

func (f *freeFirstClient) Complete(ctx context.Context, req Request) (*Msg, error) {
	if err := checkRequestBudget(req, f.paid.Model()); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	free, reason := f.freeClient(ctx, req)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if free == nil {
		return f.paidComplete(ctx, req, reason)
	}
	if req.Stream.Route != nil {
		req.Stream.Route(free.Model(), "trying exact free variant")
	}
	freeCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	// Only first output has a short deadline. Once generation begins, keep
	// the configured request/idle limits so long valid answers can finish.
	timer := time.AfterFunc(f.firstOutputTimeout, cancel)
	defer timer.Stop()
	r := req
	r.Stream = Stream{
		Token:     func(string) { timer.Stop() },
		Reasoning: func(string) { timer.Stop() },
		ToolArgs:  func(string, string) { timer.Stop() },
		Usage:     req.Stream.Usage,
		Cached:    req.Stream.Cached,
	}
	msg, err := free.Complete(freeCtx, r)
	timer.Stop()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil || unusableReply(msg) != "" {
		return f.paidComplete(ctx, req, "free variant unavailable, limited, or unusable")
	}
	// Buffer the free attempt so a partial response cannot be spliced into a
	// paid continuation. Tools execute only after Complete returns a winner.
	if req.Stream.Reasoning != nil && msg.Reasoning != "" {
		req.Stream.Reasoning(msg.Reasoning)
	}
	if req.Stream.Token != nil && msg.Content != "" {
		req.Stream.Token(msg.Content)
	}
	if req.Stream.ToolArgs != nil {
		for _, tc := range msg.ToolCalls {
			req.Stream.ToolArgs(tc.Function.Name, tc.Function.Arguments)
		}
	}
	return msg, nil
}

func (f *freeFirstClient) paidComplete(ctx context.Context, req Request, reason string) (*Msg, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if req.Stream.Route != nil {
		req.Stream.Route(f.paid.Model(), reason)
	}
	return f.paid.Complete(ctx, req)
}

func (f *freeFirstClient) freeClient(ctx context.Context, req Request) (*Client, string) {
	data, ok := f.models(ctx)
	if !ok {
		return nil, "catalog unavailable"
	}
	var base, free *catalogModel
	for i := range data {
		m := &data[i]
		if m.ID == f.paid.Model() {
			base = m
		}
		if m.ID == f.paid.Model()+":free" {
			free = m
		}
	}
	if base == nil || free == nil {
		return nil, "no exact free variant in current catalog"
	}
	if base.Canonical != "" && free.Canonical != "" && base.Canonical != free.Canonical {
		return nil, "free variant resolves to a different model revision"
	}
	for _, kind := range []string{"prompt", "completion"} {
		if v, _, ok := free.price(kind); !ok || v != 0 {
			return nil, "free pricing not confirmed"
		}
	}
	if v, present, ok := free.price("request"); present && (!ok || v != 0) {
		return nil, "free request pricing not confirmed"
	}
	supported := map[string]bool{}
	for _, p := range free.Parameters {
		supported[p] = true
	}
	if len(req.Tools) > 0 && !supported["tools"] {
		return nil, "free variant lacks required tools"
	}
	if len(req.ResponseFormat) > 0 && !supported["response_format"] {
		return nil, "free variant lacks required response format"
	}
	cfg := f.paid.cfg
	cfg.Provider.Model = free.ID
	options := map[string]any{}
	if len(cfg.Provider.Extra) > 0 {
		if err := json.Unmarshal(cfg.Provider.Extra, &options); err != nil {
			return nil, "invalid routing options"
		}
	}
	if options == nil {
		return nil, "invalid routing options"
	}
	options["max_price"] = map[string]float64{"prompt": 0, "completion": 0, "request": 0}
	options["require_parameters"] = true
	cfg.Provider.Extra, _ = json.Marshal(options)
	client, err := NewClient(cfg)
	if err != nil {
		return nil, fmt.Sprintf("free client unavailable: %v", err)
	}
	client.http = f.paid.http
	client.requireCompleteStream = true
	return client, ""
}
