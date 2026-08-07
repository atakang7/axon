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
	"time"
)

// Model is an LLM the runtime can talk to.
//
// This interface is the whole contract. Implement it to reach a provider that
// is not OpenAI-compatible, to route through your own gateway, or to supply a
// deterministic fake so the turn loop can be tested without a network. Client,
// below, is simply the implementation that ships.
type Model interface {
	Complete(ctx context.Context, req Request) (*Msg, error)
}

// Request is one completion.
type Request struct {
	// Messages is the conversation as the model should see it.
	Messages []Msg
	// Tools the model may call. Empty means none.
	Tools []ToolSpec
	// MaxTokens caps this one reply. Zero means the model's own default.
	// It is per-request because callers want different budgets: an agent turn
	// may need thousands of tokens, while the pruner needs one line of JSON.
	MaxTokens int
	// Stream receives output as it arrives. The zero value discards it.
	Stream Stream
}

// Stream receives incremental output during a completion. Every field is
// optional; a nil func is simply not called.
//
// Callbacks run synchronously on the goroutine reading the response, in
// arrival order, and must not block. Anything slow — a network write to a
// browser, a disk flush — has to be handed to a buffered channel and done
// elsewhere: blocking here stops the read, and a stall long enough to trip the
// idle timeout fails the whole completion.
//
// Reasoning is separate from Token because reasoning models emit a long
// thinking block before any content, and a caller usually wants to render the
// two differently. ToolArgs exists because some providers buffer tool-call
// arguments to end-of-message rather than streaming them, so a UI that only
// watches Token can look frozen during a perfectly healthy stream.
type Stream struct {
	Token     func(text string)
	Reasoning func(text string)
	ToolArgs  func(name, delta string)
}

// Provider is one endpoint: base URL, model name, credentials, and any
// provider-specific routing options forwarded verbatim as the request's
// "provider" field.
//
// How an embedder decides which provider to use — a config file, flags, an
// environment cascade — is the embedder's business. This package only needs
// the resolved answer.
type Provider struct {
	Name, BaseURL, Model, APIKey string
	Extra                        json.RawMessage
}

// ToolSpec is a tool as the model sees it: a name, a description, and a JSON
// schema. It deliberately has no implementation field.
//
// This type is the contract that keeps the model layer independent of the
// execution layer. The agent package holds the richer Tool (schema plus the Go
// function that runs it) and projects it down to ToolSpec at the call
// boundary, so nothing here can reach a tool's behaviour — only its shape.
type ToolSpec struct {
	Name        string
	Description string
	Schema      map[string]any
}

// Msg is one entry in the conversation. Session.Messages is the immutable log.
//
// Parked == true means ContextMessages emits a one-line breadcrumb for this
// block instead of its content. The content itself is never modified.
type Msg struct {
	Role        string     `json:"role"`
	Content     string     `json:"content,omitempty"`
	ToolCalls   []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID  string     `json:"tool_call_id,omitempty"`
	ToolName    string     `json:"tool_name,omitempty"`
	ID          string     `json:"id,omitempty"`
	Parked      bool       `json:"parked,omitempty"`
	ParkSummary string     `json:"park_summary,omitempty"`
	ParkReason  string     `json:"park_reason,omitempty"`
}

type ToolCall struct {
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

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
		// Include the first line of the body: a bare status tells you nothing
		// about which of a dozen things the provider objected to.
		s := bufio.NewScanner(resp.Body)
		s.Scan()
		return nil, fmt.Errorf("API error %s: %s", resp.Status, s.Text())
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
		"model":               c.cfg.Provider.Model,
		"messages":            req.Messages,
		"tools":               tools,
		"stream":              true,
		"parallel_tool_calls": true,
		"max_tokens":          maxTokens,
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
	content  strings.Builder
	toolArgs map[int]*strings.Builder
	toolMeta map[int]ToolCall
}

// readStream consumes the SSE body until the server closes it, applying an
// idle timeout so a silent provider fails fast instead of hanging the turn.
// cancel aborts the in-flight request when that happens.
func readStream(ctx context.Context, body io.Reader, stream Stream, cancel context.CancelFunc) (*Msg, error) {
	out := &reply{
		toolArgs: map[int]*strings.Builder{},
		toolMeta: map[int]ToolCall{},
	}

	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 1<<20), 1<<20)

	// Pump lines through a channel so the reader can be given a deadline;
	// sc.Scan alone would block uninterruptibly on a silent connection.
	type line struct {
		text string
		err  error
	}
	// Every send is guarded by ctx. When this function returns early — an idle
	// timeout, or the user interrupting the turn — nothing drains the channel
	// again, so an unguarded send would block this goroutine forever once the
	// buffer filled, leaking a goroutine and its connection per cancelled
	// stream. Interrupting mid-stream is routine in an agent, not an edge case.
	lines := make(chan line, 32)
	go func() {
		defer close(lines)
		send := func(l line) bool {
			select {
			case lines <- l:
				return true
			case <-ctx.Done():
				return false
			}
		}
		for sc.Scan() {
			if !send(line{text: sc.Text()}) {
				return
			}
		}
		if err := sc.Err(); err != nil {
			send(line{err: err})
		}
	}()

	idle := time.NewTimer(idleTimeout)
	defer idle.Stop()

	for {
		select {
		case l, ok := <-lines:
			if !ok {
				return out.message(), nil
			}
			if l.err != nil {
				return nil, l.err
			}
			if !idle.Stop() {
				select {
				case <-idle.C:
				default:
				}
			}
			idle.Reset(idleTimeout)
			out.consume(l.text, stream)

		case <-idle.C:
			cancel()
			return nil, fmt.Errorf("stream stalled: no data for %s (provider went silent mid-response)", idleTimeout)

		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
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

	if delta.ReasoningContent != "" && stream.Reasoning != nil {
		stream.Reasoning(delta.ReasoningContent)
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
	if len(r.toolMeta) == 0 {
		return &Msg{Role: "assistant", Content: r.content.String()}
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
	return &Msg{Role: "assistant", Content: r.content.String(), ToolCalls: calls}
}
