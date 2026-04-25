package main

// agent_regression_test.go keeps the original compact regression tests together.
// These are the fast, focused sanity checks for core behavior that we want to
// preserve even as the broader unit and integration suites grow.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteCreateAndReplaceString(t *testing.T) {
	s := tmpSession(t)
	tool := WriteTool(s)

	_, err := tool.Fn(json.RawMessage(`{"path":"a.txt","mode":"create","content":"hello","reason":"new file"}`))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	b, _ := os.ReadFile(filepath.Join(s.Cwd, "a.txt"))
	if string(b) != "hello" {
		t.Fatalf("got %q", b)
	}

	_, err = tool.Fn(json.RawMessage(`{"path":"a.txt","mode":"replace_string","old_str":"hello","content":"world","reason":"swap word"}`))
	if err != nil {
		t.Fatalf("replace: %v", err)
	}
	b, _ = os.ReadFile(filepath.Join(s.Cwd, "a.txt"))
	if string(b) != "world" {
		t.Fatalf("got %q", b)
	}

	if _, err := tool.Fn(json.RawMessage(`{"path":"a.txt","mode":"replace_string","old_str":"nope","content":"x","reason":"miss"}`)); err == nil {
		t.Fatal("expected not-found error")
	}
}

func TestWriteAmbiguousMatch(t *testing.T) {
	s := tmpSession(t)
	os.WriteFile(filepath.Join(s.Cwd, "f.txt"), []byte("x\nx\n"), 0644)
	_, err := WriteTool(s).Fn(json.RawMessage(`{"path":"f.txt","mode":"replace_string","old_str":"x","content":"y","reason":"ambig"}`))
	if err == nil || !strings.Contains(err.Error(), "matches 2") {
		t.Fatalf("expected ambiguous, got %v", err)
	}
}

func TestWriteOverwrite(t *testing.T) {
	s := tmpSession(t)
	path := filepath.Join(s.Cwd, "full.txt")
	os.WriteFile(path, []byte("before"), 0644)
	_, err := WriteTool(s).Fn(json.RawMessage(`{"path":"full.txt","mode":"overwrite","content":"after","reason":"reset"}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if string(b) != "after" {
		t.Fatalf("got %q", b)
	}
}

func TestWriteReplaceLines(t *testing.T) {
	s := tmpSession(t)
	path := filepath.Join(s.Cwd, "lines.txt")
	os.WriteFile(path, []byte("a\nb\nc\nd\n"), 0644)
	_, err := WriteTool(s).Fn(json.RawMessage(`{"path":"lines.txt","mode":"replace_lines","start_line":2,"end_line":3,"content":"B\nC","reason":"line edit"}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if string(b) != "a\nB\nC\nd\n" {
		t.Fatalf("got %q", b)
	}
}

func TestUndo(t *testing.T) {
	s := tmpSession(t)
	path := filepath.Join(s.Cwd, "u.txt")
	os.WriteFile(path, []byte("before"), 0644)
	WriteTool(s).Fn(json.RawMessage(`{"path":"u.txt","mode":"replace_string","old_str":"before","content":"after","reason":"undo test"}`))
	e, ok := s.Undo()
	if !ok || e.Before != "before" {
		t.Fatalf("bad undo: %+v", e)
	}
}

func TestReadSlice(t *testing.T) {
	s := tmpSession(t)
	os.WriteFile(filepath.Join(s.Cwd, "r.txt"), []byte("a\nb\nc"), 0644)
	out, err := ReadTool(s).Fn(json.RawMessage(`{"path":"r.txt","mode":"slice","offset":1,"limit":10,"reason":"read all"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "1\ta") || !strings.Contains(out, "3\tc") {
		t.Fatalf("bad output: %q", out)
	}
}

func TestReadSkeleton(t *testing.T) {
	s := tmpSession(t)
	os.WriteFile(filepath.Join(s.Cwd, "x.go"), []byte("package main\n\nfunc Hello() {}\n\nfunc World() {}\n"), 0644)
	out, err := ReadTool(s).Fn(json.RawMessage(`{"path":"x.go","mode":"skeleton","reason":"shape"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Hello") || !strings.Contains(out, "World") {
		t.Fatalf("bad skeleton: %q", out)
	}
}

func TestReadRequiresReason(t *testing.T) {
	s := tmpSession(t)
	os.WriteFile(filepath.Join(s.Cwd, "r.txt"), []byte("a"), 0644)
	_, err := ReadTool(s).Fn(json.RawMessage(`{"path":"r.txt","mode":"full"}`))
	if err == nil || !strings.Contains(err.Error(), "reason is required") {
		t.Fatalf("expected reason required, got %v", err)
	}
}

func TestSearchLiteral(t *testing.T) {
	s := tmpSession(t)
	os.WriteFile(filepath.Join(s.Cwd, "r.txt"), []byte("alpha\nbeta\nGamma\n"), 0644)
	out, err := SearchTool(s).Fn(json.RawMessage(`{"query":"gamma","mode":"literal","path":".","reason":"find gamma"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Gamma") {
		t.Fatalf("bad output: %q", out)
	}
}

func TestBuildToolsOnlyMinimalSet(t *testing.T) {
	s := tmpSession(t)
	got := BuildTools(s)
	want := []string{toolRead, toolWrite, toolExec, toolSearch, toolTask, toolPark, toolRecall, toolForget, toolRefresh}
	if len(got) != len(want) {
		t.Fatalf("got %d tools want %d", len(got), len(want))
	}
	for i, name := range want {
		if got[i].Name != name {
			t.Fatalf("tool %d = %q want %q", i, got[i].Name, name)
		}
	}
}

func TestExecBasic(t *testing.T) {
	s := tmpSession(t)
	out, err := ExecTool(s).Fn(json.RawMessage(`{"mode":"run","command":"echo hi","tail_lines":10,"reason":"test"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "hi") || !strings.Contains(out, "exit_code: 0") {
		t.Fatalf("bad: %q", out)
	}
}

func TestExecRequiresTailLines(t *testing.T) {
	s := tmpSession(t)
	_, err := ExecTool(s).Fn(json.RawMessage(`{"mode":"run","command":"echo hi","reason":"no tail"}`))
	if err == nil || !strings.Contains(err.Error(), "tail_lines") {
		t.Fatalf("expected tail_lines required, got %v", err)
	}
}

func TestExecVerify(t *testing.T) {
	s := tmpSession(t)
	// tmpSession cwd is a temp dir with no project markers — expect a clear error
	_, err := ExecTool(s).Fn(json.RawMessage(`{"mode":"verify","reason":"check build"}`))
	if err == nil || !strings.Contains(err.Error(), "could not detect") {
		t.Fatalf("expected detection failure, got %v", err)
	}
}

func TestSessionPersist(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "s.json")
	s := &Session{ID: "x", path: p, Cwd: dir}
	s.ensure()
	s.Append(Msg{Role: "user", Content: "hello"})
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(p)
	if !strings.Contains(string(data), "hello") {
		t.Fatalf("not saved: %s", data)
	}
}

func TestSessionPathEnvOverride(t *testing.T) {
	t.Setenv("AXON_SESSION_PATH", "/tmp/axon-session-test.json")
	if got := sessionPath(); got != "/tmp/axon-session-test.json" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveProviderFromEnv(t *testing.T) {
	t.Setenv("LLM_MODEL", "test-model")
	t.Setenv("LLM_BASE_URL", "http://example.test")
	t.Setenv("LLM_API_KEY", "secret")
	p, err := ResolveProvider(nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "env" || p.Model != "test-model" || p.BaseURL != "http://example.test" || p.APIKey != "secret" {
		t.Fatalf("bad provider: %+v", p)
	}
}

func TestResolveProviderFromEnvWithExplicitName(t *testing.T) {
	t.Setenv("LLM_PROVIDER", "openai")
	t.Setenv("LLM_MODEL", "test-model")
	t.Setenv("LLM_BASE_URL", "http://example.test")
	p, err := ResolveProvider(nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "openai" {
		t.Fatalf("expected named env provider, got %+v", p)
	}
}

func TestResolveProviderRequiresExplicitChoice(t *testing.T) {
	providers := map[string]Provider{
		"one": {Name: "one", Model: "m1", BaseURL: "http://one.test"},
		"two": {Name: "two", Model: "m2", BaseURL: "http://two.test"},
	}
	_, err := ResolveProvider(providers)
	if err == nil || !strings.Contains(err.Error(), "set LLM_PROVIDER") {
		t.Fatalf("expected explicit provider error, got %v", err)
	}
}

func TestParkAndRecall(t *testing.T) {
	s := tmpSession(t)
	s.Append(Msg{Role: "user", Content: "important context"})
	id := s.Messages[len(s.Messages)-1].ID
	if err := s.Park(id, "gist", "no longer needed"); err != nil {
		t.Fatal(err)
	}
	recs := s.Recall([]string{id}, "")
	if len(recs) != 1 || recs[0].OriginalContent != "important context" {
		t.Fatalf("bad recall: %+v", recs)
	}
	ctx := s.ContextMessages()
	// ctx[0] is the parked block, ctx[1] is the transient decay-status tail.
	if !strings.Contains(ctx[0].Content, "parked") || !strings.Contains(ctx[0].Content, "gist") {
		t.Fatalf("parked msg should show breadcrumb, got %q", ctx[0].Content)
	}
}

func TestContextMessagesStripsInternalFields(t *testing.T) {
	s := tmpSession(t)
	s.ParkedBlocks["m99"] = ParkedBlock{
		ID:         "m99",
		Summary:    "summary",
		Reason:     "done with it",
		Breadcrumb: "[#m99 parked | reason: done with it | gist: summary]",
	}
	s.Append(Msg{
		Role:        "tool",
		ToolName:    toolRead,
		ToolCallID:  "call_1",
		Content:     "file contents line 1\nline 2",
		ID:          "m99",
		Parked:      true,
		TTL:         2,
		ParkSummary: "summary",
		ParkReason:  "done with it",
	})
	ctx := s.ContextMessages()
	// 1 stored message + 1 transient decay-status block at the tail.
	if len(ctx) != 2 {
		t.Fatalf("expected 2 messages (1 stored + decay status), got %d", len(ctx))
	}
	if ctx[0].Content != "[#m99 parked | reason: done with it | gist: summary]" {
		t.Fatalf("parked block should emit breadcrumb, got %q", ctx[0].Content)
	}
	if ctx[0].ID != "" || ctx[0].Parked || ctx[0].TTL != 0 || ctx[0].ParkSummary != "" || ctx[0].ParkReason != "" {
		t.Fatalf("internal bookkeeping leaked into model context: %+v", ctx[0])
	}
	if ctx[1].Role != "system" || !strings.Contains(ctx[1].Content, "memory —") {
		t.Fatalf("expected trailing decay-status system block, got %+v", ctx[1])
	}
}

func TestDecayBlocksAutoParksOnExpiry(t *testing.T) {
	s := tmpSession(t)
	s.Append(Msg{Role: "user", Content: "x"})
	id := s.Messages[len(s.Messages)-1].ID
	// Force TTL=1 so the next decay tick auto-parks it.
	s.Messages[len(s.Messages)-1].TTL = 1
	s.DecayBlocks()
	m := s.Messages[len(s.Messages)-1]
	if !m.Parked {
		t.Fatalf("expected auto-park after TTL=1 decay, got %+v", m)
	}
	if _, ok := s.ParkedBlocks[id]; !ok {
		t.Fatalf("auto-parked block should be in ParkedBlocks: %+v", s.ParkedBlocks)
	}
}

func TestForgetTombstonesAndDropsRecallability(t *testing.T) {
	s := tmpSession(t)
	s.Append(Msg{Role: "user", Content: "noise"})
	id := s.Messages[len(s.Messages)-1].ID
	if err := s.Forget(id, "irrelevant"); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range s.Messages {
		if m.ID != id {
			continue
		}
		found = true
		if !m.Forgotten {
			t.Fatalf("forgotten block should remain in session log as tombstoned metadata")
		}
		if m.ForgetReason != "irrelevant" {
			t.Fatalf("expected forget reason to be recorded, got %q", m.ForgetReason)
		}
	}
	if !found {
		t.Fatalf("forgotten block should remain in session log")
	}
	if _, ok := s.ParkedBlocks[id]; ok {
		t.Fatalf("forgotten block still in ParkedBlocks")
	}
	ctx := s.ContextMessages()
	// 1 stored message + 1 transient decay-status block at the tail.
	if len(ctx) != 2 || ctx[0].Content != "[#"+id+" forgotten | reason: irrelevant]" {
		t.Fatalf("forgotten block should emit tombstone, got %+v", ctx)
	}
	if ctx[1].Role != "system" || !strings.Contains(ctx[1].Content, "memory —") {
		t.Fatalf("expected trailing decay-status system block, got %+v", ctx[1])
	}
}

func TestRefreshExtendsTTL(t *testing.T) {
	s := tmpSession(t)
	s.Append(Msg{Role: "user", Content: "x"})
	id := s.Messages[len(s.Messages)-1].ID
	s.Messages[len(s.Messages)-1].TTL = 1
	if err := s.Refresh(id, 7); err != nil {
		t.Fatal(err)
	}
	if got := s.Messages[len(s.Messages)-1].TTL; got != 7 {
		t.Fatalf("expected ttl=7, got %d", got)
	}
}

func TestShouldEndTurnAfterTool(t *testing.T) {
	if !shouldEndTurnAfterTool(toolPark, json.RawMessage(`{"next_step":"end"}`)) {
		t.Fatal("park with next_step=end should stop the turn")
	}
	if shouldEndTurnAfterTool(toolPark, json.RawMessage(`{"next_step":"continue"}`)) {
		t.Fatal("park with next_step=continue should keep the turn going")
	}
	if shouldEndTurnAfterTool(toolRead, json.RawMessage(`{"next_step":"end"}`)) {
		t.Fatal("non-memory tools should not stop the turn")
	}
}

func TestRetryableError(t *testing.T) {
	cases := map[string]bool{
		"API error 429 Too Many Requests": true,
		"API error 500 err":               true,
		"connection refused":              true,
		"bad request":                     false,
		"EOF parse error":                 false,
	}
	for msg, want := range cases {
		if got := retryable(&stringErr{msg}); got != want {
			t.Errorf("%q: got %v want %v", msg, got, want)
		}
	}
	if !retryable(io.EOF) {
		t.Error("io.EOF should be retryable")
	}
	if retryable(context.Canceled) {
		t.Error("context.Canceled should not be retryable")
	}
}

type stringErr struct{ s string }

func (e *stringErr) Error() string { return e.s }

func TestTailN(t *testing.T) {
	in := "a\nb\nc\nd\ne"
	out, hidden := tailN(in, 2)
	if out != "d\ne" || hidden != 3 {
		t.Fatalf("bad tail: out=%q hidden=%d", out, hidden)
	}
	out, hidden = tailN("x\ny", 10)
	if out != "x\ny" || hidden != 0 {
		t.Fatalf("should pass through: out=%q hidden=%d", out, hidden)
	}
}

func TestChatStreamingToolCallOrder(t *testing.T) {
	chunks := []string{
		`data: {"choices":[{"delta":{"content":"hello "}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":1,"id":"b","type":"function","function":{"name":"second","arguments":"{\"x\":"}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"a","type":"function","function":{"name":"first","arguments":"{}"}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":1,"function":{"arguments":"2}"}}]}}]}`,
		`data: [DONE]`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, c := range chunks {
			w.Write([]byte(c + "\n"))
		}
	}))
	defer srv.Close()

	c, err := NewClient(Provider{Name: "test", BaseURL: srv.URL, Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	var tokens []string
	msg, err := c.Chat(context.Background(), nil, nil, func(t string) { tokens = append(tokens, t) })
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(tokens, "") != "hello " || msg.Content != "hello " {
		t.Fatalf("content mismatch: %q %v", msg.Content, tokens)
	}
	if len(msg.ToolCalls) != 2 {
		t.Fatalf("got %d tool calls", len(msg.ToolCalls))
	}
	if msg.ToolCalls[0].Function.Name != "first" || msg.ToolCalls[1].Function.Name != "second" {
		t.Fatalf("bad order: %+v", msg.ToolCalls)
	}
	if msg.ToolCalls[1].Function.Arguments != `{"x":2}` {
		t.Fatalf("args not assembled: %q", msg.ToolCalls[1].Function.Arguments)
	}
}

func TestNewClientRequiresBaseURL(t *testing.T) {
	_, err := NewClient(Provider{Name: "test", Model: "m"})
	if err == nil || !strings.Contains(err.Error(), "no base_url") {
		t.Fatalf("expected missing base_url error, got %v", err)
	}
}
