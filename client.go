package axon

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
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
}

func (e *APIError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("API error %d", e.Status)
	}

	return fmt.Sprintf("API error %d: %s", e.Status, e.Body)
}

// Client is an OpenAI-compatible streaming Model. Safe for concurrent use.
type Client struct {
	http    *http.Client
	baseURL string
	cfg     ClientConfig
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
		return nil, &APIError{Status: resp.StatusCode, Body: strings.TrimSpace(string(detail))}
	}

	return readStream(ctx, resp.Body, req.Stream, cancel, c.cfg.IdleTimeout)
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
		"model":      c.cfg.Provider.Model,
		"messages":   req.Messages,
		"stream":     true,
		"max_tokens": maxTokens,
	}

	if len(tools) > 0 {
		body["tools"] = tools
		body["parallel_tool_calls"] = true
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
	content          strings.Builder
	reasoningContent strings.Builder
	toolArgs         map[int]*strings.Builder
	toolMeta         map[int]ToolCall
}

// readStream consumes the SSE body until the server closes it, applying an
// idle timeout so a silent provider fails fast instead of hanging the turn.
func readStream(ctx context.Context, body io.Reader, stream Stream, cancel context.CancelFunc, idleTimeout time.Duration) (*Msg, error) {
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
		return nil, fmt.Errorf("stream stalled: no data for %s (provider went silent mid-response)", idleTimeout)
	}

	if err := sc.Err(); err != nil {
		return nil, err
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return out.message(), nil
}

// consume applies one SSE line. Anything unparseable is skipped: a malformed
// keep-alive or comment must not abort an otherwise healthy stream.
func (r *reply) consume(text string, stream Stream) {
	data := strings.TrimPrefix(text, "data: ")
	if data == "" || data == "[DONE]" {
		return
	}

	var chunk struct {
		Choices []struct {
			Delta struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
				ToolCalls        []struct {
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
	}

	if json.Unmarshal([]byte(data), &chunk) != nil || len(chunk.Choices) == 0 {
		return
	}

	delta := chunk.Choices[0].Delta

	if delta.ReasoningContent != "" {
		r.reasoningContent.WriteString(delta.ReasoningContent)
		if stream.Reasoning != nil {
			stream.Reasoning(delta.ReasoningContent)
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
