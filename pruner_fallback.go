package axon

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// NewPrunerClient retains a free primary, with one bounded paid fallback.
// Pool slots use distinct fallback models to avoid counting one model twice.
func (s Settings) NewPrunerClient(endpoint, model string, slot ...int) (Model, error) {
	free, err := s.NewFreePrunerClient(endpoint, model)
	if err != nil {
		return nil, err
	}
	primary := free.(*Client)
	cfg := primary.cfg
	cfg.Provider.Model = "z-ai/glm-5.3-flash"
	if len(slot) > 0 && slot[0]%2 == 1 {
		cfg.Provider.Model = "qwen/qwen3.7-flash"
	}
	var extra map[string]any
	if err := json.Unmarshal(cfg.Provider.Extra, &extra); err != nil {
		return nil, err
	}
	extra["max_price"] = map[string]float64{"prompt": 0.20, "completion": 0.60, "request": 0}
	extra["sort"] = "price"
	cfg.Provider.Extra, _ = json.Marshal(extra)
	// A whole-context review is a long prompt with a long JSON answer; a
	// 30-second cap turned slow-but-working replies into failures.
	cfg.RequestTimeout = 150 * time.Second
	cfg.IdleTimeout = 30 * time.Second
	paid, err := NewClient(cfg)
	if err != nil {
		return nil, err
	}
	return &prunerFallback{free: primary, paid: paid, metadata: &http.Client{Timeout: 3 * time.Second}, freeTimeout: 25 * time.Second}, nil
}

type prunerFallback struct {
	free     Model
	paid     *Client
	metadata *http.Client
	// freeTimeout bounds the wait for the free model's first output, not the
	// whole reply: reading a 60K-token context legitimately takes longer than
	// a fixed total deadline allowed, which sent working calls to the paid
	// fallback. Once output starts, the client's own limits apply.
	freeTimeout time.Duration
}

func (p *prunerFallback) Complete(ctx context.Context, r Request) (*Msg, error) {
	if err := checkRequestBudget(r, p.paid.Model()); err != nil {
		return nil, err
	}
	freeCtx, cancel := context.WithCancelCause(ctx)
	timer := time.AfterFunc(p.freeTimeout, func() { cancel(context.DeadlineExceeded) })
	free := r
	free.Stream = Stream{
		Token:     func(string) { timer.Stop() },
		Reasoning: func(string) { timer.Stop() },
		Usage:     r.Stream.Usage,
	}
	reply, err := p.free.Complete(freeCtx, free)
	timer.Stop()
	if err != nil && errors.Is(context.Cause(freeCtx), context.DeadlineExceeded) {
		err = context.DeadlineExceeded
	}
	cancel(nil)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err == nil {
		return reply, nil
	}
	if !prunerUnavailable(err) {
		return nil, err
	}
	paid := p.rankedClient(ctx, r)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if r.Stream.Route != nil {
		r.Stream.Route(paid.Model(), "pruner free endpoint unavailable; bounded-price fallback")
	}
	r.MaxTokens = min(r.MaxTokens, 8192)
	if r.MaxTokens <= 0 {
		r.MaxTokens = 8192
	}
	return paid.Complete(ctx, r)
}

func prunerUnavailable(err error) bool {
	var api *APIError
	if errors.As(err, &api) {
		return api.Status == 404 || api.Status == 408 || api.Status == 429 || api.Status >= 500
	}
	var network net.Error
	return errors.Is(err, context.DeadlineExceeded) || errors.As(err, &network) || errors.Is(err, io.ErrUnexpectedEOF)
}

type prunerEndpoint struct {
	Tag        string            `json:"tag"`
	Status     int               `json:"status"`
	Context    int               `json:"context_length"`
	Parameters []string          `json:"supported_parameters"`
	Pricing    map[string]string `json:"pricing"`
}

func rankPrunerEndpoints(endpoints []prunerEndpoint, r Request) []string {
	budget, _ := RequestInputBudget(r)
	input := float64(budget)
	output := r.MaxTokens
	if output <= 0 || output > 8192 {
		output = 8192
	}
	type priced struct {
		tag  string
		cost float64
	}
	var ranked []priced
	for _, e := range endpoints {
		if e.Status != 0 || e.Tag == "" || e.Context < budget+output {
			continue
		}
		// Do not trade away precision solely to win the cheapest advertised route.
		if strings.Contains(e.Tag, "fp4") || strings.Contains(e.Tag, "int4") {
			continue
		}
		if len(r.ResponseFormat) > 0 {
			supported := false
			for _, v := range e.Parameters {
				if v == "response_format" {
					supported = true
				}
			}
			if !supported {
				continue
			}
		}
		in, err := strconv.ParseFloat(e.Pricing["prompt"], 64)
		if err != nil || in < 0 || in > 0.20/1e6 {
			continue
		}
		out, err := strconv.ParseFloat(e.Pricing["completion"], 64)
		if err != nil || out < 0 || out > 0.60/1e6 {
			continue
		}
		if v := e.Pricing["request"]; v != "" {
			fee, err := strconv.ParseFloat(v, 64)
			if err != nil || fee != 0 {
				continue
			}
		}
		ranked = append(ranked, priced{e.Tag, input*in + float64(output)*out})
	}
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].cost < ranked[j].cost })
	tags := []string{}
	for _, v := range ranked {
		tags = append(tags, v.tag)
	}
	return tags
}

func (p *prunerFallback) rankedClient(ctx context.Context, r Request) *Client {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.paid.baseURL+"/models/"+p.paid.Model()+"/endpoints", nil)
	if err != nil {
		return p.paid
	}
	resp, err := p.metadata.Do(req)
	if err != nil {
		return p.paid
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return p.paid
	}
	var catalog struct {
		Data struct {
			Endpoints []prunerEndpoint `json:"endpoints"`
		} `json:"data"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&catalog) != nil {
		return p.paid
	}
	tags := rankPrunerEndpoints(catalog.Data.Endpoints, r)
	if len(tags) == 0 {
		return p.paid
	}
	cfg := p.paid.cfg
	var extra map[string]any
	if json.Unmarshal(cfg.Provider.Extra, &extra) != nil {
		return p.paid
	}
	// Respect account/user allowlists; only reorder eligible endpoint routing.
	if _, explicit := extra["order"]; explicit {
		return p.paid
	}
	extra["order"] = tags
	extra["allow_fallbacks"] = true
	cfg.Provider.Extra, _ = json.Marshal(extra)
	client, err := NewClient(cfg)
	if err != nil {
		return p.paid
	}
	return client
}

var _ Model = (*prunerFallback)(nil)
