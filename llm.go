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
// Memory state is projection metadata carried inline:
//
//   - TTL ticks every turn for active blocks. When it hits zero the block is
//     auto-parked.
//   - Parked == true means ContextMessages emits a breadcrumb for this block,
//     not the original content. The original lives in Session.ParkedBlocks
//     under this Msg.ID and can be recalled.
//   - Forgotten == true means ContextMessages emits a tombstone for this block.
//     The original content stays in Session.Messages for traceability, but is
//     no longer reachable by the agent.
type Msg struct {
	Role         string     `json:"role"`
	Content      string     `json:"content,omitempty"`
	ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID   string     `json:"tool_call_id,omitempty"`
	ToolName     string     `json:"tool_name,omitempty"`
	ID           string     `json:"id,omitempty"`
	TTL          int        `json:"ttl,omitempty"`
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
		} `json:"providers"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	for _, p := range cfg.Providers {
		if p.Model == "" || p.Name == "" {
			return nil, fmt.Errorf("provider name and model required")
		}
		extra := p.Provider
		if t := strings.TrimSpace(string(extra)); strings.HasPrefix(t, "\"") {
			var slug string
			if err := json.Unmarshal(extra, &slug); err == nil {
				extra, _ = json.Marshal(map[string]any{"order": []string{slug}, "allow_fallbacks": true})
			}
		}
		name := strings.ToLower(p.Name)
		out[name] = Provider{Name: name, BaseURL: p.BaseURL, Model: p.Model, APIKey: p.APIKey, Extra: extra}
	}
	return out, nil
}

func (c *Client) Chat(ctx context.Context, msgs []Msg, tools []Tool, onToken func(string)) (*Msg, error) {
	td := make([]map[string]any, len(tools))
	for i, t := range tools {
		td[i] = map[string]any{"type": "function", "function": map[string]any{"name": t.Name, "description": t.Description, "parameters": t.Schema}}
	}
	body := map[string]any{
		"model": c.p.Model, "messages": msgs, "tools": td,
		"stream": true, "parallel_tool_calls": true, "max_tokens": 20000,
	}
	if len(c.p.Extra) > 0 {
		body["provider"] = c.p.Extra
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(raw))
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
	for sc.Scan() {
		line := strings.TrimPrefix(sc.Text(), "data: ")
		chunk.Choices = nil
		if line == "" || line == "[DONE]" || json.Unmarshal([]byte(line), &chunk) != nil || len(chunk.Choices) == 0 {
			continue
		}
		d := chunk.Choices[0].Delta
		if d.Content != "" {
			content.WriteString(d.Content)
			if onToken != nil {
				onToken(d.Content)
			}
		}
		for _, tc := range d.ToolCalls {
			if _, ok := toolMeta[tc.Index]; !ok {
				m := ToolCall{ID: tc.ID, Type: tc.Type}
				m.Function.Name = tc.Function.Name
				toolMeta[tc.Index] = m
				toolArgs[tc.Index] = &strings.Builder{}
			}
			toolArgs[tc.Index].WriteString(tc.Function.Arguments)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
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
