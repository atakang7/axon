package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// Msg is one entry in the conversation. Session.Messages is the immutable log.
// Memory state is projection metadata set by the pruner:
//
//   - Parked == true means ContextMessages emits a breadcrumb for this block,
//     not the original content. The original lives in Session.ParkedBlocks
//     under this Msg.ID.
//   - Forgotten == true means ContextMessages drops this block from the
//     model's view entirely. The original Msg stays in Session.Messages for
//     human audit only.
type Msg struct {
	Role         string     `json:"role"`
	Content      string     `json:"content,omitempty"`
	ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID   string     `json:"tool_call_id,omitempty"`
	ToolName     string     `json:"tool_name,omitempty"`
	ID           string     `json:"id,omitempty"`
	Parked       bool       `json:"parked,omitempty"`
	ParkSummary  string     `json:"park_summary,omitempty"`
	ParkReason   string     `json:"park_reason,omitempty"`
	Forgotten    bool       `json:"forgotten,omitempty"`
	ForgetReason string     `json:"forget_reason,omitempty"`
}

type ToolCall struct {
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type Provider struct {
	Name, BaseURL, Model, APIKey string
	Extra                        json.RawMessage
}

type Client struct {
	http    *http.Client
	baseURL string
	p       Provider

	// MaxTokens, when > 0, overrides the default per-request max_tokens cap.
	// Used by the pruner to keep its output bounded — a chatty cheap model can
	// otherwise emit thousands of tokens of "reasoning" before producing the
	// structured output.
	MaxTokens int
}

func NewClient(p Provider) (*Client, error) {
	url := strings.TrimRight(p.BaseURL, "/")
	if url == "" {
		return nil, fmt.Errorf("provider %q has no base_url", p.Name)
	}
	if !strings.HasSuffix(url, "/v1") {
		url += "/v1"
	}
	return &Client{http: &http.Client{Timeout: 30 * time.Minute}, baseURL: url, p: p}, nil
}

// LoadProviders reads providers.json and returns a flattened map keyed by
// "provider/model". Each provider entry may carry multiple models under a
// "models" array; the legacy single "model" field is still accepted so old
// configs keep working.
//
//	{
//	  "providers": [
//	    {
//	      "name": "openrouter",
//	      "base_url": "https://openrouter.ai/api",
//	      "api_key": "sk-...",
//	      "provider": "",
//	      "models": [{"model": "anthropic/claude-sonnet-4-6"}]
//	    }
//	  ]
//	}
func LoadProviders() (map[string]Provider, error) {
	out := map[string]Provider{}
	data, err := os.ReadFile(providersPath())
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	var cfg struct {
		Providers []struct {
			Name     string          `json:"name"`
			BaseURL  string          `json:"base_url"`
			Model    string          `json:"model"`
			APIKey   string          `json:"api_key"`
			Provider json.RawMessage `json:"provider"`
			Models   []struct {
				Model    string          `json:"model"`
				Alias    string          `json:"alias,omitempty"`
				Provider json.RawMessage `json:"provider,omitempty"`
			} `json:"models"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	// providerExtra normalises the `provider` field. A bare string (e.g.
	// "anthropic") is the OpenRouter routing slug shorthand and gets expanded
	// to the full {order, allow_fallbacks} object. Anything else passes through.
	providerExtra := func(raw json.RawMessage) json.RawMessage {
		if len(raw) == 0 {
			return nil
		}
		t := strings.TrimSpace(string(raw))
		if strings.HasPrefix(t, "\"") {
			var slug string
			if err := json.Unmarshal(raw, &slug); err == nil {
				if slug == "" {
					return nil
				}
				out, _ := json.Marshal(map[string]any{"order": []string{slug}, "allow_fallbacks": true})
				return out
			}
		}
		return raw
	}
	for _, p := range cfg.Providers {
		if p.Name == "" {
			return nil, fmt.Errorf("provider name required")
		}
		name := strings.ToLower(p.Name)
		defaultExtra := providerExtra(p.Provider)
		models := p.Models
		if len(models) == 0 && p.Model != "" {
			models = []struct {
				Model    string          `json:"model"`
				Alias    string          `json:"alias,omitempty"`
				Provider json.RawMessage `json:"provider,omitempty"`
			}{{Model: p.Model}}
		}
		if len(models) == 0 {
			return nil, fmt.Errorf("provider %q has no models", name)
		}
		for _, m := range models {
			if m.Model == "" {
				return nil, fmt.Errorf("provider %q has a model entry with no model field", name)
			}
			extra := providerExtra(m.Provider)
			if extra == nil {
				extra = defaultExtra
			}
			key := name + "/" + m.Model
			out[key] = Provider{Name: name, BaseURL: p.BaseURL, Model: m.Model, APIKey: p.APIKey, Extra: extra}
		}
	}
	return out, nil
}

func (c *Client) Chat(ctx context.Context, msgs []Msg, tools []Tool, onToken func(string), onPhase func(string, time.Duration)) (*Msg, error) {
	return c.ChatStream(ctx, msgs, tools, onToken, nil, nil, nil, onPhase)
}

// ChatStream is Chat with separate callbacks for reasoning tokens, tool-call
// argument deltas, and a per-second heartbeat. DeepSeek and other reasoning
// models stream a long reasoning block before any content arrives, and some
// providers buffer tool_calls.function.arguments to end-of-message instead of
// streaming them — meaning a "frozen" screen is sometimes a healthy stream
// with no deltas yet. The heartbeat fires every second after first-byte so
// the UI can show "alive, waiting Ns" instead of looking dead.
func (c *Client) ChatStream(ctx context.Context, msgs []Msg, tools []Tool, onToken, onReasoning func(string), onToolArg func(name, delta string), onHeartbeat func(elapsed time.Duration), onPhase func(string, time.Duration)) (*Msg, error) {
	t0 := time.Now()
	emit := func(name string, since time.Time) {
		if onPhase != nil {
			onPhase(name, time.Since(since))
		}
	}
	td := make([]map[string]any, len(tools))
	for i, t := range tools {
		td[i] = map[string]any{"type": "function", "function": map[string]any{"name": t.Name, "description": t.Description, "parameters": t.Schema}}
	}
	maxTokens := 20000
	if c.MaxTokens > 0 {
		maxTokens = c.MaxTokens
	}
	body := map[string]any{
		"model": c.p.Model, "messages": msgs, "tools": td,
		"stream": true, "parallel_tool_calls": true, "max_tokens": maxTokens,
	}
	if len(c.p.Extra) > 0 {
		body["provider"] = c.p.Extra
	}
	raw, _ := json.Marshal(body)
	emit("body-marshal", t0)
	tReq := time.Now()
	reqCtx, cancelReq := context.WithCancel(ctx)
	defer cancelReq()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.p.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.p.APIKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	emit("http-headers", tReq)
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		s := bufio.NewScanner(resp.Body)
		s.Scan()
		return nil, fmt.Errorf("API error %s: %s", resp.Status, s.Text())
	}
	var content strings.Builder
	toolArgs := map[int]*strings.Builder{}
	toolMeta := map[int]ToolCall{}
	var chunk struct {
		Choices []struct {
			Delta struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"` // DeepSeek-style thinking tokens — ignored, not content
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
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	tStream := time.Now()
	var (
		gotFirstByte, gotFirstReasoning, gotFirstContent, gotFirstTool bool
		reasoningBytes, contentBytes, toolArgBytes                     int
	)

	// Pump SSE lines into a channel so the consumer can apply an idle-read
	// timeout. Without this, a silent upstream (no [DONE], no close) would
	// block sc.Scan() until http.Client.Timeout (30 minutes).
	type lineMsg struct {
		text string
		err  error
	}
	lines := make(chan lineMsg, 32)
	go func() {
		defer close(lines)
		for sc.Scan() {
			lines <- lineMsg{text: sc.Text()}
		}
		if err := sc.Err(); err != nil {
			lines <- lineMsg{err: err}
		}
	}()

	const idleTimeout = 20 * time.Second
	idle := time.NewTimer(idleTimeout)
	defer idle.Stop()

	// Heartbeat: while the stream is open but bytes are sparse, tick every
	// second so the UI can show "stream alive, waiting…" instead of looking
	// frozen. Stops once anything starts arriving fast enough on its own.
	heartbeat := time.NewTicker(1 * time.Second)
	defer heartbeat.Stop()

streamLoop:
	for {
		var lm lineMsg
		var ok bool
		select {
		case <-heartbeat.C:
			if onHeartbeat != nil && gotFirstByte {
				onHeartbeat(time.Since(tStream))
			}
			continue
		case lm, ok = <-lines:
			if !ok {
				break streamLoop
			}
			if lm.err != nil {
				return nil, lm.err
			}
			if !idle.Stop() {
				select {
				case <-idle.C:
				default:
				}
			}
			idle.Reset(idleTimeout)
		case <-idle.C:
			cancelReq()
			return nil, fmt.Errorf("stream stalled: no data for %s (provider went silent mid-response)", idleTimeout)
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		if !gotFirstByte {
			gotFirstByte = true
			emit("first-byte", tStream)
		}
		line := strings.TrimPrefix(lm.text, "data: ")
		chunk.Choices = nil
		if line == "" || line == "[DONE]" || json.Unmarshal([]byte(line), &chunk) != nil || len(chunk.Choices) == 0 {
			continue
		}
		d := chunk.Choices[0].Delta
		if d.ReasoningContent != "" {
			if !gotFirstReasoning {
				gotFirstReasoning = true
				emit("first-reasoning", tStream)
			}
			reasoningBytes += len(d.ReasoningContent)
			if onReasoning != nil {
				onReasoning(d.ReasoningContent)
			}
		}
		if d.Content != "" {
			if !gotFirstContent {
				gotFirstContent = true
				emit("first-content", tStream)
			}
			contentBytes += len(d.Content)
			content.WriteString(d.Content)
			if onToken != nil {
				onToken(d.Content)
			}
		}
		for _, tc := range d.ToolCalls {
			if !gotFirstTool {
				gotFirstTool = true
				emit("first-toolcall", tStream)
			}
			toolArgBytes += len(tc.Function.Arguments)
			if _, ok := toolMeta[tc.Index]; !ok {
				m := ToolCall{ID: tc.ID, Type: tc.Type}
				m.Function.Name = tc.Function.Name
				toolMeta[tc.Index] = m
				toolArgs[tc.Index] = &strings.Builder{}
			}
			toolArgs[tc.Index].WriteString(tc.Function.Arguments)
			if onToolArg != nil && tc.Function.Arguments != "" {
				onToolArg(toolMeta[tc.Index].Function.Name, tc.Function.Arguments)
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	emit("stream-end", tStream)
	emit("total", t0)
	if onPhase != nil {
		onPhase(fmt.Sprintf("bytes reasoning=%d content=%d toolargs=%d", reasoningBytes, contentBytes, toolArgBytes), 0)
	}
	if len(toolMeta) > 0 {
		indices := make([]int, 0, len(toolMeta))
		for i := range toolMeta {
			indices = append(indices, i)
		}
		sort.Ints(indices)
		calls := make([]ToolCall, 0, len(toolMeta))
		for _, i := range indices {
			tc := toolMeta[i]
			tc.Function.Arguments = toolArgs[i].String()
			if tc.Function.Arguments == "" {
				tc.Function.Arguments = "{}"
			}
			calls = append(calls, tc)
		}
		return &Msg{Role: "assistant", Content: content.String(), ToolCalls: calls}, nil
	}
	return &Msg{Role: "assistant", Content: content.String()}, nil
}
