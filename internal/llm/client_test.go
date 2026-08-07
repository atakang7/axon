package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// NewClient / requestBody
// ---------------------------------------------------------------------------

// A provider with no base_url cannot be talked to; failing at construction
// beats failing on the first request with a confusing "no host" error.
func TestNewClientRejectsEmptyBaseURL(t *testing.T) {
	_, err := NewClient(ClientConfig{Provider: Provider{Name: "x"}})
	if err == nil {
		t.Fatal("NewClient accepted an empty BaseURL")
	}
}

// Every OpenAI-compatible endpoint lives under /v1. Appending it once, and not
// twice when the caller already included it, is what keeps requestBody's URL
// construction correct regardless of how the embedder wrote BaseURL.
func TestNewClientNormalisesBaseURL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"bare host gets /v1 appended", "https://api.example.com", "https://api.example.com/v1"},
		{"trailing slash trimmed before appending", "https://api.example.com/", "https://api.example.com/v1"},
		{"existing /v1 is not doubled", "https://api.example.com/v1", "https://api.example.com/v1"},
		{"existing /v1 with trailing slash trimmed first, not doubled", "https://api.example.com/v1/", "https://api.example.com/v1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := NewClient(ClientConfig{Provider: Provider{Name: "x", BaseURL: tc.in, Model: "m"}})
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}
			if c.baseURL != tc.want {
				t.Fatalf("baseURL = %q, want %q", c.baseURL, tc.want)
			}
		})
	}
}

// Model() is what an embedder logs and what the session header shows; it must
// report the provider's model id, not the provider name or base URL.
func TestModelReportsProviderModel(t *testing.T) {
	c, err := NewClient(ClientConfig{Provider: Provider{BaseURL: "https://x", Model: "gpt-5"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Model(); got != "gpt-5" {
		t.Fatalf("Model() = %q, want %q", got, "gpt-5")
	}
}

// requestBody is the one place the wire format is assembled; every field the
// provider requires (streaming, parallel tool calls, model id, and each tool
// projected to the OpenAI function-calling shape) must actually be present.
func TestRequestBodyShapesCoreFields(t *testing.T) {
	c, err := NewClient(ClientConfig{Provider: Provider{BaseURL: "https://x", Model: "gpt-5"}})
	if err != nil {
		t.Fatal(err)
	}
	req := Request{
		Messages: []Msg{{Role: "user", Content: "hi"}},
		Tools: []ToolSpec{
			{Name: "read", Description: "reads a file", Schema: map[string]any{"type": "object"}},
		},
	}
	raw, err := c.requestBody(req)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("requestBody produced invalid JSON: %v", err)
	}
	if body["stream"] != true {
		t.Fatalf("stream = %v, want true", body["stream"])
	}
	if body["parallel_tool_calls"] != true {
		t.Fatalf("parallel_tool_calls = %v, want true", body["parallel_tool_calls"])
	}
	if body["model"] != "gpt-5" {
		t.Fatalf("model = %v, want gpt-5", body["model"])
	}
	tools, ok := body["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %v, want one entry", body["tools"])
	}
	tool := tools[0].(map[string]any)
	if tool["type"] != "function" {
		t.Fatalf("tool type = %v, want function", tool["type"])
	}
	fn := tool["function"].(map[string]any)
	if fn["name"] != "read" || fn["description"] != "reads a file" {
		t.Fatalf("tool function = %v, missing name/description", fn)
	}
	if _, ok := fn["parameters"]; !ok {
		t.Fatal("tool function missing parameters (schema)")
	}
}

// MaxTokens has three levels of precedence, cheapest override wins: an
// explicit per-request value beats the client's configured default, which
// beats the library's own fallback. Getting the order wrong either silently
// ignores a caller's budget or blows past a provider's cap.
func TestRequestBodyMaxTokensPrecedence(t *testing.T) {
	cases := []struct {
		name          string
		clientMax     int
		reqMax        int
		wantMaxTokens float64
	}{
		{"request wins over client config", 5000, 100, 100},
		{"client config wins when request unset", 5000, 0, 5000},
		{"library default when neither set", 0, 0, defaultMaxTokens},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := NewClient(ClientConfig{Provider: Provider{BaseURL: "https://x", Model: "m"}, MaxTokens: tc.clientMax})
			if err != nil {
				t.Fatal(err)
			}
			raw, err := c.requestBody(Request{MaxTokens: tc.reqMax})
			if err != nil {
				t.Fatal(err)
			}
			var body map[string]any
			json.Unmarshal(raw, &body)
			if body["max_tokens"] != tc.wantMaxTokens {
				t.Fatalf("max_tokens = %v, want %v", body["max_tokens"], tc.wantMaxTokens)
			}
		})
	}
}

// The reasoning block is entirely optional wire, so it must be absent when
// neither field is configured, and the two fields (effort / exclude) must map
// to the exact keys OpenRouter documents, including the legacy
// include_reasoning fallback for exclude.
func TestRequestBodyReasoningFields(t *testing.T) {
	t.Run("neither field set means no reasoning key at all", func(t *testing.T) {
		c, _ := NewClient(ClientConfig{Provider: Provider{BaseURL: "https://x", Model: "m"}})
		raw, _ := c.requestBody(Request{})
		var body map[string]any
		json.Unmarshal(raw, &body)
		if _, ok := body["reasoning"]; ok {
			t.Fatalf("reasoning key present with nothing configured: %v", body["reasoning"])
		}
	})
	t.Run("ReasoningEffort sets reasoning.effort", func(t *testing.T) {
		c, _ := NewClient(ClientConfig{Provider: Provider{BaseURL: "https://x", Model: "m"}, ReasoningEffort: "low"})
		raw, _ := c.requestBody(Request{})
		var body map[string]any
		json.Unmarshal(raw, &body)
		reasoning, ok := body["reasoning"].(map[string]any)
		if !ok {
			t.Fatalf("no reasoning object: %v", body["reasoning"])
		}
		if reasoning["effort"] != "low" {
			t.Fatalf("reasoning.effort = %v, want low", reasoning["effort"])
		}
		if _, ok := reasoning["exclude"]; ok {
			t.Fatal("exclude set without ExcludeReasoning")
		}
	})
	t.Run("ExcludeReasoning sets exclude and include_reasoning", func(t *testing.T) {
		c, _ := NewClient(ClientConfig{Provider: Provider{BaseURL: "https://x", Model: "m"}, ExcludeReasoning: true})
		raw, _ := c.requestBody(Request{})
		var body map[string]any
		json.Unmarshal(raw, &body)
		reasoning, ok := body["reasoning"].(map[string]any)
		if !ok {
			t.Fatalf("no reasoning object: %v", body["reasoning"])
		}
		if reasoning["exclude"] != true {
			t.Fatalf("reasoning.exclude = %v, want true", reasoning["exclude"])
		}
		if body["include_reasoning"] != false {
			t.Fatalf("include_reasoning = %v, want false", body["include_reasoning"])
		}
	})
}

// Provider.Extra is opaque routing JSON the runtime must never interpret —
// only forward. A test that inspects its keys would be testing the wrong
// layer; the contract is verbatim passthrough as the "provider" field.
func TestRequestBodyForwardsProviderExtra(t *testing.T) {
	c, err := NewClient(ClientConfig{Provider: Provider{
		BaseURL: "https://x", Model: "m",
		Extra: json.RawMessage(`{"order":["a","b"],"allow_fallbacks":false}`),
	}})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := c.requestBody(Request{})
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	json.Unmarshal(raw, &body)
	provider, ok := body["provider"].(map[string]any)
	if !ok {
		t.Fatalf("provider field missing or wrong shape: %v", body["provider"])
	}
	if provider["allow_fallbacks"] != false {
		t.Fatalf("provider.allow_fallbacks = %v, want false", provider["allow_fallbacks"])
	}
	order, ok := provider["order"].([]any)
	if !ok || len(order) != 2 || order[0] != "a" {
		t.Fatalf("provider.order = %v, want [a b]", provider["order"])
	}
}

// ---------------------------------------------------------------------------
// readStream / consume / message — driven directly against an io.Reader, no
// network involved.
// ---------------------------------------------------------------------------

// sseLines turns a slice of raw "data: ..." payloads into the newline-joined
// text readStream expects to scan line by line.
func sseLines(lines ...string) io.Reader {
	return strings.NewReader(strings.Join(lines, "\n") + "\n")
}

func dataLine(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return "data: " + string(b)
}

type chunkDelta struct {
	Content          string          `json:"content,omitempty"`
	ReasoningContent string          `json:"reasoning_content,omitempty"`
	ToolCalls        []toolCallDelta `json:"tool_calls,omitempty"`
}

type toolCallDelta struct {
	Index    int    `json:"index"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function"`
}

func chunk(delta chunkDelta) string {
	return dataLine(map[string]any{
		"choices": []map[string]any{{"delta": delta}},
	})
}

// Content deltas must accumulate in arrival order into Msg.Content, and every
// delta must fire Stream.Token so a UI can render incrementally — losing
// either breaks either the final message or the live typing effect.
func TestReadStreamAccumulatesContentAndFiresToken(t *testing.T) {
	var tokens []string
	stream := Stream{Token: func(s string) { tokens = append(tokens, s) }}

	body := sseLines(
		chunk(chunkDelta{Content: "Hello, "}),
		chunk(chunkDelta{Content: "world"}),
		chunk(chunkDelta{Content: "!"}),
		"data: [DONE]",
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	msg, err := readStream(ctx, body, stream, cancel)
	if err != nil {
		t.Fatalf("readStream: %v", err)
	}
	if msg.Content != "Hello, world!" {
		t.Fatalf("Content = %q, want %q", msg.Content, "Hello, world!")
	}
	if strings.Join(tokens, "") != "Hello, world!" {
		t.Fatalf("Token deltas = %v, want to reassemble to the full content", tokens)
	}
}

// reasoning_content is a distinct channel from content: it must fire
// Stream.Reasoning and must never leak into Msg.Content, or a UI rendering
// the two separately would show the model's chain of thought as its answer.
func TestReadStreamSeparatesReasoningFromContent(t *testing.T) {
	var reasoning []string
	stream := Stream{Reasoning: func(s string) { reasoning = append(reasoning, s) }}

	body := sseLines(
		chunk(chunkDelta{ReasoningContent: "thinking..."}),
		chunk(chunkDelta{Content: "the answer"}),
		"data: [DONE]",
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	msg, err := readStream(ctx, body, stream, cancel)
	if err != nil {
		t.Fatalf("readStream: %v", err)
	}
	if msg.Content != "the answer" {
		t.Fatalf("Content = %q, reasoning leaked in", msg.Content)
	}
	if strings.Join(reasoning, "") != "thinking..." {
		t.Fatalf("Reasoning callback got %v", reasoning)
	}
}

// Providers stream tool-call arguments as fragments keyed by index; the
// client must concatenate same-index fragments into one Arguments string and
// fire ToolArgs per non-empty fragment carrying the name from the very first
// fragment (later fragments often omit the name entirely).
func TestReadStreamAssemblesToolCallArguments(t *testing.T) {
	var argEvents []string
	stream := Stream{ToolArgs: func(name, delta string) {
		argEvents = append(argEvents, fmt.Sprintf("%s:%s", name, delta))
	}}

	first := toolCallDelta{Index: 0}
	first.Function.Name = "read"
	first.Function.Arguments = `{"path":`
	second := toolCallDelta{Index: 0}
	second.Function.Arguments = `"x.go"}`

	body := sseLines(
		chunk(chunkDelta{ToolCalls: []toolCallDelta{first}}),
		chunk(chunkDelta{ToolCalls: []toolCallDelta{second}}),
		"data: [DONE]",
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	msg, err := readStream(ctx, body, stream, cancel)
	if err != nil {
		t.Fatalf("readStream: %v", err)
	}
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("ToolCalls = %v, want exactly one", msg.ToolCalls)
	}
	if got := msg.ToolCalls[0].Function.Arguments; got != `{"path":"x.go"}` {
		t.Fatalf("Arguments = %q, want the concatenation of both fragments", got)
	}
	if msg.ToolCalls[0].Function.Name != "read" {
		t.Fatalf("Function.Name = %q, want read", msg.ToolCalls[0].Function.Name)
	}
	wantEvents := []string{`read:{"path":`, `read:"x.go"}`}
	if fmt.Sprint(argEvents) != fmt.Sprint(wantEvents) {
		t.Fatalf("ToolArgs events = %v, want %v", argEvents, wantEvents)
	}
}

// Tool calls must come out ordered by the provider's own index, not by map
// iteration order (which Go randomises) — feeding index 1 before index 0
// must still yield [call0, call1].
func TestReadStreamOrdersToolCallsByIndexNotArrivalOrder(t *testing.T) {
	second := toolCallDelta{Index: 1}
	second.Function.Name = "search"
	second.Function.Arguments = `{"q":"b"}`
	first := toolCallDelta{Index: 0}
	first.Function.Name = "read"
	first.Function.Arguments = `{"path":"a"}`

	body := sseLines(
		chunk(chunkDelta{ToolCalls: []toolCallDelta{second}}),
		chunk(chunkDelta{ToolCalls: []toolCallDelta{first}}),
		"data: [DONE]",
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	msg, err := readStream(ctx, body, Stream{}, cancel)
	if err != nil {
		t.Fatalf("readStream: %v", err)
	}
	if len(msg.ToolCalls) != 2 {
		t.Fatalf("ToolCalls = %v, want two", msg.ToolCalls)
	}
	if msg.ToolCalls[0].Function.Name != "read" || msg.ToolCalls[1].Function.Name != "search" {
		t.Fatalf("ToolCalls out of index order: %+v", msg.ToolCalls)
	}
}

// A tool called with zero arguments must still produce valid JSON ("{}"), not
// an empty string — an empty Arguments field is rejected by providers on the
// follow-up request.
func TestReadStreamEmptyToolArgumentsBecomeEmptyObject(t *testing.T) {
	tc := toolCallDelta{Index: 0}
	tc.Function.Name = "noop"
	// No Arguments field at all — some providers announce a call with no
	// argument fragment ever following.
	body := sseLines(
		chunk(chunkDelta{ToolCalls: []toolCallDelta{tc}}),
		"data: [DONE]",
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	msg, err := readStream(ctx, body, Stream{}, cancel)
	if err != nil {
		t.Fatalf("readStream: %v", err)
	}
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("ToolCalls = %v, want one", msg.ToolCalls)
	}
	if got := msg.ToolCalls[0].Function.Arguments; got != "{}" {
		t.Fatalf("Arguments = %q, want the empty-object fallback", got)
	}
}

// [DONE], blank lines, SSE comments and malformed JSON are all routine noise
// in a real stream; none of them may abort assembly of the rest of the
// message, and content on either side of the noise must still be assembled.
func TestReadStreamSkipsNoiseWithoutAborting(t *testing.T) {
	body := sseLines(
		"",
		": this is a comment",
		"data: not json at all {{{",
		chunk(chunkDelta{Content: "before "}),
		"data: [DONE]",
		chunk(chunkDelta{Content: "after"}),
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	msg, err := readStream(ctx, body, Stream{}, cancel)
	if err != nil {
		t.Fatalf("readStream: %v", err)
	}
	if msg.Content != "before after" {
		t.Fatalf("Content = %q, want %q (noise must be skipped without aborting assembly)", msg.Content, "before after")
	}
}

// A chunk with zero choices (some providers send these as heartbeats or role
// announcements) must be ignored, not treated as an error or a blank delta.
func TestReadStreamIgnoresChunkWithNoChoices(t *testing.T) {
	body := sseLines(
		dataLine(map[string]any{"choices": []map[string]any{}}),
		chunk(chunkDelta{Content: "hi"}),
		"data: [DONE]",
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	msg, err := readStream(ctx, body, Stream{}, cancel)
	if err != nil {
		t.Fatalf("readStream: %v", err)
	}
	if msg.Content != "hi" {
		t.Fatalf("Content = %q, want %q", msg.Content, "hi")
	}
}

// A reply carrying no tool calls must have a nil ToolCalls slice, not an
// empty non-nil one — callers (and JSON marshalling with omitempty) depend on
// nil meaning "no tool calls happened".
func TestReadStreamNoToolCallsYieldsNilSlice(t *testing.T) {
	body := sseLines(chunk(chunkDelta{Content: "plain answer"}), "data: [DONE]")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	msg, err := readStream(ctx, body, Stream{}, cancel)
	if err != nil {
		t.Fatalf("readStream: %v", err)
	}
	if msg.Role != "assistant" {
		t.Fatalf("Role = %q, want assistant", msg.Role)
	}
	if msg.Content != "plain answer" {
		t.Fatalf("Content = %q", msg.Content)
	}
	if msg.ToolCalls != nil {
		t.Fatalf("ToolCalls = %v, want nil", msg.ToolCalls)
	}
}

// Caller cancellation must surface as ctx.Err(), not as a truncated or empty
// reply — an embedder distinguishes "the user hit stop" from "the provider
// broke" by this exact signal.
func TestReadStreamCallerCancellationSurfacesContextError(t *testing.T) {
	pr, pw := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())

	// Simulate a slow/unfinished stream: write one chunk, then let the caller
	// cancel before any more data or a close arrives.
	go func() {
		fmt.Fprintln(pw, chunk(chunkDelta{Content: "partial"}))
		time.Sleep(20 * time.Millisecond)
		cancel()
		time.Sleep(20 * time.Millisecond)
		pw.Close()
	}()

	_, err := readStream(ctx, pr, Stream{}, func() { pw.Close() })
	if err == nil {
		t.Fatal("readStream returned nil error after caller cancellation")
	}
	if err != context.Canceled {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

// ---------------------------------------------------------------------------
// Complete — end-to-end against httptest, still fully offline.
// ---------------------------------------------------------------------------

// An HTTP status of 300+ must fail with an error naming both the status and
// the response body (bounded to 4096 bytes) — a bare status code gives no clue
// which of a dozen things the provider objected to.
func TestCompleteErrorStatusIncludesBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"bad model name"}`))
	}))
	defer srv.Close()

	c, err := NewClient(ClientConfig{Provider: Provider{BaseURL: srv.URL, Model: "m"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Complete(context.Background(), Request{})
	if err == nil {
		t.Fatal("Complete returned nil error on a 400 response")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Fatalf("error %q does not mention the status", err)
	}
	if !strings.Contains(err.Error(), "bad model name") {
		t.Fatalf("error %q does not mention the body", err)
	}
}

// A reply with no tool calls must come back shaped exactly as
// {Role:"assistant", Content:...} with ToolCalls nil — this is the common
// case (a plain answer) and it must not carry incidental cruft.
func TestCompletePlainReplyShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, chunk(chunkDelta{Content: "hello there"}))
		fmt.Fprintln(w, "data: [DONE]")
	}))
	defer srv.Close()

	c, err := NewClient(ClientConfig{Provider: Provider{BaseURL: srv.URL, Model: "m"}})
	if err != nil {
		t.Fatal(err)
	}
	msg, err := c.Complete(context.Background(), Request{})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if msg.Role != "assistant" || msg.Content != "hello there" || msg.ToolCalls != nil {
		t.Fatalf("msg = %+v, want {assistant, hello there, nil}", msg)
	}
}

// Sanity: readStream must be safe to drive concurrently across independent
// Client instances, since Client claims to be safe for concurrent use and
// nothing here is mutated after construction.
func TestClientConcurrentComplete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, chunk(chunkDelta{Content: "ok"}))
		fmt.Fprintln(w, "data: [DONE]")
	}))
	defer srv.Close()

	c, err := NewClient(ClientConfig{Provider: Provider{BaseURL: srv.URL, Model: "m"}})
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := c.Complete(context.Background(), Request{})
			if err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent Complete: %v", err)
	}
}
