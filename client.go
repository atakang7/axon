package axon

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// ---------------------------------------------------------------------------
// Configuration & Types
// ---------------------------------------------------------------------------

// errorBodyLimit caps how much of a failed response is kept: enough for a
// provider's JSON error, short of an HTML error page from a proxy.
const errorBodyLimit = 4096

// ClientConfig configures the shipped Model implementation.
//
// Every zero value falls back to DefaultSettings, so a client constructed with
// only a Provider behaves exactly as the documented defaults say.
type ClientConfig struct {
	// Provider is the endpoint, model name and credentials. Required.
	Provider Provider

	// MaxTokens is the default cap when a Request does not set its own.
	MaxTokens int

	// ReasoningEffort is forwarded as reasoning.effort.
	ReasoningEffort string

	// ExcludeReasoning asks the provider to omit reasoning tokens entirely.
	ExcludeReasoning bool

	// RequestTimeout bounds one whole streamed response.
	RequestTimeout time.Duration

	// IdleTimeout bounds the gap between two chunks, so a provider that goes
	// silent mid-response fails fast rather than holding the turn for the
	// whole of RequestTimeout.
	IdleTimeout time.Duration
}

// APIError is a non-2xx response from the provider.
//
// It carries the status as a number so callers can branch on it. The retry
// loop has to tell a rate limit from a bad request, and deciding that by
// matching the text of a message makes retry behaviour depend on wording
// nobody thinks of as load-bearing.
type APIError struct {
	// Status is the HTTP status code.
	Status int

	// Body is the response body, truncated to errorBodyLimit. Providers put
	// the actual reason here — an unknown model, an expired key — and
	// discarding it costs hours.
	Body string

	// RetryAfter is the duration the server asked us to wait before retrying,
	// parsed from the Retry-After response header. Zero means the server did
	// not provide one, so the caller should fall back to its own backoff.
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("API error %d", e.Status)
	}

	return fmt.Sprintf("API error %d: %s", e.Status, e.Body)
}

// Client is an OpenAI-compatible streaming Model. Safe for concurrent use.
type Client struct {
	requireCompleteStream bool
	http                  *http.Client
	baseURL               string
	cfg                   ClientConfig
}

// ---------------------------------------------------------------------------
// Client Constructors & Methods
// ---------------------------------------------------------------------------

// NewClient builds a Model for any OpenAI-compatible endpoint.
func NewClient(cfg ClientConfig) (*Client, error) {
	url := strings.TrimRight(cfg.Provider.BaseURL, "/")
	if url == "" {
		return nil, fmt.Errorf("provider %q has no base_url", cfg.Provider.Name)
	}

	if !strings.HasSuffix(url, "/v1") {
		url += "/v1"
	}

	defaults := DefaultSettings().Model

	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = defaults.MaxTokens
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = defaults.RequestTimeout.Std()
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = defaults.IdleTimeout.Std()
	}

	return &Client{
		http:    &http.Client{Timeout: cfg.RequestTimeout},
		baseURL: url,
		cfg:     cfg,
	}, nil
}

// Model reports which model this client talks to.
func (c *Client) Model() string {
	return c.cfg.Provider.Model
}

// Complete sends one request and assembles the reply, invoking req.Stream as output arrives.
func (c *Client) Complete(ctx context.Context, req Request) (*Msg, error) {
	if err := checkRequestBudget(req, c.Model()); err != nil {
		return nil, err
	}
	// Every reply's prompt_tokens calibrates the meter for this model.
	if payload, err := requestPayloadBytes(req); err == nil {
		report := req.Stream.Usage
		req.Stream.Usage = func(prompt, completion int, cost float64) {
			meter.observe(c.Model(), payload, prompt)
			if report != nil {
				report(prompt, completion, cost)
			}
		}
	}
	body, err := c.requestBody(req)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/chat/completions",
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("Cache-Control", "no-cache")

	if key := c.cfg.Provider.APIKey; key != "" {
		httpReq.Header.Set("Authorization", "Bearer "+key)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, errorBodyLimit))
		return nil, &APIError{
			Status:     resp.StatusCode,
			Body:       strings.TrimSpace(string(detail)),
			RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
		}
	}

	return readStream(ctx, resp.Body, req.Stream, cancel, c.cfg.IdleTimeout, c.requireCompleteStream)
}

// wireMessages is the conversation as the provider receives it. A reply's
// reasoning is kept in the log for the user to read, never sent back: it would
// be paid again on every later request.
func wireMessages(msgs []Msg) []Msg {
	for i := range msgs {
		if msgs[i].Reasoning == "" {
			continue
		}
		out := append([]Msg(nil), msgs...)
		for j := i; j < len(out); j++ {
			out[j].Reasoning = ""
		}
		return out
	}
	return msgs
}

// cacheMarked adds prompt-cache breakpoints for models that cache only where
// the request marks it (Anthropic's, directly or through OpenRouter); every
// other model caches a repeated prefix by itself and gets msgs unchanged.
// Without the marks every request paid the whole context at the full price:
// the eval of 2026-10-28 measured a 0% cache hit for Claude against 74-84%
// for MiMo. The breakpoints are the system message, which with the tools
// before it is the stable prefix, and the last two messages with text, so
// each request reads the previous one's prefix from the cache. Anthropic
// allows four.
func cacheMarked(msgs []Msg, model string) any {
	if !needsCacheMarks(model) || len(msgs) == 0 {
		return msgs
	}
	raw, err := json.Marshal(msgs)
	if err != nil {
		return msgs
	}
	var out []map[string]any
	if json.Unmarshal(raw, &out) != nil {
		return msgs
	}
	mark := func(i int) bool {
		text, ok := out[i]["content"].(string)
		if !ok || text == "" {
			return false
		}
		out[i]["content"] = []map[string]any{{"type": "text", "text": text, "cache_control": map[string]string{"type": "ephemeral"}}}
		return true
	}
	if msgs[0].Role == "system" {
		mark(0)
	}
	for i, marked := len(out)-1, 0; i > 0 && marked < 2; i-- {
		if mark(i) {
			marked++
		}
	}
	return out
}

func needsCacheMarks(model string) bool {
	m := strings.ToLower(model)
	return strings.HasPrefix(m, "anthropic/") || strings.HasPrefix(m, "claude")
}

func (c *Client) requestBody(req Request) ([]byte, error) {
	tools := make([]map[string]any, len(req.Tools))
	for i, t := range req.Tools {
		tools[i] = map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  t.Schema,
			},
		}
	}

	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = c.cfg.MaxTokens
	}

	body := map[string]any{
		"model":          c.cfg.Provider.Model,
		"messages":       cacheMarked(wireMessages(req.Messages), c.cfg.Provider.Model),
		"stream":         true,
		"stream_options": map[string]any{"include_usage": true},
		"max_tokens":     maxTokens,
	}

	if len(tools) > 0 {
		body["tools"] = tools
		body["parallel_tool_calls"] = true
	}

	if len(req.ResponseFormat) > 0 {
		body["response_format"] = req.ResponseFormat
	}

	if c.cfg.ReasoningEffort != "" || c.cfg.ExcludeReasoning {
		reasoning := map[string]any{}
		if c.cfg.ReasoningEffort != "" {
			reasoning["effort"] = c.cfg.ReasoningEffort
		}
		if c.cfg.ExcludeReasoning {
			reasoning["exclude"] = true
			body["include_reasoning"] = false
		}
		body["reasoning"] = reasoning
	}

	if len(c.cfg.Provider.Extra) > 0 {
		body["provider"] = c.cfg.Provider.Extra
	}

	return json.Marshal(body)
}

// ---------------------------------------------------------------------------
// Server-Sent Events (SSE) Processing
// ---------------------------------------------------------------------------

// reply accumulates a streamed response. Tool calls arrive as fragments keyed by index.
type reply struct {
	streamErr        error
	complete         bool
	truncated        bool
	content          strings.Builder
	reasoningContent strings.Builder
	toolArgs         map[int]*strings.Builder
	toolMeta         map[int]ToolCall
	usage            usage
}

// usage is the token accounting carried by a stream's final chunk, when the
// provider includes it. Providers disagree on field names, so this is the
// normalized shape; parsing normalizes into it. Cost is the USD charge for
// the call when the provider reports one (OpenRouter fills it), zero otherwise.
type usage struct {
	PromptTokens     int
	CompletionTokens int
	Cost             float64
	CachedTokens     int
}

// reportUsage delivers any usage the stream carried to the caller, once.
// Cached is reported before Usage so a caller can fold both into one record.
func (r *reply) reportUsage(stream Stream) {
	if stream.Cached != nil && r.usage.PromptTokens != 0 {
		stream.Cached(r.usage.CachedTokens)
	}
	if stream.Usage != nil && (r.usage.PromptTokens != 0 || r.usage.CompletionTokens != 0 || r.usage.Cost != 0) {
		stream.Usage(r.usage.PromptTokens, r.usage.CompletionTokens, r.usage.Cost)
	}
}

// ErrStreamStalled is a provider that went silent mid-response; retryable.
var ErrStreamStalled = errors.New("stream stalled")

// readStream consumes the SSE body until the server closes it, applying an
// idle timeout so a silent provider fails fast instead of hanging the turn.
func readStream(ctx context.Context, body io.Reader, stream Stream, cancel context.CancelFunc, idleTimeout time.Duration, requireComplete ...bool) (*Msg, error) {
	out := &reply{
		toolArgs: map[int]*strings.Builder{},
		toolMeta: map[int]ToolCall{},
	}

	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 1<<20), 1<<20)

	var stalled atomic.Bool
	idle := time.AfterFunc(idleTimeout, func() {
		stalled.Store(true)
		cancel()
	})
	defer idle.Stop()

	for sc.Scan() {
		idle.Reset(idleTimeout)
		out.consume(sc.Text(), stream)
	}

	if stalled.Load() {
		return nil, fmt.Errorf("%w: no data for %s (provider went silent mid-response)", ErrStreamStalled, idleTimeout)
	}

	if err := sc.Err(); err != nil {
		return nil, err
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	out.reportUsage(stream)
	if out.streamErr != nil {
		return nil, out.streamErr
	}
	if len(requireComplete) > 0 && requireComplete[0] && (!out.complete || out.truncated) {
		return nil, fmt.Errorf("incomplete provider response")
	}
	return out.message(), nil
}

// consume applies one SSE line. Anything unparseable is skipped: a malformed
// keep-alive or comment must not abort an otherwise healthy stream.
func (r *reply) consume(text string, stream Stream) {
	data := strings.TrimPrefix(text, "data: ")
	if data == "[DONE]" {
		r.complete = true
		return
	}
	if data == "" {
		return
	}

	var chunk struct {
		Error   json.RawMessage `json:"error"`
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Delta        struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
				// OpenRouter streams a model's thinking as "reasoning";
				// reasoning_content is the DeepSeek-native name.
				Reasoning string `json:"reasoning"`
				ToolCalls []struct {
					Index    int    `json:"index"`
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"delta"`
		} `json:"choices"`

		// usage arrives on the stream's final chunk, keyed separately from the
		// per-choice delta. Providers disagree on the field names, so collect
		// it verbatim and normalize below.
		Usage *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			// OpenRouter sends cost as a plain float, not an object.
			Cost                float64 `json:"cost"`
			PromptTokensDetails struct {
				CachedTokens int `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
		} `json:"usage"`
	}

	if err := json.Unmarshal([]byte(data), &chunk); err != nil {
		return
	}
	if len(chunk.Error) > 0 && string(chunk.Error) != "null" {
		detail := string(chunk.Error)
		if len(detail) > errorBodyLimit {
			detail = detail[:errorBodyLimit]
		}
		r.streamErr = fmt.Errorf("provider stream error: %s", detail)
		return
	}

	// usage arrives on the stream's final chunk, often with an empty choices
	// array. Parse it before the choices-length guard below, or a usage-only
	// chunk is dropped on the floor and the caller never sees token totals or
	// cost.
	if chunk.Usage != nil {
		r.usage.PromptTokens = chunk.Usage.PromptTokens
		r.usage.CompletionTokens = chunk.Usage.CompletionTokens
		r.usage.Cost = chunk.Usage.Cost
		r.usage.CachedTokens = chunk.Usage.PromptTokensDetails.CachedTokens
	}

	if len(chunk.Choices) == 0 {
		return
	}
	if reason := chunk.Choices[0].FinishReason; reason != "" {
		r.complete = true
		if reason == "length" || reason == "content_filter" {
			r.truncated = true
		}
	}

	delta := chunk.Choices[0].Delta

	thinking := delta.ReasoningContent
	if thinking == "" {
		thinking = delta.Reasoning
	}
	if thinking != "" {
		r.reasoningContent.WriteString(thinking)
		if stream.Reasoning != nil {
			stream.Reasoning(thinking)
		}
	}

	if delta.Content != "" {
		r.content.WriteString(delta.Content)
		if stream.Token != nil {
			stream.Token(delta.Content)
		}
	}

	for _, tc := range delta.ToolCalls {
		if _, seen := r.toolMeta[tc.Index]; !seen {
			meta := ToolCall{ID: tc.ID, Type: tc.Type}
			meta.Function.Name = tc.Function.Name
			r.toolMeta[tc.Index] = meta
			r.toolArgs[tc.Index] = &strings.Builder{}
		}

		r.toolArgs[tc.Index].WriteString(tc.Function.Arguments)

		if stream.ToolArgs != nil && tc.Function.Arguments != "" {
			stream.ToolArgs(r.toolMeta[tc.Index].Function.Name, tc.Function.Arguments)
		}
	}
}

// message assembles the finished assistant message, ordering tool calls by the
// index the provider gave them so the sequence is stable.
func (r *reply) message() *Msg {
	finalContent := r.content.String()
	finalReasoning := r.reasoningContent.String()

	if len(r.toolMeta) == 0 {
		return &Msg{
			Role:      "assistant",
			Content:   finalContent,
			Reasoning: finalReasoning,
		}
	}

	indices := make([]int, 0, len(r.toolMeta))
	for i := range r.toolMeta {
		indices = append(indices, i)
	}
	sort.Ints(indices)

	calls := make([]ToolCall, 0, len(indices))
	for _, i := range indices {
		tc := r.toolMeta[i]
		tc.Function.Arguments = r.toolArgs[i].String()
		if tc.Function.Arguments == "" {
			// A tool called with no arguments still needs valid JSON, or the
			// provider rejects the follow-up request.
			tc.Function.Arguments = "{}"
		}
		calls = append(calls, tc)
	}

	return &Msg{
		Role:      "assistant",
		Content:   finalContent,
		Reasoning: finalReasoning,
		ToolCalls: calls,
	}
}

// parseRetryAfter converts a Retry-After header value to a duration. Most
// providers send seconds ("5"), some send an HTTP-date; either way a value
// we cannot parse yields zero so the caller falls back to its own backoff.
func parseRetryAfter(v string) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}

	if secs, err := strconv.Atoi(v); err == nil {
		if secs > 0 {
			return time.Duration(secs) * time.Second
		}
		return 0
	}

	if t, err := http.ParseTime(v); err == nil {
		d := time.Until(t)
		if d > 0 {
			return d
		}
	}

	return 0
}
