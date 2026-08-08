package llm

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

	axon "github.com/atakang7/axon"
)

// The vocabulary lives in the public root package so embedders can read its
// documentation; these aliases keep this file readable.
type (
	Model    = axon.Model
	Request  = axon.Request
	Stream   = axon.Stream
	Provider = axon.Provider
	Msg      = axon.Msg
	ToolCall = axon.ToolCall
	ToolSpec = axon.ToolSpec
)

// ---------------------------------------------------------------------------
// The OpenAI-compatible implementation
// ---------------------------------------------------------------------------

// ClientConfig configures the shipped Model implementation.
type ClientConfig struct {
	// Provider is the endpoint, model name and credentials. Required.
	Provider Provider

	// MaxTokens is the default cap when a Request does not set its own.
	// Zero uses defaultMaxTokens. Lower it for budget-sensitive providers
	// that reject very large caps.
	MaxTokens int

	// ReasoningEffort is forwarded as OpenRouter/OpenAI-style
	// reasoning.effort ("none", "minimal", "low", …). Use "none" for fast
	// tool-use runs on models that otherwise think too long before acting.
	ReasoningEffort string

	// ExcludeReasoning asks the provider to omit reasoning tokens entirely.
	ExcludeReasoning bool
}

const defaultMaxTokens = 20000

// Client is an OpenAI-compatible streaming Model. It is safe for concurrent
// use: every request carries its own state, and nothing here is mutated after
// construction.
type Client struct {
	http    *http.Client
	baseURL string
	cfg     ClientConfig
}

// NewClient builds a Model for any OpenAI-compatible endpoint.
func NewClient(cfg ClientConfig) (*Client, error) {
	url := strings.TrimRight(cfg.Provider.BaseURL, "/")
	if url == "" {
		return nil, fmt.Errorf("provider %q has no base_url", cfg.Provider.Name)
	}
	if !strings.HasSuffix(url, "/v1") {
		url += "/v1"
	}
	return &Client{
		http:    &http.Client{Timeout: 30 * time.Minute},
		baseURL: url,
		cfg:     cfg,
	}, nil
}

// Model reports which model this client talks to. Useful for logging and for
// the session header.
func (c *Client) Model() string { return c.cfg.Provider.Model }

// idleTimeout bounds silence mid-stream. Without it a provider that stops
// sending without closing the connection would hold the turn until the HTTP
// client's own 30-minute timeout.
const idleTimeout = 20 * time.Second

// Complete sends one request and assembles the reply, invoking req.Stream as
// output arrives.
func (c *Client) Complete(ctx context.Context, req Request) (*Msg, error) {
	body, err := c.requestBody(req)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	// Without Accept: text/event-stream, some OpenAI-compatible routers
	// (OpenRouter in particular) buffer the whole SSE response server-side and
	// flush it as one chunk, making `stream: true` behave like a non-streaming
	// call. Setting it forces true incremental delivery.
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
		// Include the body: a bare status tells you nothing about which of a
		// dozen things the provider objected to. Reading only the first line
		// was worse than useless on the providers that answer with pretty
		// printed JSON — the message was always "{". Bounded so a provider that
		// returns an HTML error page cannot flood the log.
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("API error %s: %s", resp.Status, strings.TrimSpace(string(detail)))
	}
	return readStream(ctx, resp.Body, req.Stream, cancel)
}

func (c *Client) requestBody(req Request) ([]byte, error) {
	tools := make([]map[string]any, len(req.Tools))
	for i, t := range req.Tools {
		tools[i] = map[string]any{"type": "function", "function": map[string]any{
			"name": t.Name, "description": t.Description, "parameters": t.Schema,
		}}
	}

	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = c.cfg.MaxTokens
	}
	if maxTokens == 0 {
		maxTokens = defaultMaxTokens
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
			body["include_reasoning"] = false // legacy OpenRouter compatibility
		}
		body["reasoning"] = reasoning
	}
	if len(c.cfg.Provider.Extra) > 0 {
		body["provider"] = c.cfg.Provider.Extra
	}
	return json.Marshal(body)
}

// ---------------------------------------------------------------------------
// SSE
// ---------------------------------------------------------------------------

// reply accumulates a streamed response. Tool calls arrive as fragments keyed
// by index and are assembled at the end.
type reply struct {
	content          strings.Builder
	reasoningContent strings.Builder
	toolArgs         map[int]*strings.Builder
	toolMeta         map[int]ToolCall
}

// readStream consumes the SSE body until the server closes it, applying an
// idle timeout so a silent provider fails fast instead of hanging the turn.
// cancel aborts the in-flight request when that happens.
//
// The read is synchronous because it is already interruptible: cancelling the
// request context closes the response body, and a blocked sc.Scan returns with
// an error the moment that happens. An earlier version pumped lines through a
// buffered channel from a second goroutine to give the reader a deadline. That
// bought nothing the transport does not already provide, and cost a goroutine,
// a channel, a three-way select and a timer-drain dance per completion.
func readStream(ctx context.Context, body io.Reader, stream Stream, cancel context.CancelFunc) (*Msg, error) {
	out := &reply{
		toolArgs: map[int]*strings.Builder{},
		toolMeta: map[int]ToolCall{},
	}

	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 1<<20), 1<<20)

	// stalled distinguishes our own idle cancellation from the caller's: both
	// surface as a closed body, and only one of them is the provider's fault.
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
	// A cancelled turn can close the body cleanly enough that the scanner just
	// ends. Report the cancellation rather than a truncated reply.
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
		return &Msg{Role: "assistant", Content: finalContent, Reasoning: finalReasoning}
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
	return &Msg{Role: "assistant", Content: finalContent, Reasoning: finalReasoning, ToolCalls: calls}
}
