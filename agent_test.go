package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// --- test client ---

type scriptedClient struct {
	responses     []*ChatMessage
	calls         int
	capturedTurns [][]ChatMessage
	capturedTools [][]ToolDefinition
}

func (s *scriptedClient) Chat(_ context.Context, messages []ChatMessage, tools []ToolDefinition, _ func(string)) (*ChatMessage, error) {
	s.capturedTurns = append(s.capturedTurns, append([]ChatMessage(nil), messages...))
	s.capturedTools = append(s.capturedTools, append([]ToolDefinition(nil), tools...))
	if s.calls >= len(s.responses) {
		return &ChatMessage{Role: "assistant"}, nil
	}
	r := s.responses[s.calls]
	s.calls++
	return r, nil
}

// --- agent ---

func TestNewAgent_SetsFields(t *testing.T) {
	agent := NewAgent(NewOllamaClient("", "m", ""), func() (string, bool) { return "hi", true }, []ToolDefinition{ReadFileDefinition}, NewSession())
	if agent.client == nil || agent.getUserMessage == nil || len(agent.tools) != 1 {
		t.Fatal("unexpected agent state")
	}
}

func TestExecuteTool_NotFound(t *testing.T) {
	agent := NewAgent(NewOllamaClient("", "m", ""), func() (string, bool) { return "", false }, nil, NewSession())
	result := agent.executeTool("id", "missing", json.RawMessage(`{}`))
	if result.Content != "tool not found" {
		t.Fatalf("got %q", result.Content)
	}
}

func TestExecuteTool_Success(t *testing.T) {
	tool := ToolDefinition{Name: "t", Function: func(json.RawMessage) (string, error) { return "ok", nil }}
	agent := NewAgent(NewOllamaClient("", "m", ""), func() (string, bool) { return "", false }, []ToolDefinition{tool}, NewSession())
	if r := agent.executeTool("id", "t", json.RawMessage(`{}`)); r.Content != "ok" {
		t.Fatalf("got %q", r.Content)
	}
}

func TestRun_SystemMessageFirst(t *testing.T) {
	c := &scriptedClient{responses: []*ChatMessage{{Role: "assistant", Content: "hi"}}}
	n := 0
	agent := NewAgent(c, func() (string, bool) { n++; if n == 1 { return "hey", true }; return "", false }, nil, NewSession())
	agent.Run(context.Background())
	if c.capturedTurns[0][0].Role != "system" || c.capturedTurns[0][1].Content != "hey" {
		t.Fatal("unexpected first turn")
	}
}

func TestRun_ToolCallNoExtraUserRead(t *testing.T) {
	c := &scriptedClient{responses: []*ChatMessage{
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "c1", Type: "function", Function: ToolCallFunction{Name: "t", Arguments: `{}`}}}},
		{Role: "assistant", Content: "done"},
	}}
	n := 0
	tool := ToolDefinition{Name: "t", Function: func(json.RawMessage) (string, error) { return "ok", nil }}
	agent := NewAgent(c, func() (string, bool) { n++; if n == 1 { return "go", true }; return "", false }, []ToolDefinition{tool}, NewSession())
	agent.Run(context.Background())
	last := c.capturedTurns[1][len(c.capturedTurns[1])-1]
	if last.Role != "tool" || last.Content != "ok" {
		t.Fatalf("unexpected last message: %+v", last)
	}
}

// --- tools ---

func TestReadFile(t *testing.T) {
	f := filepath.Join(t.TempDir(), "f.txt")
	os.WriteFile(f, []byte("hello"), 0644)
	input, _ := json.Marshal(ReadFileInput{Path: f})
	got, err := ReadFile(input)
	if err != nil || got != "1\thello" {
		t.Fatalf("err=%v got=%q", err, got)
	}
}

func TestReadFile_Missing(t *testing.T) {
	input, _ := json.Marshal(ReadFileInput{Path: "/no/such/file"})
	if _, err := ReadFile(input); err == nil {
		t.Fatal("expected error")
	}
}

func TestEditFile_Replace(t *testing.T) {
	f := filepath.Join(t.TempDir(), "f.txt")
	os.WriteFile(f, []byte("hello world"), 0644)
	input, _ := json.Marshal(EditFileInput{Path: f, OldStr: "world", NewStr: "Go"})
	if _, err := editFile(NewSession())(input); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(f)
	if string(b) != "hello Go" {
		t.Fatalf("got %q", b)
	}
}

func TestEditFile_Create(t *testing.T) {
	f := filepath.Join(t.TempDir(), "new.txt")
	input, _ := json.Marshal(EditFileInput{Path: f, OldStr: "", NewStr: "content"})
	if _, err := editFile(NewSession())(input); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(f)
	if string(b) != "content" {
		t.Fatalf("got %q", b)
	}
}

func TestEditFile_OldStrNotFound(t *testing.T) {
	f := filepath.Join(t.TempDir(), "f.txt")
	os.WriteFile(f, []byte("hello"), 0644)
	input, _ := json.Marshal(EditFileInput{Path: f, OldStr: "nope", NewStr: "x"})
	if _, err := editFile(NewSession())(input); err == nil {
		t.Fatal("expected error")
	}
}

func TestListFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0644)
	input, _ := json.Marshal(ListFilesInput{Path: dir})
	result, err := ListFiles(input)
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	json.Unmarshal([]byte(result), &files)
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %v", files)
	}
}

// --- providers ---

func TestProviderRegistry_RegisterGet(t *testing.T) {
	r := NewProviderRegistry()
	r.Register("local", "http://localhost:8080", "llama3", "")
	p, ok := r.Get("LOCAL")
	if !ok || p.Model != "llama3" {
		t.Fatalf("unexpected provider: %+v", p)
	}
}

func TestProviderRegistry_MissingModel(t *testing.T) {
	if err := NewProviderRegistry().Register("x", "", "", ""); err == nil {
		t.Fatal("expected error")
	}
}


func TestSelectedProviderName(t *testing.T) {
	if SelectedProviderName(func(string) string { return "" }) != "ollama" {
		t.Fatal("expected default ollama")
	}
	if SelectedProviderName(func(k string) string { if k == "LLM_PROVIDER" { return " Proxy " }; return "" }) != "proxy" {
		t.Fatal("expected proxy")
	}
}

func TestLoadConfigFile(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "providers.json")
	os.WriteFile(cfgFile, []byte(`{"providers":[{"name":"local","base_url":"http://localhost:8080","model":"llama3"}]}`), 0644)

	r := NewProviderRegistry()
	// patch by reading directly since LoadConfigFile uses $HOME
	data, _ := os.ReadFile(cfgFile)
	var cfg struct {
		Providers []struct {
			Name    string `json:"name"`
			BaseURL string `json:"base_url"`
			Model   string `json:"model"`
			APIKey  string `json:"api_key"`
		} `json:"providers"`
	}
	json.Unmarshal(data, &cfg)
	for _, p := range cfg.Providers {
		r.Register(p.Name, p.BaseURL, p.Model, p.APIKey)
	}

	p, ok := r.Get("local")
	if !ok || p.Model != "llama3" || p.BaseURL != "http://localhost:8080" {
		t.Fatalf("unexpected provider: %+v", p)
	}
}
