package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type ChatClient interface {
	Chat(ctx context.Context, messages []ChatMessage, tools []ToolDefinition, onToken func(string)) (*ChatMessage, error)
}

type ChatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type ToolCall struct {
	ID       string           `json:"id,omitempty"`
	Type     string           `json:"type,omitempty"`
	Function ToolCallFunction `json:"function"`
}

type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type OllamaClient struct{ httpClient *http.Client; baseURL, model, apiKey string }

func NewOllamaClient(baseURL, model, apiKey string) *OllamaClient {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	baseURL = strings.TrimRight(baseURL, "/")
	if !strings.HasSuffix(baseURL, "/v1") {
		baseURL += "/v1"
	}
	return &OllamaClient{&http.Client{Timeout: 10 * time.Minute}, baseURL, model, apiKey}
}

func (c *OllamaClient) Chat(ctx context.Context, messages []ChatMessage, tools []ToolDefinition, onToken func(string)) (*ChatMessage, error) {
	toolDefs := make([]map[string]any, len(tools))
	for i, t := range tools {
		toolDefs[i] = map[string]any{"type": "function", "function": map[string]any{"name": t.Name, "description": t.Description, "parameters": t.InputSchema}}
	}
	body, _ := json.Marshal(map[string]any{"model": c.model, "messages": messages, "tools": toolDefs, "stream": true})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.httpClient.Do(req)
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
	toolArgs, toolMeta := map[int]*strings.Builder{}, map[int]ToolCall{}
	var chunk struct {
		Choices []struct {
			Delta struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					Index    int    `json:"index"`
					ID, Type string
					Function struct{ Name, Arguments string } `json:"function"`
				} `json:"tool_calls"`
			} `json:"delta"`
		} `json:"choices"`
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimPrefix(scanner.Text(), "data: ")
		chunk.Choices = nil
		if line == "" || line == "[DONE]" || json.Unmarshal([]byte(line), &chunk) != nil || len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta
		if delta.Content != "" {
			content.WriteString(delta.Content)
			if onToken != nil {
				onToken(delta.Content)
			}
		}
		for _, tc := range delta.ToolCalls {
			if _, ok := toolMeta[tc.Index]; !ok {
				toolMeta[tc.Index] = ToolCall{tc.ID, tc.Type, ToolCallFunction{Name: tc.Function.Name}}
				toolArgs[tc.Index] = &strings.Builder{}
			}
			toolArgs[tc.Index].WriteString(tc.Function.Arguments)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(toolMeta) > 0 {
		calls := make([]ToolCall, 0, len(toolMeta))
		for i, tc := range toolMeta {
			tc.Function.Arguments = toolArgs[i].String()
			if tc.Function.Arguments == "" {
				tc.Function.Arguments = "{}"
			}
			calls = append(calls, tc)
		}
		return &ChatMessage{Role: "assistant", ToolCalls: calls}, nil
	}
	return &ChatMessage{Role: "assistant", Content: content.String()}, nil
}
