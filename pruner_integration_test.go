package axon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Integration harness
//
// These tests drive the pruner the way production does: a real *Client
// speaking the OpenAI wire protocol over a real HTTP server, real SSE framing,
// real Session persistence. The unit tests in pruner_test.go stub the Model
// interface, which means they cannot see any failure that originates between
// "the pruner asked for a completion" and "a *Msg came back" — and that gap is
// exactly where the empty-response failures live.
// ---------------------------------------------------------------------------

// sseDelta is one streamed chunk. Exactly the shape client.go's consume parses.
type sseDelta struct {
	Content   string
	Reasoning string
}

// prunerProvider is an OpenAI-compatible endpoint that replays a scripted
// stream and records every request body it was sent, so a test can assert on
// what actually crossed the wire.
type prunerProvider struct {
	server   *httptest.Server
	requests []map[string]any
}

// newPrunerProvider starts a server that answers /v1/chat/completions by
// streaming deltas back as text/event-stream.
func newPrunerProvider(t *testing.T, deltas ...sseDelta) *prunerProvider {
	t.Helper()
	return newPrunerProviderFunc(t, func(int) []sseDelta { return deltas })
}

// newPrunerProviderFunc is newPrunerProvider where the reply depends on which
// call this is (0-indexed), for tests that need the curator to answer
// differently on a second pass.
func newPrunerProviderFunc(t *testing.T, reply func(call int) []sseDelta) *prunerProvider {
	t.Helper()

	p := &prunerProvider{}

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
			chunk := map[string]any{
				"choices": []map[string]any{{
					"delta": map[string]any{
						"content":           d.Content,
						"reasoning_content": d.Reasoning,
					},
				}},
			}
			raw, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", raw)
			flusher.Flush()
		}

		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))

	t.Cleanup(p.server.Close)
	return p
}

// curatorPrompt returns the user-role prompt sent on the given call — the
// rendered task + log the curator was actually asked to judge.
func (p *prunerProvider) curatorPrompt(t *testing.T, call int) string {
	t.Helper()
	if call >= len(p.requests) {
		t.Fatalf("provider received %d calls, wanted prompt from call %d", len(p.requests), call)
	}
	msgs, ok := p.requests[call]["messages"].([]any)
	if !ok {
		t.Fatalf("call %d has no messages array", call)
	}
	for _, raw := range msgs {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if m["role"] == "user" {
			s, _ := m["content"].(string)
			return s
		}
	}
	t.Fatalf("call %d had no user message", call)
	return ""
}

// livePruner builds a Pruner whose model is a real Client pointed at the test
// server, with the production defaults for everything the test does not set.
func livePruner(t *testing.T, p *prunerProvider, settings PrunerSettings) *Pruner {
	t.Helper()

	client, err := NewClient(ClientConfig{
		Provider: Provider{
			Name:    "test",
			BaseURL: p.server.URL,
			Model:   "curator-model",
			APIKey:  "test-key",
		},
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	pruner := NewPruner(PrunerConfig{Model: client, Settings: settings})
	if pruner == nil {
		t.Fatal("NewPruner returned nil for a non-nil model")
	}
	return pruner
}

// ---------------------------------------------------------------------------
// Artificial context
// ---------------------------------------------------------------------------

// blockSpec describes one block to synthesise into a session.
type blockSpec struct {
	role     string
	toolName string
	bytes    int
	label    string
}

// buildContext appends the given blocks to a fresh session, mirroring how the
// turn loop actually records them: tool results carry the "[#mN]\n" prefix
// loop.go prepends, and every block gets content so it earns an ID.
func buildContext(t *testing.T, specs ...blockSpec) *Session {
	t.Helper()

	s := newPrunerSession(t)
	for _, spec := range specs {
		label := spec.label
		if label == "" {
			label = spec.role
		}
		content := label + "\n" + strings.Repeat("x", spec.bytes)

		s.Append(Msg{Role: spec.role, Content: content, ToolName: spec.toolName})

		if spec.role == "tool" {
			last := &s.Messages[len(s.Messages)-1]
			if last.ID != "" {
				last.Content = "[#" + last.ID + "]\n" + last.Content
			}
		}
	}
	return s
}

// explorationSession synthesises the shape a real coding session reaches: a
// user request, then repeated read/search cycles whose tool output dominates
// the context, then a recent turn still in play.
func explorationSession(t *testing.T, cycles, resultBytes int) *Session {
	t.Helper()

	specs := []blockSpec{
		{role: "user", bytes: 200, label: "USER: refactor the session layer"},
	}
	for i := 0; i < cycles; i++ {
		specs = append(specs,
			blockSpec{role: "assistant", bytes: 100, label: fmt.Sprintf("ASSISTANT: reading file %d", i)},
			blockSpec{role: "tool", toolName: "read", bytes: resultBytes, label: fmt.Sprintf("FILE DUMP %d", i)},
		)
	}
	specs = append(specs,
		blockSpec{role: "user", bytes: 120, label: "USER: now write the fix"},
		blockSpec{role: "assistant", bytes: 300, label: "ASSISTANT: here is the plan"},
	)

	return buildContext(t, specs...)
}

// parkedIDs lists the blocks currently parked, in log order.
func parkedIDs(s *Session) []string {
	var out []string
	for _, m := range s.Messages {
		if m.Parked {
			out = append(out, m.ID)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// I1: the curator's net effect on context size
// ---------------------------------------------------------------------------

// The parking mechanism itself is sound: with the window off, the curator sees
// full blocks, parks them, and the projection genuinely shrinks. This is the
// control for the test below it.
func TestIntegrationCuratorReclaimsTokensWithWindowOff(t *testing.T) {
	s := explorationSession(t, 12, 4000)

	provider := newPrunerProvider(t,
		sseDelta{Content: `{"park":`},
		sseDelta{Content: `[3,5,7]}`},
	)
	p := livePruner(t, provider, PrunerSettings{WindowBlocks: 0, FloorTokens: 1, GrowthTokens: 1})

	before := p.ContextTokens(s)
	after, err := p.Prune(context.Background(), s)
	if err != nil {
		t.Fatalf("Prune over real transport failed: %v", err)
	}

	parked := parkedIDs(s)
	if len(parked) != 3 {
		t.Fatalf("parked %v, want 3 blocks (m3, m5, m7)", parked)
	}
	if after >= before {
		t.Fatalf("context did not shrink: before=%d after=%d", before, after)
	}
	t.Logf("window off: %d -> %d tokens, parked %v (%d reclaimed)",
		before, after, parked, before-after)
}

// With the window ON — the shipped default — parking cannot reclaim anything,
// and in fact makes the context slightly LARGER.
//
// The two mechanisms compose into a dead end. The window already collapsed
// every block the curator is allowed to see into
//
//	[#mN parked | reason: outside recency window | gist: ...]
//
// and parking one only rewrites that same breadcrumb with a different reason
// string. "pruner: not needed to continue" is longer than "outside recency
// window", so each park costs a handful of characters and saves none.
//
// Every curator call under the default configuration is therefore pure loss:
// the money, the latency, and a net increase in the thing it was called to
// reduce.
func TestIntegrationCuratorCannotReclaimTokensWithWindowOn(t *testing.T) {
	s := explorationSession(t, 12, 4000)

	provider := newPrunerProvider(t, sseDelta{Content: `{"park":[3,5,7]}`})
	p := livePruner(t, provider, PrunerSettings{WindowBlocks: 6, FloorTokens: 1, GrowthTokens: 1})

	before := p.ContextTokens(s)
	after, err := p.Prune(context.Background(), s)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}

	if len(parkedIDs(s)) != 3 {
		t.Fatalf("parked %v, want 3 blocks", parkedIDs(s))
	}
	if after < before {
		t.Fatalf("context shrank by %d tokens — the window/curator overlap "+
			"documented here has been fixed; update this test", before-after)
	}

	t.Logf("NET LOSS: parked 3 blocks and context went %d -> %d tokens (+%d). "+
		"Every candidate the curator sees was already collapsed by the window; "+
		"parking only swaps one breadcrumb reason for a longer one.",
		before, after, after-before)
}

// ---------------------------------------------------------------------------
// I2: the reported failure — "empty responses everywhere"
// ---------------------------------------------------------------------------

// A reasoning model that emits its entire answer as reasoning_content and
// never produces a content delta yields Msg.Content == "", which Prune
// reports as "pruner: empty response".
//
// This is not a hypothetical: the pruner prompt asks for a bare JSON object,
// which is precisely the kind of short answer a reasoning model spends its
// whole budget deliberating over and then truncates before emitting. Nothing
// in the client promotes reasoning to content, and nothing in Prune looks at
// Msg.Reasoning, so the parked-nothing outcome is indistinguishable from a
// dead provider.
func TestIntegrationEmptyResponseWhenModelAnswersInReasoning(t *testing.T) {
	s := explorationSession(t, 12, 4000)

	provider := newPrunerProvider(t,
		sseDelta{Reasoning: "The user wants me to park stale blocks. "},
		sseDelta{Reasoning: `Blocks 3, 5 and 7 are superseded file reads. {"park":[3,5,7]}`},
	)
	p := livePruner(t, provider, PrunerSettings{WindowBlocks: 6, FloorTokens: 1, GrowthTokens: 1})

	_, err := p.Prune(context.Background(), s)
	if err == nil {
		t.Fatal("expected an error when the model answered only in reasoning_content")
	}
	if !strings.Contains(err.Error(), "empty response") {
		t.Fatalf("error = %v, want it to mention an empty response", err)
	}
	if got := parkedIDs(s); len(got) != 0 {
		t.Fatalf("parked %v despite empty response, want nothing", got)
	}

	t.Logf("REPRODUCED: model put a valid park list in reasoning_content; "+
		"Prune saw empty content and parked nothing. err=%v", err)
}

// A provider that opens the stream and closes it without a single delta —
// what a truncated or filtered reply looks like on the wire — produces the
// same indistinguishable failure.
func TestIntegrationEmptyResponseWhenStreamCarriesNoDeltas(t *testing.T) {
	s := explorationSession(t, 12, 4000)

	provider := newPrunerProvider(t) // opens, sends [DONE], closes
	p := livePruner(t, provider, PrunerSettings{WindowBlocks: 6, FloorTokens: 1, GrowthTokens: 1})

	before := p.ContextTokens(s)
	after, err := p.Prune(context.Background(), s)
	if err == nil || !strings.Contains(err.Error(), "empty response") {
		t.Fatalf("err = %v, want an empty-response error", err)
	}
	if after != before {
		t.Fatalf("failed prune changed the projection: before=%d after=%d", before, after)
	}
}

// After an empty response the pruner must not retry on the very next loop
// iteration — a curator that is returning nothing would otherwise be called
// on every tool call for the rest of the turn, each time paying its timeout.
func TestIntegrationEmptyResponseDoesNotRetryImmediately(t *testing.T) {
	s := explorationSession(t, 12, 4000)

	provider := newPrunerProvider(t)
	p := livePruner(t, provider, PrunerSettings{WindowBlocks: 6, FloorTokens: 1, GrowthTokens: 5000})

	if _, err := p.Prune(context.Background(), s); err == nil {
		t.Fatal("expected empty-response error")
	}
	if p.ShouldFire(s) {
		t.Fatal("pruner would fire again immediately after an empty response — " +
			"every subsequent tool call in the turn pays the curator timeout")
	}
	if len(provider.requests) != 1 {
		t.Fatalf("provider saw %d calls, want 1", len(provider.requests))
	}
}

// ---------------------------------------------------------------------------
// I3: what the curator is actually shown
// ---------------------------------------------------------------------------

// The window collapses old blocks to breadcrumbs for free, and prunerRequest
// deliberately shows the curator only what is already outside the window. Those
// two facts compose into a bug: the blocks costing tokens are the ones inside
// the window, and the curator is structurally forbidden from seeing them, so a
// perfectly obedient curator that parks everything it is offered reclaims
// almost nothing.
func TestIntegrationCuratorCanOnlyParkBlocksThatAreAlreadyFree(t *testing.T) {
	// Ten small old blocks, then a window's worth of very large recent ones.
	var specs []blockSpec
	for i := 0; i < 10; i++ {
		specs = append(specs, blockSpec{role: "tool", toolName: "search", bytes: 200,
			label: fmt.Sprintf("OLD SEARCH %d", i)})
	}
	for i := 0; i < 8; i++ {
		specs = append(specs, blockSpec{role: "tool", toolName: "read", bytes: 20000,
			label: fmt.Sprintf("RECENT FILE %d", i)})
	}
	specs = append(specs,
		blockSpec{role: "user", bytes: 100, label: "USER: continue"},
		blockSpec{role: "assistant", bytes: 100, label: "ASSISTANT: working"},
	)
	s := buildContext(t, specs...)

	// A curator that parks every single id it is offered — the best possible
	// case for reclaiming tokens.
	provider := newPrunerProviderFunc(t, func(int) []sseDelta {
		var ids []string
		for i := 1; i <= 10; i++ {
			ids = append(ids, fmt.Sprint(i))
		}
		return []sseDelta{{Content: `{"park":[` + strings.Join(ids, ",") + `]}`}}
	})
	p := livePruner(t, provider, PrunerSettings{WindowBlocks: 10, FloorTokens: 1, GrowthTokens: 1})

	before := p.ContextTokens(s)
	after, err := p.Prune(context.Background(), s)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}

	prompt := provider.curatorPrompt(t, 0)
	if strings.Contains(prompt, "RECENT FILE") {
		t.Fatal("precondition broken: curator was shown in-window blocks")
	}

	reclaimed := before - after
	pct := float64(reclaimed) / float64(before) * 100

	t.Logf("curator parked %d/%d blocks; context %d -> %d tokens (%.1f%% reclaimed)",
		len(parkedIDs(s)), len(s.Messages), before, after, pct)
	t.Logf("the 8 in-window file dumps hold ~%d tokens and no layer can touch them",
		8*20000/4)

	if pct > 5 {
		t.Fatalf("expected the curator to be structurally unable to reclaim much "+
			"(it only sees already-collapsed blocks), but it reclaimed %.1f%%", pct)
	}
	if after < p.floor {
		t.Fatal("precondition broken: context fell below the floor on its own")
	}
}

// A session whose active blocks all fit inside the window renders a log with
// no candidates at all. ShouldFire still fires on it, so the runtime pays for
// a full curator round trip whose only possible correct answer is {"park":[]}.
func TestIntegrationCuratorIsCalledWithAnEmptyLog(t *testing.T) {
	// Five blocks, window of thirty: nothing is ever outside it.
	s := buildContext(t,
		blockSpec{role: "user", bytes: 100, label: "USER: start"},
		blockSpec{role: "assistant", bytes: 100, label: "ASSISTANT: reading"},
		blockSpec{role: "tool", toolName: "read", bytes: 60000, label: "HUGE FILE"},
		blockSpec{role: "assistant", bytes: 100, label: "ASSISTANT: done reading"},
		blockSpec{role: "user", bytes: 100, label: "USER: continue"},
	)

	provider := newPrunerProvider(t, sseDelta{Content: `{"park":[]}`})
	p := livePruner(t, provider, PrunerSettings{WindowBlocks: 30, FloorTokens: 10000, GrowthTokens: 5000})

	if !p.ShouldFire(s) {
		t.Fatal("precondition broken: context should be over the floor")
	}
	if _, err := p.Prune(context.Background(), s); err != nil {
		t.Fatalf("Prune: %v", err)
	}

	prompt := provider.curatorPrompt(t, 0)
	_, log, found := strings.Cut(prompt, "# LOG\n")
	if !found {
		t.Fatal("curator prompt had no LOG section")
	}
	log, _, _ = strings.Cut(log, "\n\nCRITICAL REMINDER")

	if strings.TrimSpace(log) != "" {
		t.Fatalf("expected an empty log section, got %d bytes", len(log))
	}
	t.Logf("WASTED CALL: context is %d tokens (over the %d floor) but every block "+
		"is inside the window, so the curator was billed for a log containing "+
		"zero candidates", p.ContextTokens(s), p.floor)
}

// The curator names blocks by the integer in "mN". Confirm the prompt it
// receives really does carry the ids the parked-block loop will parse back,
// since a mismatch here parks the wrong content rather than failing loudly.
func TestIntegrationCuratorPromptIDsRoundTrip(t *testing.T) {
	s := explorationSession(t, 10, 3000)

	provider := newPrunerProvider(t, sseDelta{Content: `{"park":[4]}`})
	p := livePruner(t, provider, PrunerSettings{WindowBlocks: 4, FloorTokens: 1, GrowthTokens: 1})

	if _, err := p.Prune(context.Background(), s); err != nil {
		t.Fatalf("Prune: %v", err)
	}

	prompt := provider.curatorPrompt(t, 0)
	if !strings.Contains(prompt, "[m4 |") {
		t.Fatalf("prompt does not label block m4; curator cannot name it:\n%s",
			firstLines(prompt, 20))
	}

	parked := parkedIDs(s)
	if len(parked) != 1 || parked[0] != "m4" {
		t.Fatalf("parked %v, want [m4]", parked)
	}
}

// ---------------------------------------------------------------------------
// I4: blocks no layer can reach
// ---------------------------------------------------------------------------

// Session.Append only assigns a block ID when Content is non-empty. An
// assistant message that is purely a tool call — a write whose entire file
// body sits in Function.Arguments — therefore never gets an ID, is skipped by
// windowCutoff and by prunerRequest, and counts toward ContextTokens forever.
//
// This is the single largest category of unprunable context in a coding agent,
// because writes carry whole files.
func TestIntegrationToolCallOnlyBlocksAreUnprunable(t *testing.T) {
	s := explorationSession(t, 4, 500)

	var write ToolCall
	write.ID = "call_write_1"
	write.Type = "function"
	write.Function.Name = "write"
	write.Function.Arguments = `{"path":"main.go","content":"` + strings.Repeat("y", 60000) + `"}`
	s.Append(Msg{Role: "assistant", ToolCalls: []ToolCall{write}}) // no Content

	s.Append(Msg{Role: "tool", ToolName: "write", ToolCallID: "call_write_1", Content: "wrote main.go"})
	s.Append(Msg{Role: "user", Content: "thanks, now continue"})

	var idless int
	for _, m := range s.Messages {
		if m.Role != "system" && m.ID == "" {
			idless++
		}
	}
	if idless == 0 {
		t.Skip("Append now assigns IDs to tool-call-only messages; bug is fixed")
	}

	// The curator parks everything it can see.
	provider := newPrunerProviderFunc(t, func(int) []sseDelta {
		var ids []string
		for i := 1; i <= 20; i++ {
			ids = append(ids, fmt.Sprint(i))
		}
		return []sseDelta{{Content: `{"park":[` + strings.Join(ids, ",") + `]}`}}
	})
	p := livePruner(t, provider, PrunerSettings{WindowBlocks: 2, FloorTokens: 1, GrowthTokens: 1})

	prompt := ""
	before := p.ContextTokens(s)
	after, _ := p.Prune(context.Background(), s)
	prompt = provider.curatorPrompt(t, 0)

	if strings.Contains(prompt, "main.go") {
		t.Fatal("precondition broken: the write's arguments reached the curator")
	}

	// Whatever else was parked, the 60KB of arguments survives in full.
	remaining := 0
	for _, m := range s.ContextMessages(p.window) {
		for _, tc := range m.ToolCalls {
			remaining += len(tc.Function.Arguments)
		}
	}

	t.Logf("UNPRUNABLE: %d id-less block(s); context %d -> %d tokens with "+
		"%d chars of tool-call arguments still resident and invisible to the curator",
		idless, before, after, remaining)

	if remaining < 60000 {
		t.Fatalf("expected the 60KB write payload to survive pruning, found %d chars", remaining)
	}
}

// ---------------------------------------------------------------------------
// I5: provider-shaped replies the parser must survive
// ---------------------------------------------------------------------------

// Real curator models wrap their JSON in prose, in markdown fences, or split
// it across chunk boundaries mid-token. Each of these must still park.
func TestIntegrationRealisticReplyShapes(t *testing.T) {
	cases := []struct {
		name   string
		deltas []sseDelta
		want   int
	}{
		{
			name:   "bare json",
			deltas: []sseDelta{{Content: `{"park":[3,5]}`}},
			want:   2,
		},
		{
			name: "markdown fenced",
			deltas: []sseDelta{
				{Content: "```json\n"},
				{Content: `{"park":[3,5]}`},
				{Content: "\n```"},
			},
			want: 2,
		},
		{
			name: "prose before and after",
			deltas: []sseDelta{
				{Content: "Looking at the log, blocks 3 and 5 are superseded.\n\n"},
				{Content: `{"park":[3,5]}`},
				{Content: "\n\nEverything else is still load-bearing."},
			},
			want: 2,
		},
		{
			name: "split mid-token across chunks",
			deltas: []sseDelta{
				{Content: `{"pa`}, {Content: `rk":[`}, {Content: `3,`}, {Content: `5]}`},
			},
			want: 2,
		},
		{
			name: "reasoning then content",
			deltas: []sseDelta{
				{Reasoning: "block 3 is a superseded read; block 5 led nowhere"},
				{Content: `{"park":[3,5]}`},
			},
			want: 2,
		},
		{
			name:   "explicit keep-everything",
			deltas: []sseDelta{{Content: `{"park":[]}`}},
			want:   0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := explorationSession(t, 12, 3000)
			provider := newPrunerProvider(t, tc.deltas...)
			p := livePruner(t, provider, PrunerSettings{WindowBlocks: 6, FloorTokens: 1, GrowthTokens: 1})

			if _, err := p.Prune(context.Background(), s); err != nil {
				t.Fatalf("Prune: %v", err)
			}
			if got := len(parkedIDs(s)); got != tc.want {
				t.Fatalf("parked %d blocks (%v), want %d", got, parkedIDs(s), tc.want)
			}
		})
	}
}

// The curator's reply cap is small by design. Assert it is actually sent, since
// a provider that never receives it will let a chatty model burn its context
// budget on prose — and a cap set too low is itself a cause of empty replies
// on reasoning models, which spend it before emitting any content.
func TestIntegrationCuratorReplyCapReachesTheWire(t *testing.T) {
	s := explorationSession(t, 12, 3000)

	provider := newPrunerProvider(t, sseDelta{Content: `{"park":[]}`})
	p := livePruner(t, provider, PrunerSettings{WindowBlocks: 6, FloorTokens: 1, GrowthTokens: 1, MaxTokens: 512})

	if _, err := p.Prune(context.Background(), s); err != nil {
		t.Fatalf("Prune: %v", err)
	}

	got, ok := provider.requests[0]["max_tokens"].(float64)
	if !ok {
		t.Fatal("request carried no max_tokens")
	}
	if int(got) != 512 {
		t.Fatalf("max_tokens = %d, want 512", int(got))
	}
	if _, sentTools := provider.requests[0]["tools"]; sentTools {
		t.Error("curator request carried tool definitions; it must not be able to act")
	}
}

// ---------------------------------------------------------------------------
// I6: transport failures stay recoverable
// ---------------------------------------------------------------------------

// A curator that returns a non-2xx must surface the provider's own body —
// "empty response" and "model not found" call for very different fixes, and a
// pruner that flattens both to the former is why this failure looked mysterious.
func TestIntegrationProviderErrorSurfacesBody(t *testing.T) {
	s := explorationSession(t, 12, 3000)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error":{"message":"model curator-model does not exist","code":"model_not_found"}}`)
	}))
	t.Cleanup(server.Close)

	client, err := NewClient(ClientConfig{Provider: Provider{
		BaseURL: server.URL, Model: "curator-model", APIKey: "k",
	}})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	p := NewPruner(PrunerConfig{Model: client, Settings: PrunerSettings{
		WindowBlocks: 6, FloorTokens: 1, GrowthTokens: 1,
	}})

	_, err = p.Prune(context.Background(), s)
	if err == nil {
		t.Fatal("expected an error from a 404 provider")
	}
	if !strings.Contains(err.Error(), "model_not_found") {
		t.Fatalf("error lost the provider's explanation: %v", err)
	}
	if strings.Contains(err.Error(), "empty response") {
		t.Fatalf("a 404 was reported as an empty response: %v", err)
	}
}

// A provider that opens the stream and then goes silent must fail on the idle
// timeout rather than holding the turn, and must leave the session untouched.
func TestIntegrationStalledProviderLeavesSessionIntact(t *testing.T) {
	s := explorationSession(t, 12, 3000)
	release := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-release
	}))
	t.Cleanup(func() { close(release); server.Close() })

	client, err := NewClient(ClientConfig{
		Provider:    Provider{BaseURL: server.URL, Model: "curator-model"},
		IdleTimeout: 150 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	p := NewPruner(PrunerConfig{Model: client, Settings: PrunerSettings{
		WindowBlocks: 6, FloorTokens: 1, GrowthTokens: 1,
	}})

	before := p.ContextTokens(s)
	start := time.Now()
	after, err := p.Prune(context.Background(), s)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected a stall error")
	}
	if elapsed > 5*time.Second {
		t.Fatalf("stalled provider held the turn for %s", elapsed)
	}
	if after != before || len(parkedIDs(s)) != 0 {
		t.Fatalf("stalled prune mutated the session: before=%d after=%d parked=%v",
			before, after, parkedIDs(s))
	}
	t.Logf("stall detected in %s: %v", elapsed.Round(time.Millisecond), err)
}

// ---------------------------------------------------------------------------
// I7: against a live provider
// ---------------------------------------------------------------------------

// Run with the curator you actually configured to see what it returns for a
// realistic log:
//
//	AXON_LIVE_BASE_URL=... AXON_LIVE_MODEL=... AXON_LIVE_API_KEY=... \
//	  go test -run TestIntegrationLiveCurator -v
//
// It asserts almost nothing on purpose. Its job is to show the raw reply, so a
// curator that answers in reasoning, refuses the task, or ignores the format is
// visible rather than flattened into "pruner: empty response".
func TestIntegrationLiveCurator(t *testing.T) {
	base, model := os.Getenv("AXON_LIVE_BASE_URL"), os.Getenv("AXON_LIVE_MODEL")
	if base == "" || model == "" {
		t.Skip("set AXON_LIVE_BASE_URL and AXON_LIVE_MODEL to exercise a real curator")
	}

	s := explorationSession(t, 14, 4000)

	client, err := NewClient(ClientConfig{Provider: Provider{
		Name: "live", BaseURL: base, Model: model, APIKey: os.Getenv("AXON_LIVE_API_KEY"),
	}})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	settings := PrunerSettings{WindowBlocks: 8, FloorTokens: 1, GrowthTokens: 1}
	p := NewPruner(PrunerConfig{Model: client, Settings: settings})

	// Ask the model directly first, so the raw reply is visible even when
	// Prune would reject it.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	reply, err := client.Complete(ctx, Request{
		Messages: []Msg{
			{Role: "system", Content: prunerSystemPrompt(p.mode)},
			{Role: "user", Content: prunerRequest(s, p.window, defaultPrunerReminder)},
		},
		MaxTokens: p.maxTokens,
	})
	if err != nil {
		t.Fatalf("live curator call failed: %v", err)
	}

	t.Logf("content   (%d chars): %q", len(reply.Content), truncate(reply.Content, 500))
	t.Logf("reasoning (%d chars): %q", len(reply.Reasoning), truncate(reply.Reasoning, 500))

	if strings.TrimSpace(reply.Content) == "" {
		t.Errorf("LIVE CURATOR RETURNED EMPTY CONTENT (reasoning was %d chars) — "+
			"this is the production failure; the model %q is not usable as a "+
			"curator through the current request shape", len(reply.Reasoning), model)
		return
	}

	ids, err := parkList(reply.Content)
	if err != nil {
		t.Errorf("live curator reply is not parseable: %v", err)
		return
	}
	t.Logf("live curator asked to park %d block(s): %v", len(ids), ids)

	before := p.ContextTokens(s)
	after, err := p.Prune(ctx, s)
	t.Logf("live Prune: %d -> %d tokens, parked %v, err=%v",
		before, after, parkedIDs(s), err)
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func firstLines(s string, n int) string {
	lines := strings.SplitN(s, "\n", n+1)
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}
