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

// A leaked block must be retried, not accepted as an answer. The provider
// dropped a tool call the model asked for; one more attempt is far more likely
// to produce it than treating template tokens as the reply.
func TestProviderToolMarkupLeakIsRetried(t *testing.T) {
	provider := newPrunerProviderFunc(t, func(call int) []sseDelta {
		if call == 0 {
			return dsmlCloserChunks
		}
		return []sseDelta{{Content: "here is the real answer"}}
	})
	a := agentAgainst(t, provider)

	res, err := a.Step(context.Background(), "keep going")
	if err != nil {
		t.Fatalf("Step: %v", err)
	}

	if len(provider.requests) != 2 {
		t.Fatalf("provider saw %d calls, want 2 — the leak was not retried",
			len(provider.requests))
	}
	if res.Assistant != "here is the real answer" {
		t.Fatalf("assistant = %q, want the retry's answer", res.Assistant)
	}
	if strings.Contains(res.Assistant, "DSML") {
		t.Fatal("template tokens were shown to the user as the reply")
	}
}

// A provider that leaks on every attempt must fail the turn with a named
// cause. Stopping silently — the original symptom — is the one outcome that
// tells nobody anything.
func TestProviderToolMarkupLeakFailsLoudlyWhenPersistent(t *testing.T) {
	provider := newPrunerProvider(t, dsmlCloserChunks...)
	a := agentAgainst(t, provider)

	_, err := a.Step(context.Background(), "keep going")
	if err == nil {
		t.Fatal("a persistently leaking provider ended the turn as a success")
	}
	if !strings.Contains(err.Error(), "tool-call markup") {
		t.Fatalf("error does not name the cause: %v", err)
	}
	t.Logf("failed loudly after %d attempts: %v", len(provider.requests), err)
}

// Prose that merely mentions tool calls is a real answer and must survive —
// including the sentence this very changelog entry is written in.
func TestProviderToolMarkupDoesNotSwallowRealAnswers(t *testing.T) {
	for _, content := range []string{
		"The provider leaked a tool_calls block instead of parsing it.",
		"I could not emit a <tool_call> because the schema was rejected.",
		"Done. See the DSML notes in the trace for why the earlier turn stopped.",
	} {
		t.Run(content[:20], func(t *testing.T) {
			provider := newPrunerProvider(t, sseDelta{Content: content})
			a := agentAgainst(t, provider)

			res, err := a.Step(context.Background(), "explain")
			if err != nil {
				t.Fatalf("a real answer was rejected: %v", err)
			}
			if res.Assistant != content {
				t.Fatalf("assistant = %q, want %q", res.Assistant, content)
			}
			if len(provider.requests) != 1 {
				t.Fatalf("a real answer was retried %d times", len(provider.requests))
			}
		})
	}
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
