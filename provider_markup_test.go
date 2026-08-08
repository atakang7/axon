package axon

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Provider tool-call markup leaking into content
//
// Observed against deepseek-v3.2 on OpenRouter's "Baidu Qianfan" route. The
// model's native tool-call syntax is a DSML block; the provider is supposed to
// parse it into the OpenAI tool_calls field and never show it to the client.
// On this route it instead streamed the block's CLOSING tags as ordinary
// content, with no tool_calls at all:
//
//	\n</｜DSML｜parameter>\n</｜DSML｜invoke>\n</｜DSML｜tool_calls>
//
// The turn loop ends a turn when an assistant message carries no tool calls,
// so the agent stopped mid-task and printed the markup as its answer.
//
// These tests reuse the SSE stub from pruner_integration_test.go — it is a
// plain OpenAI-compatible endpoint, not pruner-specific.
// ---------------------------------------------------------------------------

// dsmlCloserChunks is how the tokens actually arrived on the wire, split
// mid-token across SSE frames exactly as the trace recorded them.
var dsmlCloserChunks = []sseDelta{
	{Content: "\n"},
	{Content: "</｜DSML｜"},
	{Content: "parameter>\n</｜DSML｜inv"},
	{Content: "oke>\n</｜DSML｜tool_c"},
	{Content: "alls>"},
}

// agentAgainst builds an Agent whose model is a real Client pointed at the
// stub provider, with a session in a temp file.
func agentAgainst(t *testing.T, p *prunerProvider) *Agent {
	t.Helper()

	client, err := NewClient(ClientConfig{Provider: Provider{
		Name: "test", BaseURL: p.server.URL, Model: "deepseek-v3.2", APIKey: "k",
	}})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	cfg := baseConfig(t)
	cfg.Model = client
	cfg.ExcludeBuiltins = allBuiltins

	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { a.Close() })
	return a
}

// REPRODUCTION: the agent accepts the markup as a finished answer and ends the
// turn. Nothing errors, nothing retries, and the user is shown raw template
// tokens as if they were the assistant's reply.
func TestProviderToolMarkupLeakEndsTheTurnSilently(t *testing.T) {
	provider := newPrunerProvider(t, dsmlCloserChunks...)
	a := agentAgainst(t, provider)

	res, err := a.Step(context.Background(), "keep going")
	if err != nil {
		t.Fatalf("Step returned an error: %v", err)
	}

	if len(res.ToolCalls) != 0 {
		t.Fatalf("expected no tool calls, got %d", len(res.ToolCalls))
	}
	if !strings.Contains(res.Assistant, "DSML") {
		t.Fatalf("assistant text does not carry the markup: %q", res.Assistant)
	}

	t.Logf("REPRODUCED: turn ended after one call with the assistant text set to "+
		"%q — the agent stopped mid-task and showed the user template tokens",
		res.Assistant)

	if len(provider.requests) != 1 {
		t.Fatalf("provider saw %d calls; the runtime did not retry", len(provider.requests))
	}
}

// The leaked block is stored verbatim in the session, so it is replayed to the
// provider on every later turn. Whatever made the model emit an unbalanced
// block once is now permanently in its context.
func TestProviderToolMarkupLeakPersistsIntoHistory(t *testing.T) {
	provider := newPrunerProvider(t, dsmlCloserChunks...)
	a := agentAgainst(t, provider)

	if _, err := a.Step(context.Background(), "keep going"); err != nil {
		t.Fatalf("Step: %v", err)
	}

	var found bool
	for _, m := range a.Session().ContextMessages(0) {
		if m.Role == "assistant" && strings.Contains(m.Content, "DSML") {
			found = true
		}
	}
	if !found {
		t.Skip("markup is not replayed; the runtime already strips it")
	}
	t.Log("the markup is replayed to the provider on every subsequent turn")
}

// The same leak with tool_calls PRESENT is harmless: the loop runs the tool and
// keeps going, and the stray text is just noise in one message. This isolates
// what actually breaks — the empty tool_calls field, not the markup itself.
func TestProviderToolMarkupWithToolCallsStillContinues(t *testing.T) {
	calls := 0
	provider := newPrunerProviderFunc(t, func(call int) []sseDelta {
		calls++
		if call == 0 {
			// Markup leaked alongside a real tool call.
			return append(append([]sseDelta{}, dsmlCloserChunks...),
				sseDelta{ToolCallID: "call_1", ToolName: "probe", ToolArgs: "{}"})
		}
		return []sseDelta{{Content: "done"}}
	})

	a := agentAgainst(t, provider)
	var ran bool
	a.tools = append(a.tools, Tool{
		Name: "probe", Description: "test", Schema: okSchema,
		Fn: func(context.Context, json.RawMessage) (string, error) { ran = true; return "ok", nil },
	})

	res, err := a.Step(context.Background(), "keep going")
	if err != nil {
		t.Fatalf("Step: %v", err)
	}
	if !ran {
		t.Fatal("the tool was not executed despite tool_calls being present")
	}
	if res.Assistant != "done" {
		t.Fatalf("assistant = %q, want the second turn's answer", res.Assistant)
	}
	t.Log("with tool_calls present the leak is cosmetic — the loop continues")
}
