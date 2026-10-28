package axon

import (
	"encoding/json"
	"strings"
	"sync"
)

// Tokenizers differ across routed models and no single local tokenizer fits
// all of them. Instead of guessing a fixed chars-per-token constant, every
// response's provider-reported prompt_tokens is compared with the bytes that
// were sent, and the observed bytes-per-token ratio is learned per model.
//
// Until a model has been observed, defaultBytesPerToken is used: lower than
// typical code/English (~3.5-4) so the first requests err towards counting
// too many tokens, never too few.
const (
	defaultBytesPerToken = 3.0
	minBytesPerToken     = 1.5
	maxBytesPerToken     = 5.0
	// Tiny requests (titles, probes) are dominated by chat-template overhead
	// and would skew the ratio; only sizeable payloads calibrate.
	minCalibrationBytes = 4096
	// tokenSafety inflates estimates so a calibrated ratio that drifts on a
	// different mix of text (Turkish prose vs. Go source) still stays under
	// the real limit.
	tokenSafety = 1.08
)

type tokenMeter struct {
	mu    sync.Mutex
	ratio map[string]float64
}

var meter = &tokenMeter{ratio: map[string]float64{}}

// A ":free" variant routes the same weights, so it shares the tokenizer.
func meterKey(model string) string { return strings.TrimSuffix(model, ":free") }

// observe records one real request. The "" key tracks the latest of any model
// and serves estimates that are not tied to one (session size thresholds).
func (t *tokenMeter) observe(model string, payloadBytes, promptTokens int) {
	if payloadBytes < minCalibrationBytes || promptTokens <= 0 {
		return
	}
	r := float64(payloadBytes) / float64(promptTokens)
	r = max(minBytesPerToken, min(maxBytesPerToken, r))
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, k := range []string{meterKey(model), ""} {
		if old, ok := t.ratio[k]; ok {
			t.ratio[k] = 0.7*old + 0.3*r
		} else {
			t.ratio[k] = r
		}
	}
}

func (t *tokenMeter) bytesPerToken(model string) float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	if r, ok := t.ratio[meterKey(model)]; ok {
		return r
	}
	if r, ok := t.ratio[""]; ok {
		return min(r, defaultBytesPerToken)
	}
	return defaultBytesPerToken
}

func (t *tokenMeter) tokens(model string, payloadBytes int) int {
	if payloadBytes <= 0 {
		return 0
	}
	return int(float64(payloadBytes)/t.bytesPerToken(model)*tokenSafety) + 1
}

// requestPayloadBytes is the part of a request that the provider tokenizes.
func requestPayloadBytes(req Request) (int, error) {
	data, err := json.Marshal(struct {
		Messages []Msg           `json:"messages"`
		Tools    []ToolSpec      `json:"tools,omitempty"`
		Format   json.RawMessage `json:"response_format,omitempty"`
	}{wireMessages(req.Messages), req.Tools, req.ResponseFormat})
	return len(data), err
}

// modelName reports the routed model id when the Model exposes one.
func modelName(m Model) string {
	if named, ok := m.(interface{ Model() string }); ok {
		return named.Model()
	}
	return ""
}
