package axon

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAnthropicRequestsCarryCacheBreakpoints(t *testing.T) {
	msgs := []Msg{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "do it"},
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "c1", Type: "function"}}},
		{Role: "tool", ToolCallID: "c1", Content: "result"},
	}
	raw, _ := json.Marshal(cacheMarked(msgs, "anthropic/claude-sonnet-5.5"))
	if n := strings.Count(string(raw), `"cache_control"`); n != 3 {
		t.Fatalf("want breakpoints on system and the last two texts, got %d: %s", n, raw)
	}
	if !strings.Contains(string(raw), `"tool_calls"`) || !strings.Contains(string(raw), `"tool_call_id":"c1"`) {
		t.Fatalf("other fields must survive: %s", raw)
	}
	plain, _ := json.Marshal(cacheMarked(msgs, "xiaomi/mimo-v2.6-pro"))
	if strings.Contains(string(plain), "cache_control") {
		t.Fatalf("other models get no marks: %s", plain)
	}
}

// A reply that is only thinking ran out of budget: the retry carries a note
// and a larger budget instead of repeating the request that failed.
func TestThinkingOnlyReplyIsRetriedWithMoreBudget(t *testing.T) {
	cfg := noToolsConfig(t)
	model := newQueueModel(
		scriptedReply{msg: &Msg{Role: "assistant", Reasoning: "hmm, let me think about everything..."}},
		scriptedReply{msg: &Msg{Role: "assistant", Content: "done"}},
	)
	cfg.Model = model
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Close()
	msg, err := a.chat(t.Context(), nil)
	if err != nil || msg.Content != "done" {
		t.Fatalf("chat = %+v, %v", msg, err)
	}
	reqs := model.reqs()
	if len(reqs) != 2 {
		t.Fatalf("%d requests, want 2", len(reqs))
	}
	last := reqs[1].Messages[len(reqs[1].Messages)-1]
	if !strings.Contains(last.Content, "cut off while you were still thinking") {
		t.Fatalf("retry carries no note: %+v", last)
	}
	if reqs[1].MaxTokens <= reqs[0].MaxTokens {
		t.Fatalf("retry budget %d not above %d", reqs[1].MaxTokens, reqs[0].MaxTokens)
	}
}
