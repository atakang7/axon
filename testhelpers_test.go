package axon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// sseDelta is one streamed chunk. Exactly the shape client.go's consume parses.
//
// Setting ToolName/ToolArgs emits a tool_calls fragment instead of text, which
// is what lets a test distinguish "the provider sent tool calls" from "the
// provider sent the tool-call syntax as prose".
type sseDelta struct {
	Content   string
	Reasoning string

	ToolCallID string
	ToolName   string
	ToolArgs   string
}

// fakeProvider is an OpenAI-compatible endpoint that replays a scripted
// stream and records every request body it was sent, so a test can assert on
// what actually crossed the wire.
type fakeProvider struct {
	server   *httptest.Server
	requests []map[string]any
}

// newFakeProvider starts a server that answers /v1/chat/completions by
// streaming deltas back as text/event-stream.
func newFakeProvider(t *testing.T, deltas ...sseDelta) *fakeProvider {
	t.Helper()
	return newFakeProviderFunc(t, func(int) []sseDelta { return deltas })
}

// newFakeProviderFunc is newFakeProvider where the reply depends on which
// call this is (0-indexed).
func newFakeProviderFunc(t *testing.T, reply func(call int) []sseDelta) *fakeProvider {
	t.Helper()

	p := &fakeProvider{}

	p.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("provider got undecodable request body: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		call := len(p.requests)
		p.requests = append(p.requests, body)

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("httptest ResponseWriter is not a Flusher")
		}

		for _, d := range reply(call) {
			delta := map[string]any{
				"content":           d.Content,
				"reasoning_content": d.Reasoning,
			}
			if d.ToolName != "" || d.ToolArgs != "" {
				delta["tool_calls"] = []map[string]any{{
					"index": 0,
					"id":    d.ToolCallID,
					"type":  "function",
					"function": map[string]any{
						"name":      d.ToolName,
						"arguments": d.ToolArgs,
					},
				}}
			}
			raw, _ := json.Marshal(map[string]any{
				"choices": []map[string]any{{"delta": delta}},
			})
			fmt.Fprintf(w, "data: %s\n\n", raw)
			flusher.Flush()
		}

		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))

	t.Cleanup(p.server.Close)
	return p
}

// stateBlock is the view's closing system block: the goal and the index of
// what is out of context.
func stateBlock(view []Msg) string {
	if n := len(view); n > 0 && view[n-1].Role == "system" {
		return view[n-1].Content
	}
	return ""
}
