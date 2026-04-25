package main

// agent_live_integration_test.go holds opt-in networked tests against the real
// configured provider. These are intentionally low-cost and write UI/session
// traces to disk for human inspection.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func liveEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("AXON_LIVE_INTEGRATION"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func liveProviderFromHome(t *testing.T) Provider {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping live integration in -short mode")
	}
	if !liveEnabled() {
		t.Skip("set AXON_LIVE_INTEGRATION=1 to run live provider tests")
	}

	homeProviders := filepath.Join(userHomeDir(), ".config", "agent", "providers.json")
	if _, err := os.Stat(homeProviders); err != nil {
		t.Skipf("home provider config not available: %v", err)
	}

	// Use the real home configuration and clear per-test overrides so the
	// integration path reflects the user's configured provider as closely as possible.
	t.Setenv("AXON_PROVIDERS_PATH", homeProviders)
	t.Setenv("LLM_MODEL", "")
	t.Setenv("LLM_BASE_URL", "")
	t.Setenv("LLM_API_KEY", "")
	t.Setenv("LLM_PROVIDER_EXTRA", "")

	providerName := strings.TrimSpace(os.Getenv("AXON_LIVE_PROVIDER"))
	if providerName == "" {
		providerName = "openrouter"
	}
	t.Setenv("LLM_PROVIDER", providerName)
	t.Setenv("LLM_PROVIDER_NAME", "")

	providers, err := LoadProviders()
	if err != nil {
		t.Fatalf("LoadProviders: %v", err)
	}
	p, err := ResolveProvider(providers)
	if err != nil {
		t.Fatalf("ResolveProvider: %v", err)
	}
	if strings.TrimSpace(p.BaseURL) == "" || strings.TrimSpace(p.Model) == "" {
		t.Fatalf("live provider is incomplete: %+v", p)
	}
	return p
}

func liveArtifactDir(t *testing.T) string {
	t.Helper()
	base := strings.TrimSpace(os.Getenv("AXON_INTEGRATION_LOG_DIR"))
	if base == "" {
		base = filepath.Join("/home/zperson/axon/agent", "test_artifacts", "live_integration")
	}
	if err := os.MkdirAll(base, 0755); err != nil {
		t.Fatalf("mkdir live artifact dir: %v", err)
	}
	return base
}

func writeLiveTraceArtifacts(t *testing.T, provider Provider, s *Session, prompt, uiOut string, extra map[string]any) string {
	t.Helper()
	dir := liveArtifactDir(t)
	stamp := time.Now().Format("20060102-150405")
	base := filepath.Join(dir, fmt.Sprintf("%s-%s", stamp, sanitizeTestName(t.Name())))

	meta := map[string]any{
		"test_name":  t.Name(),
		"provider":   provider.Name,
		"base_url":   provider.BaseURL,
		"model":      provider.Model,
		"cwd":        s.Cwd,
		"prompt":     prompt,
		"session":    s.path,
		"written_at": time.Now().Format(time.RFC3339),
		"extra":      extra,
	}

	metaBytes, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatalf("marshal live meta: %v", err)
	}
	if err := os.WriteFile(base+".meta.json", metaBytes, 0644); err != nil {
		t.Fatalf("write live meta: %v", err)
	}
	if err := os.WriteFile(base+".ui.log", []byte(uiOut), 0644); err != nil {
		t.Fatalf("write ui trace: %v", err)
	}
	sessionBytes, err := os.ReadFile(s.path)
	if err == nil {
		if err := os.WriteFile(base+".session.json", sessionBytes, 0644); err != nil {
			t.Fatalf("write session trace: %v", err)
		}
	}
	return base
}

func TestHomeProviderConfigMatchesExpectedOpenRouterShape(t *testing.T) {
	homeProviders := filepath.Join(userHomeDir(), ".config", "agent", "providers.json")
	if _, err := os.Stat(homeProviders); err != nil {
		t.Skipf("home provider config not available: %v", err)
	}

	t.Setenv("AXON_PROVIDERS_PATH", homeProviders)
	t.Setenv("LLM_PROVIDER", "openrouter")
	t.Setenv("LLM_MODEL", "")
	t.Setenv("LLM_BASE_URL", "")
	t.Setenv("LLM_API_KEY", "")
	t.Setenv("LLM_PROVIDER_EXTRA", "")

	providers, err := LoadProviders()
	if err != nil {
		t.Fatal(err)
	}
	p, err := ResolveProvider(providers)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "openrouter" {
		t.Fatalf("expected openrouter provider, got %+v", p)
	}
	if !strings.Contains(p.BaseURL, "openrouter.ai") {
		t.Fatalf("expected openrouter base url, got %+v", p)
	}
	if strings.TrimSpace(p.Model) == "" {
		t.Fatalf("expected configured model, got %+v", p)
	}
}

func TestLiveOpenRouterChatSmokeLowCost(t *testing.T) {
	p := liveProviderFromHome(t)
	client, err := NewClient(p)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	msg, err := client.Chat(ctx, []Msg{
		{Role: "user", Content: "Reply with exactly: LIVE_OK"},
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if msg == nil || strings.TrimSpace(msg.Content) != "LIVE_OK" || len(msg.ToolCalls) != 0 {
		t.Fatalf("unexpected live smoke response: %+v", msg)
	}
}

func TestLiveAgentReadRoundTripWithTraceArtifacts(t *testing.T) {
	p := liveProviderFromHome(t)
	client, err := NewClient(p)
	if err != nil {
		t.Fatal(err)
	}

	s := tmpSession(t)
	initSessionMessages(s)
	mustWriteFile(t, filepath.Join(s.Cwd, "hello.txt"), "integration-ok\n")

	prompt := "Use the read tool exactly once on hello.txt. After you receive the tool result, answer with exactly:\nFILE_OK:integration-ok\nDo not call search, exec, write, park, forget, refresh, or recall."

	// Mirror the runtime path closely: decay first, then append the user block.
	s.DecayBlocks()
	s.Append(Msg{Role: "user", Content: prompt})
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	a := &Agent{client: client, tools: BuildTools(s), session: s}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	var first, second *Msg
	uiOut := captureStdout(t, func() {
		first, err = a.chat(ctx, a.tools)
		if err != nil {
			return
		}
		a.session.Append(*first)
		_ = a.session.Save()

		for _, tc := range first.ToolCalls {
			result := a.runTool(tc)
			a.session.Append(result)
			if id := a.session.Messages[len(a.session.Messages)-1].ID; id != "" {
				a.session.Messages[len(a.session.Messages)-1].Content = "[#" + id + "]\n" + result.Content
			}
			_ = a.session.Save()
		}

		second, err = a.chat(ctx, a.tools)
		if err != nil {
			return
		}
		a.session.Append(*second)
		_ = a.session.Save()
	})
	if err != nil {
		t.Fatal(err)
	}

	extra := map[string]any{
		"first_tool_calls": 0,
		"final_content":    "",
	}
	if first != nil {
		extra["first_tool_calls"] = len(first.ToolCalls)
	}
	if second != nil {
		extra["final_content"] = strings.TrimSpace(second.Content)
	}
	artifactBase := writeLiveTraceArtifacts(t, p, s, prompt, uiOut, extra)
	t.Logf("live trace artifacts: %s.{meta.json,ui.log,session.json}", artifactBase)

	if first == nil {
		t.Fatal("expected first assistant message")
	}
	if len(first.ToolCalls) != 1 || first.ToolCalls[0].Function.Name != toolRead {
		t.Fatalf("expected exactly one read tool call, got %+v", first.ToolCalls)
	}
	if second == nil {
		t.Fatal("expected second assistant message")
	}
	if len(second.ToolCalls) != 0 {
		t.Fatalf("expected no second-round tool calls, got %+v", second.ToolCalls)
	}
	if !strings.Contains(uiOut, "read") || !strings.Contains(uiOut, "integration-ok") {
		t.Fatalf("ui trace should include tool and final answer, got %q", uiOut)
	}
}
