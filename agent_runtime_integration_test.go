package main

// agent_runtime_integration_test.go exercises the orchestrator/runtime layer:
// provider loading, session lifecycle, UI emission, chat streaming, retry
// logic, multi-round tool execution, and integration traces written to disk.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func runtimeArtifactDir(t *testing.T) string {
	t.Helper()
	base := strings.TrimSpace(os.Getenv("AXON_RUNTIME_TEST_LOG_DIR"))
	if base == "" {
		base = filepath.Join("/home/zperson/axon/agent", "test_artifacts", "runtime_integration")
	}
	if err := os.MkdirAll(base, 0755); err != nil {
		t.Fatalf("mkdir runtime artifact dir: %v", err)
	}
	return base
}

func writeRuntimeTraceArtifacts(t *testing.T, s *Session, uiOut string, extra map[string]any) string {
	t.Helper()
	dir := runtimeArtifactDir(t)
	stamp := time.Now().Format("20060102-150405")
	base := filepath.Join(dir, fmt.Sprintf("%s-%s", stamp, sanitizeTestName(t.Name())))

	meta := map[string]any{
		"test_name":  t.Name(),
		"cwd":        s.Cwd,
		"session":    s.path,
		"written_at": time.Now().Format(time.RFC3339),
		"extra":      extra,
	}
	metaBytes, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatalf("marshal runtime meta: %v", err)
	}
	if err := os.WriteFile(base+".meta.json", metaBytes, 0644); err != nil {
		t.Fatalf("write runtime meta: %v", err)
	}
	if err := os.WriteFile(base+".ui.log", []byte(uiOut), 0644); err != nil {
		t.Fatalf("write runtime ui trace: %v", err)
	}
	if sessionBytes, err := os.ReadFile(s.path); err == nil {
		if err := os.WriteFile(base+".session.json", sessionBytes, 0644); err != nil {
			t.Fatalf("write runtime session trace: %v", err)
		}
	}
	return base
}

func TestConfigHelpersComprehensive(t *testing.T) {
	t.Run("envString trims", func(t *testing.T) {
		t.Setenv("AXON_TEST_TRIM", "  spaced  ")
		if got := envString("AXON_TEST_TRIM"); got != "spaced" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("envInt fallback and minimum", func(t *testing.T) {
		t.Setenv("AXON_TEST_INT", "")
		if got := envInt("AXON_TEST_INT", 7, 1); got != 7 {
			t.Fatalf("expected fallback 7, got %d", got)
		}
		t.Setenv("AXON_TEST_INT", "abc")
		if got := envInt("AXON_TEST_INT", 7, 1); got != 7 {
			t.Fatalf("expected fallback on invalid, got %d", got)
		}
		t.Setenv("AXON_TEST_INT", "0")
		if got := envInt("AXON_TEST_INT", 7, 1); got != 7 {
			t.Fatalf("expected fallback below min, got %d", got)
		}
		t.Setenv("AXON_TEST_INT", "9")
		if got := envInt("AXON_TEST_INT", 7, 1); got != 9 {
			t.Fatalf("expected parsed value, got %d", got)
		}
	})

	t.Run("path helper precedence", func(t *testing.T) {
		t.Setenv("AXON_CONFIG_DIR", "/tmp/axon-config")
		if got := configDir(); got != "/tmp/axon-config" {
			t.Fatalf("configDir got %q", got)
		}

		t.Setenv("AXON_CONFIG_DIR", "")
		t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg-config")
		if got := configDir(); got != filepath.Join("/tmp/xdg-config", "agent") {
			t.Fatalf("configDir xdg got %q", got)
		}

		t.Setenv("AXON_DATA_DIR", "/tmp/axon-data")
		if got := dataDir(); got != "/tmp/axon-data" {
			t.Fatalf("dataDir got %q", got)
		}

		t.Setenv("AXON_DATA_DIR", "")
		t.Setenv("XDG_DATA_HOME", "/tmp/xdg-data")
		if got := dataDir(); got != filepath.Join("/tmp/xdg-data", "agent") {
			t.Fatalf("dataDir xdg got %q", got)
		}

		t.Setenv("AXON_PROVIDERS_PATH", "/tmp/providers.json")
		if got := providersPath(); got != "/tmp/providers.json" {
			t.Fatalf("providersPath got %q", got)
		}

		t.Setenv("AXON_SESSION_PATH", "/tmp/session.json")
		if got := sessionPath(); got != "/tmp/session.json" {
			t.Fatalf("sessionPath got %q", got)
		}
	})

	t.Run("path helper defaults", func(t *testing.T) {
		t.Setenv("AXON_CONFIG_DIR", "")
		t.Setenv("XDG_CONFIG_HOME", "")
		if got := configDir(); !strings.HasSuffix(got, filepath.Join(".config", "agent")) {
			t.Fatalf("unexpected default configDir %q", got)
		}

		t.Setenv("AXON_DATA_DIR", "")
		t.Setenv("XDG_DATA_HOME", "")
		if got := dataDir(); !strings.HasSuffix(got, filepath.Join(".local", "share", "agent")) {
			t.Fatalf("unexpected default dataDir %q", got)
		}

		t.Setenv("AXON_PROVIDERS_PATH", "")
		if got := providersPath(); !strings.HasSuffix(got, filepath.Join("agent", "providers.json")) {
			t.Fatalf("unexpected default providersPath %q", got)
		}

		t.Setenv("AXON_SESSION_PATH", "")
		if got := sessionPath(); !strings.HasSuffix(got, filepath.Join("agent", "session.json")) {
			t.Fatalf("unexpected default sessionPath %q", got)
		}
	})

	t.Run("limit helpers honor env", func(t *testing.T) {
		t.Setenv("AXON_READ_LIMIT", "17")
		t.Setenv("AXON_EXEC_TIMEOUT_SECONDS", "41")
		t.Setenv("AXON_EXEC_OUTPUT_LIMIT", "321")
		t.Setenv("AXON_SEARCH_LIMIT", "25")
		t.Setenv("AXON_SEARCH_OUTPUT_LIMIT", "654")
		t.Setenv("AXON_EXEC_TAIL_LINES", "13")
		t.Setenv("AXON_EXEC_MAX_TAIL_LINES", "222")

		if readLimit() != 17 || execTimeoutSeconds() != 41 || execOutputLimit() != 321 || searchLimit() != 25 || searchOutputLimit() != 654 || execDefaultTailLines() != 13 || execMaxTailLines() != 222 {
			t.Fatalf("env-based limits not applied correctly")
		}
	})

	t.Run("userHomeDir nonempty", func(t *testing.T) {
		if got := userHomeDir(); strings.TrimSpace(got) == "" {
			t.Fatal("userHomeDir should not be empty")
		}
	})
}

func TestProviderConfigComprehensive(t *testing.T) {
	t.Run("providerFromEnv absent", func(t *testing.T) {
		p, ok, err := providerFromEnv()
		if err != nil || ok || p.Name != "" || p.BaseURL != "" || p.Model != "" || p.APIKey != "" || len(p.Extra) != 0 {
			t.Fatalf("expected empty provider, got p=%+v ok=%v err=%v", p, ok, err)
		}
	})

	t.Run("providerFromEnv requires model and base url", func(t *testing.T) {
		t.Setenv("LLM_MODEL", "m")
		_, _, err := providerFromEnv()
		if err == nil || !strings.Contains(err.Error(), "LLM_MODEL and LLM_BASE_URL") {
			t.Fatalf("expected missing-base-url error, got %v", err)
		}
	})

	t.Run("providerFromEnv invalid extra json", func(t *testing.T) {
		t.Setenv("LLM_MODEL", "m")
		t.Setenv("LLM_BASE_URL", "http://x")
		t.Setenv("LLM_PROVIDER_EXTRA", "{")
		_, _, err := providerFromEnv()
		if err == nil || !strings.Contains(err.Error(), "valid JSON") {
			t.Fatalf("expected json validation error, got %v", err)
		}
	})

	t.Run("providerFromEnv valid named provider", func(t *testing.T) {
		t.Setenv("LLM_PROVIDER_NAME", "OpenRouter")
		t.Setenv("LLM_MODEL", "m")
		t.Setenv("LLM_BASE_URL", "http://x")
		t.Setenv("LLM_API_KEY", "k")
		t.Setenv("LLM_PROVIDER_EXTRA", `{"order":["deepseek"]}`)
		p, ok, err := providerFromEnv()
		if err != nil || !ok {
			t.Fatalf("expected env provider, got p=%+v ok=%v err=%v", p, ok, err)
		}
		if p.Name != "openrouter" || p.Model != "m" || p.BaseURL != "http://x" || p.APIKey != "k" || string(p.Extra) != `{"order":["deepseek"]}` {
			t.Fatalf("unexpected env provider: %+v", p)
		}
	})

	t.Run("applyProviderEnvOverrides", func(t *testing.T) {
		base := Provider{Name: "x", BaseURL: "http://old", Model: "old", APIKey: "oldkey"}
		t.Setenv("LLM_BASE_URL", "http://new")
		t.Setenv("LLM_MODEL", "new-model")
		t.Setenv("LLM_API_KEY", "new-key")
		t.Setenv("LLM_PROVIDER_EXTRA", `{"allow_fallbacks":true}`)
		got, err := applyProviderEnvOverrides(base)
		if err != nil {
			t.Fatal(err)
		}
		if got.BaseURL != "http://new" || got.Model != "new-model" || got.APIKey != "new-key" || string(got.Extra) != `{"allow_fallbacks":true}` {
			t.Fatalf("unexpected overrides: %+v", got)
		}

		t.Setenv("LLM_PROVIDER_EXTRA", "{")
		_, err = applyProviderEnvOverrides(base)
		if err == nil || !strings.Contains(err.Error(), "valid JSON") {
			t.Fatalf("expected invalid extra error, got %v", err)
		}
	})

	t.Run("providerNames sorted", func(t *testing.T) {
		got := providerNames(map[string]Provider{"b": {}, "a": {}, "c": {}})
		if !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
			t.Fatalf("unexpected names: %+v", got)
		}
	})
}

func TestLoadProvidersAndResolveProviderComprehensive(t *testing.T) {
	t.Run("LoadProviders missing file", func(t *testing.T) {
		t.Setenv("AXON_PROVIDERS_PATH", filepath.Join(t.TempDir(), "missing.json"))
		got, err := LoadProviders()
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 0 {
			t.Fatalf("expected empty provider map, got %+v", got)
		}
	})

	t.Run("LoadProviders invalid JSON", func(t *testing.T) {
		t.Setenv("AXON_PROVIDERS_PATH", writeProvidersFile(t, "{"))
		_, err := LoadProviders()
		if err == nil {
			t.Fatal("expected invalid json error")
		}
	})

	t.Run("LoadProviders read error on directory path", func(t *testing.T) {
		t.Setenv("AXON_PROVIDERS_PATH", t.TempDir())
		if _, err := LoadProviders(); err == nil {
			t.Fatal("expected read error for directory path")
		}
	})

	t.Run("LoadProviders missing required fields", func(t *testing.T) {
		t.Setenv("AXON_PROVIDERS_PATH", writeProvidersFile(t, `{"providers":[{"name":"x"}]}`))
		_, err := LoadProviders()
		if err == nil || !strings.Contains(err.Error(), "provider name and model required") {
			t.Fatalf("expected required-field error, got %v", err)
		}
	})

	t.Run("LoadProviders normalizes shorthand provider slug", func(t *testing.T) {
		t.Setenv("AXON_PROVIDERS_PATH", writeProvidersFile(t, `{"providers":[{"name":"OpenRouter","base_url":"http://example.test","model":"m","provider":"deepseek"}]}`))
		got, err := LoadProviders()
		if err != nil {
			t.Fatal(err)
		}
		p := got["openrouter"]
		if p.Name != "openrouter" || p.BaseURL != "http://example.test" || p.Model != "m" {
			t.Fatalf("unexpected loaded provider: %+v", p)
		}
		if !strings.Contains(string(p.Extra), `"order":["deepseek"]`) || !strings.Contains(string(p.Extra), `"allow_fallbacks":true`) {
			t.Fatalf("expected normalized provider extra, got %s", p.Extra)
		}
	})

	t.Run("ResolveProvider chooses named configured provider", func(t *testing.T) {
		t.Setenv("LLM_PROVIDER", "one")
		p, err := ResolveProvider(map[string]Provider{
			"one": {Name: "one", BaseURL: "http://one", Model: "m1"},
			"two": {Name: "two", BaseURL: "http://two", Model: "m2"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if p.Name != "one" {
			t.Fatalf("expected named provider, got %+v", p)
		}
	})

	t.Run("ResolveProvider applies overrides to named configured provider", func(t *testing.T) {
		t.Setenv("LLM_PROVIDER", "one")
		t.Setenv("LLM_MODEL", "override-model")
		t.Setenv("LLM_BASE_URL", "http://override")
		p, err := ResolveProvider(map[string]Provider{
			"one": {Name: "one", BaseURL: "http://one", Model: "m1"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if p.Name != "one" || p.Model != "override-model" || p.BaseURL != "http://override" {
			t.Fatalf("unexpected overridden named provider: %+v", p)
		}
	})

	t.Run("ResolveProvider named missing provider errors", func(t *testing.T) {
		t.Setenv("LLM_PROVIDER", "missing")
		t.Setenv("AXON_PROVIDERS_PATH", "/tmp/providers-missing.json")
		_, err := ResolveProvider(map[string]Provider{"one": {Name: "one"}})
		if err == nil || !strings.Contains(err.Error(), `provider "missing" not found`) {
			t.Fatalf("expected missing provider error, got %v", err)
		}
	})

	t.Run("ResolveProvider named path surfaces env-config error first", func(t *testing.T) {
		t.Setenv("LLM_PROVIDER", "missing")
		t.Setenv("LLM_MODEL", "m")
		t.Setenv("LLM_BASE_URL", "")
		_, err := ResolveProvider(map[string]Provider{"one": {Name: "one"}})
		if err == nil || !strings.Contains(err.Error(), "LLM_MODEL and LLM_BASE_URL are required") {
			t.Fatalf("expected env config error, got %v", err)
		}
	})

	t.Run("ResolveProvider falls back to env provider matching requested name", func(t *testing.T) {
		t.Setenv("LLM_PROVIDER", "envpick")
		t.Setenv("LLM_PROVIDER_NAME", "envpick")
		t.Setenv("LLM_MODEL", "m-env")
		t.Setenv("LLM_BASE_URL", "http://env")
		p, err := ResolveProvider(map[string]Provider{"other": {Name: "other", BaseURL: "http://other", Model: "m"}})
		if err != nil {
			t.Fatal(err)
		}
		if p.Name != "envpick" || p.Model != "m-env" || p.BaseURL != "http://env" {
			t.Fatalf("unexpected env-picked provider: %+v", p)
		}
	})

	t.Run("ResolveProvider chooses sole provider", func(t *testing.T) {
		p, err := ResolveProvider(map[string]Provider{"only": {Name: "only", BaseURL: "http://only", Model: "m"}})
		if err != nil {
			t.Fatal(err)
		}
		if p.Name != "only" {
			t.Fatalf("expected sole provider, got %+v", p)
		}
	})

	t.Run("ResolveProvider no provider configured", func(t *testing.T) {
		t.Setenv("AXON_PROVIDERS_PATH", "/tmp/no-provider.json")
		_, err := ResolveProvider(map[string]Provider{})
		if err == nil || !strings.Contains(err.Error(), "no provider configured") {
			t.Fatalf("expected no-provider error, got %v", err)
		}
	})

	t.Run("ResolveProvider uses env provider without selector", func(t *testing.T) {
		t.Setenv("LLM_PROVIDER", "")
		t.Setenv("LLM_PROVIDER_NAME", "env")
		t.Setenv("LLM_MODEL", "env-model")
		t.Setenv("LLM_BASE_URL", "http://env")
		p, err := ResolveProvider(map[string]Provider{
			"one": {Name: "one", BaseURL: "http://one", Model: "m1"},
			"two": {Name: "two", BaseURL: "http://two", Model: "m2"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if p.Name != "env" || p.Model != "env-model" {
			t.Fatalf("expected env provider, got %+v", p)
		}
	})

	t.Run("ResolveProvider multiple providers requires selector", func(t *testing.T) {
		_, err := ResolveProvider(map[string]Provider{
			"b": {Name: "b", BaseURL: "http://b", Model: "mb"},
			"a": {Name: "a", BaseURL: "http://a", Model: "ma"},
		})
		if err == nil || !strings.Contains(err.Error(), "multiple providers configured (a, b); set LLM_PROVIDER") {
			t.Fatalf("expected multiple-provider error, got %v", err)
		}
	})

	t.Run("ResolveProvider sole provider still applies invalid env overrides", func(t *testing.T) {
		t.Setenv("LLM_PROVIDER_EXTRA", "{")
		_, err := ResolveProvider(map[string]Provider{"only": {Name: "only", BaseURL: "http://only", Model: "m"}})
		if err == nil || !strings.Contains(err.Error(), "LLM_PROVIDER_EXTRA must be valid JSON") {
			t.Fatalf("expected invalid override error, got %v", err)
		}
	})
}

func TestSessionLifecycleComprehensive(t *testing.T) {
	t.Run("TaskBlock empty when nil", func(t *testing.T) {
		s := tmpSession(t)
		if got := s.TaskBlock(); got != "" {
			t.Fatalf("expected empty task block, got %q", got)
		}
	})

	t.Run("LoadOrCreateSession loads valid file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "session.json")
		s := &Session{ID: "loaded", path: path, Cwd: "/tmp/test"}
		s.ensure()
		s.Append(Msg{Role: "user", Content: "hello"})
		if err := s.Save(); err != nil {
			t.Fatal(err)
		}
		t.Setenv("AXON_SESSION_PATH", path)
		got := LoadOrCreateSession()
		if got.ID != "loaded" || len(got.Messages) != 1 {
			t.Fatalf("unexpected loaded session: %+v", got)
		}
	})

	t.Run("LoadOrCreateSession backs up corrupt file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "session.json")
		if err := os.WriteFile(path, []byte("{"), 0644); err != nil {
			t.Fatal(err)
		}
		t.Setenv("AXON_SESSION_PATH", path)
		got := LoadOrCreateSession()
		if got == nil || got.ID == "" {
			t.Fatalf("expected new session, got %+v", got)
		}
		matches, err := filepath.Glob(path + ".corrupt.*")
		if err != nil {
			t.Fatal(err)
		}
		if len(matches) != 1 {
			t.Fatalf("expected corrupt backup file, got %+v", matches)
		}
	})

	t.Run("LoadOrCreateSession tolerates unreadable path kind", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv("AXON_SESSION_PATH", dir)
		got := LoadOrCreateSession()
		if got == nil || got.ID == "" {
			t.Fatalf("expected fallback session on read error, got %+v", got)
		}
	})

	t.Run("Reset clears state", func(t *testing.T) {
		s := tmpSession(t)
		s.Append(Msg{Role: "user", Content: "x"})
		s.RecordEdit("f", "a", "b")
		s.CurrentTask = &Task{Objective: "obj"}
		t.Setenv("AXON_SESSION_PATH", filepath.Join(t.TempDir(), "reset-session.json"))
		s.Reset()
		if s.ID == "" || s.path != sessionPath() || len(s.Messages) != 0 || len(s.Edits) != 0 || s.CurrentTask != nil {
			t.Fatalf("unexpected reset state: %+v", s)
		}
	})

	t.Run("ensure assigns ids and preserves max", func(t *testing.T) {
		s := &Session{path: filepath.Join(t.TempDir(), "session.json"), Messages: []Msg{
			{Role: "user", Content: "a", ID: "m7"},
			{Role: "assistant", Content: "b"},
			{Role: "system", Content: "sys"},
		}}
		s.ensure()
		if s.Messages[1].ID != "m8" || s.NextBlockID != 8 {
			t.Fatalf("unexpected ensure id assignment: %+v next=%d", s.Messages, s.NextBlockID)
		}
	})

	t.Run("ResolvePath and SetCwd", func(t *testing.T) {
		s := tmpSession(t)
		abs := filepath.Join(s.Cwd, "x")
		if got := s.ResolvePath(abs); got != abs {
			t.Fatalf("absolute path should pass through, got %q", got)
		}
		if got := s.ResolvePath("sub"); got != filepath.Join(s.Cwd, "sub") {
			t.Fatalf("relative path should resolve under cwd, got %q", got)
		}

		s.Cwd = ""
		if got := s.ResolvePath("sub2"); !strings.HasSuffix(got, "sub2") {
			t.Fatalf("ResolvePath without cwd should still produce path, got %q", got)
		}
		s.Cwd = filepath.Dir(abs)

		sub := filepath.Join(s.Cwd, "subdir")
		if err := os.MkdirAll(sub, 0755); err != nil {
			t.Fatal(err)
		}
		if err := s.SetCwd("subdir"); err != nil {
			t.Fatal(err)
		}
		if s.Cwd != sub {
			t.Fatalf("expected cwd %q, got %q", sub, s.Cwd)
		}

		if err := s.SetCwd(filepath.Join(s.Cwd, "missing")); err == nil {
			t.Fatal("expected missing-dir error")
		}
		filePath := filepath.Join(s.Cwd, "file.txt")
		mustWriteFile(t, filePath, "x")
		if err := s.SetCwd(filePath); err == nil || !strings.Contains(err.Error(), "not a directory") {
			t.Fatalf("expected not-a-directory error, got %v", err)
		}
	})

	t.Run("Undo empty", func(t *testing.T) {
		s := tmpSession(t)
		if _, ok := s.Undo(); ok {
			t.Fatal("expected no undo entry")
		}
	})

	t.Run("Save error when path is directory", func(t *testing.T) {
		s := tmpSession(t)
		s.path = t.TempDir()
		if err := s.Save(); err == nil {
			t.Fatal("expected save error when path is a directory")
		}
	})

	t.Run("Save error when parent path is blocked by file", func(t *testing.T) {
		s := tmpSession(t)
		blocker := filepath.Join(t.TempDir(), "blocker")
		mustWriteFile(t, blocker, "x")
		s.path = filepath.Join(blocker, "child", "session.json")
		if err := s.Save(); err == nil {
			t.Fatal("expected mkdirall failure when parent is file")
		}
	})
}

func TestUIFunctionsComprehensive(t *testing.T) {
	s := tmpSession(t)
	s.StartedAt = time.Date(2024, 1, 2, 15, 4, 0, 0, time.UTC)
	s.Turn = 3
	s.Edits = []Edit{{Path: "a"}, {Path: "b"}}

	out := captureStdout(t, func() {
		uiHeader("openrouter", "model-x", s)
		uiPrompt()
		uiAfterInput()
		uiStartResponse()
		uiToken("hello")
		uiResponse()
		uiTool("read", []byte(`{"path":"x.go"}`))
		uiToolResult("1\n2\n3\n4\n5\n6\n7\n8\n9\n10")
		uiToolError(errors.New("tool boom"))
		uiError(errors.New("bad"))
		uiInfo("info")
		uiUndone("file.go")
		uiMemory("memory")
		uiSessionNew()
		uiSessionInfo(s)
		stop := uiSpinner()
		time.Sleep(100 * time.Millisecond)
		stop()
	})

	want := []string{
		"openrouter · model-x",
		"3 turns",
		"❯",
		"hello",
		"read",
		"10",
		"tool boom",
		"bad",
		"info",
		"file.go",
		"memory",
		"new session",
		"Jan 2 15:04",
	}
	for _, part := range want {
		if !strings.Contains(out, part) {
			t.Fatalf("missing %q in ui output: %q", part, out)
		}
	}
	if strings.Contains(out, "1\n") || strings.Contains(out, "2\n") {
		t.Fatalf("uiToolResult should trim to last 8 lines, got %q", out)
	}
}

func TestAgentRuntimeComprehensive(t *testing.T) {
	t.Run("buildSystemPrompt includes tools", func(t *testing.T) {
		p := buildSystemPrompt()
		for _, part := range []string{toolRead, toolWrite, toolExec, toolSearch, toolTask, toolPark, toolForget, toolRefresh, toolRecall} {
			if !strings.Contains(p, part) {
				t.Fatalf("missing %q in system prompt", part)
			}
		}
	})

	t.Run("runTool success error and not found", func(t *testing.T) {
		s := tmpSession(t)
		mustWriteFile(t, filepath.Join(s.Cwd, "x.txt"), "hi")
		a := &Agent{tools: BuildTools(s), session: s}

		msg := a.runTool(ToolCall{ID: "1", Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Name: toolRead, Arguments: `{"path":"x.txt","mode":"full","reason":"read file"}`}})
		if msg.Role != "tool" || msg.ToolName != toolRead || !strings.Contains(msg.Content, "1\thi") {
			t.Fatalf("unexpected runTool success msg: %+v", msg)
		}

		msg = a.runTool(ToolCall{ID: "2", Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Name: toolRead, Arguments: `{"path":"x.txt","mode":"full"}`}})
		if !strings.Contains(msg.Content, "reason is required") {
			t.Fatalf("expected tool error content, got %+v", msg)
		}

		msg = a.runTool(ToolCall{ID: "3", Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Name: "missing_tool", Arguments: `{}`}})
		if msg.Content != "tool not found" {
			t.Fatalf("unexpected missing-tool msg: %+v", msg)
		}
	})

	t.Run("handleSlash branches", func(t *testing.T) {
		s := tmpSession(t)
		a := &Agent{session: s, tools: BuildTools(s)}

		out := captureStdout(t, func() {
			if !a.handleSlash("/pwd") {
				t.Fatal("/pwd should be handled")
			}
		})
		if !strings.Contains(out, s.Cwd) {
			t.Fatalf("/pwd output missing cwd: %q", out)
		}

		sub := filepath.Join(s.Cwd, "sub")
		if err := os.MkdirAll(sub, 0755); err != nil {
			t.Fatal(err)
		}
		if !a.handleSlash("/cd sub") || s.Cwd != sub {
			t.Fatalf("/cd should update cwd, got %q", s.Cwd)
		}

		if !a.handleSlash("/new") || len(a.session.Messages) != 1 || a.session.Messages[0].Role != "system" {
			t.Fatalf("/new should reset session, got %+v", a.session.Messages)
		}

		out = captureStdout(t, func() {
			if !a.handleSlash("/undo") {
				t.Fatal("/undo should be handled even with no edits")
			}
		})
		if !strings.Contains(out, "nothing to undo") {
			t.Fatalf("expected no-undo message, got %q", out)
		}

		a.session.RecordEdit(filepath.Join(a.session.Cwd, "undo.txt"), "before", "after")
		mustWriteFile(t, filepath.Join(a.session.Cwd, "undo.txt"), "after")
		if !a.handleSlash("/undo") {
			t.Fatal("/undo should be handled")
		}
		data, _ := os.ReadFile(filepath.Join(a.session.Cwd, "undo.txt"))
		if string(data) != "before" {
			t.Fatalf("/undo should restore file, got %q", data)
		}

		if !a.handleSlash("/session") {
			t.Fatal("/session should be handled")
		}

		out = captureStdout(t, func() {
			if !a.handleSlash("/cd missing-dir") {
				t.Fatal("/cd should be handled on error too")
			}
		})
		if !strings.Contains(out, "✗") {
			t.Fatalf("expected /cd error output, got %q", out)
		}

		if a.handleSlash("/unknown") {
			t.Fatal("unknown slash command should not be handled")
		}
	})

	t.Run("chat returns streamed content", func(t *testing.T) {
		srv, _ := newSSEServer(t, func(_ int, w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n"))
			_, _ = w.Write([]byte("data: [DONE]\n"))
		})
		defer srv.Close()

		client, err := NewClient(Provider{Name: "test", BaseURL: srv.URL, Model: "m"})
		if err != nil {
			t.Fatal(err)
		}
		a := &Agent{client: client, session: tmpSession(t)}
		msg, err := a.chat(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if msg.Content != "hello" || len(msg.ToolCalls) != 0 {
			t.Fatalf("unexpected chat msg: %+v", msg)
		}
	})

	t.Run("chat sends provider extra api key and defaults empty tool args", func(t *testing.T) {
		var gotAuth string
		var gotBody string
		srv, _ := newSSEServer(t, func(_ int, w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			gotAuth = r.Header.Get("Authorization")
			gotBody = string(body)
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"t1\",\"type\":\"function\",\"function\":{\"name\":\"read\",\"arguments\":\"\"}}]}}]}\n"))
			_, _ = w.Write([]byte("data: [DONE]\n"))
		})
		defer srv.Close()

		client, err := NewClient(Provider{
			Name:    "test",
			BaseURL: srv.URL,
			Model:   "m",
			APIKey:  "secret",
			Extra:   json.RawMessage(`{"order":["x"]}`),
		})
		if err != nil {
			t.Fatal(err)
		}
		msg, err := client.Chat(context.Background(), []Msg{{Role: "user", Content: "hi"}}, []Tool{{Name: "read", Description: "d", Schema: obj("object", nil, nil)}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if gotAuth != "Bearer secret" || !strings.Contains(gotBody, `"provider":{"order":["x"]}`) || !strings.Contains(gotBody, `"tools":[`) {
			t.Fatalf("unexpected request auth/body auth=%q body=%q", gotAuth, gotBody)
		}
		if len(msg.ToolCalls) != 1 || msg.ToolCalls[0].Function.Arguments != "{}" {
			t.Fatalf("expected empty tool args to normalize to {}, got %+v", msg)
		}
	})

	t.Run("chat returns non-retryable api error", func(t *testing.T) {
		srv, _ := newSSEServer(t, func(_ int, w http.ResponseWriter, r *http.Request) {
			http.Error(w, "bad request", http.StatusBadRequest)
		})
		defer srv.Close()

		client, err := NewClient(Provider{Name: "test", BaseURL: srv.URL, Model: "m"})
		if err != nil {
			t.Fatal(err)
		}
		a := &Agent{client: client, session: tmpSession(t)}
		_, err = a.chat(context.Background(), nil)
		if err == nil || !strings.Contains(err.Error(), "API error 400") {
			t.Fatalf("expected api error, got %v", err)
		}
	})

	t.Run("chat retries empty response once then fails", func(t *testing.T) {
		srv, calls := newSSEServer(t, func(_ int, w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"thinking\"}}]}\n"))
			_, _ = w.Write([]byte("data: [DONE]\n"))
		})
		defer srv.Close()

		client, err := NewClient(Provider{Name: "test", BaseURL: srv.URL, Model: "m"})
		if err != nil {
			t.Fatal(err)
		}
		a := &Agent{client: client, session: tmpSession(t)}
		start := time.Now()
		_, err = a.chat(context.Background(), nil)
		if err == nil || !strings.Contains(err.Error(), "empty response from model") {
			t.Fatalf("expected empty-response failure, got %v", err)
		}
		if atomic.LoadInt32(calls) != 2 {
			t.Fatalf("expected 2 chat attempts, got %d", atomic.LoadInt32(calls))
		}
		if time.Since(start) < 2*time.Second {
			t.Fatalf("expected retry backoff to elapse")
		}
	})

	t.Run("Run one-shot no tools", func(t *testing.T) {
		srv, _ := newSSEServer(t, func(_ int, w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"done\"}}]}\n"))
			_, _ = w.Write([]byte("data: [DONE]\n"))
		})
		defer srv.Close()

		client, err := NewClient(Provider{Name: "test", BaseURL: srv.URL, Model: "m"})
		if err != nil {
			t.Fatal(err)
		}
		s := tmpSession(t)
		inputs := []string{"inspect this"}
		i := 0
		a := &Agent{
			client:  client,
			tools:   BuildTools(s),
			session: s,
			input: func() (string, bool) {
				if i >= len(inputs) {
					return "", false
				}
				v := inputs[i]
				i++
				return v, true
			},
		}
		if err := a.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
		if len(s.Messages) < 3 {
			t.Fatalf("expected system+user+assistant messages, got %+v", s.Messages)
		}
		if s.Messages[1].Role != "user" || s.Messages[2].Role != "assistant" {
			t.Fatalf("unexpected message order: %+v", s.Messages)
		}
	})

	t.Run("Run handles slash command then eof without chatting", func(t *testing.T) {
		s := tmpSession(t)
		inputs := []string{"/pwd"}
		i := 0
		a := &Agent{
			tools:   BuildTools(s),
			session: s,
			input: func() (string, bool) {
				if i >= len(inputs) {
					return "", false
				}
				v := inputs[i]
				i++
				return v, true
			},
		}
		if err := a.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
		if len(s.Messages) != 1 || s.Messages[0].Role != "system" {
			t.Fatalf("slash-only run should only keep the system prompt, got %+v", s.Messages)
		}
	})

	t.Run("Run tolerates chat error then exits on eof", func(t *testing.T) {
		srv, _ := newSSEServer(t, func(_ int, w http.ResponseWriter, r *http.Request) {
			http.Error(w, "bad request", http.StatusBadRequest)
		})
		defer srv.Close()

		client, err := NewClient(Provider{Name: "test", BaseURL: srv.URL, Model: "m"})
		if err != nil {
			t.Fatal(err)
		}
		s := tmpSession(t)
		inputs := []string{"do something"}
		i := 0
		a := &Agent{
			client:  client,
			tools:   BuildTools(s),
			session: s,
			input: func() (string, bool) {
				if i >= len(inputs) {
					return "", false
				}
				v := inputs[i]
				i++
				return v, true
			},
		}
		if err := a.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
		if len(s.Messages) != 2 || s.Messages[1].Role != "user" {
			t.Fatalf("run should keep user message after chat error, got %+v", s.Messages)
		}
	})

	t.Run("Run processes ending memory tool call", func(t *testing.T) {
		srv, _ := newSSEServer(t, func(_ int, w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"done\",\"tool_calls\":[{\"index\":0,\"id\":\"t1\",\"type\":\"function\",\"function\":{\"name\":\"forget\",\"arguments\":\"{\\\"ids\\\":[\\\"m1\\\"],\\\"reason\\\":\\\"clean up\\\",\\\"next_step\\\":\\\"end\\\"}\"}}]}}]}\n"))
			_, _ = w.Write([]byte("data: [DONE]\n"))
		})
		defer srv.Close()

		client, err := NewClient(Provider{Name: "test", BaseURL: srv.URL, Model: "m"})
		if err != nil {
			t.Fatal(err)
		}
		s := tmpSession(t)
		inputs := []string{"forget the prior block"}
		i := 0
		a := &Agent{
			client:  client,
			tools:   BuildTools(s),
			session: s,
			input: func() (string, bool) {
				if i >= len(inputs) {
					return "", false
				}
				v := inputs[i]
				i++
				return v, true
			},
		}
		if err := a.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
		if len(s.Messages) < 4 {
			t.Fatalf("expected user+assistant+tool messages, got %+v", s.Messages)
		}
		if !s.Messages[1].Forgotten {
			t.Fatalf("expected first user block to be forgotten, got %+v", s.Messages[1])
		}
	})

	t.Run("Run stops processing later tool calls after end signal", func(t *testing.T) {
		srv, _ := newSSEServer(t, func(_ int, w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"done\",\"tool_calls\":[{\"index\":0,\"id\":\"t1\",\"type\":\"function\",\"function\":{\"name\":\"forget\",\"arguments\":\"{\\\"ids\\\":[\\\"m1\\\"],\\\"reason\\\":\\\"clean up\\\",\\\"next_step\\\":\\\"end\\\"}\"}},{\"index\":1,\"id\":\"t2\",\"type\":\"function\",\"function\":{\"name\":\"read\",\"arguments\":\"{\\\"path\\\":\\\"missing.txt\\\",\\\"mode\\\":\\\"full\\\",\\\"reason\\\":\\\"should never run\\\"}\"}}]}}]}\n"))
			_, _ = w.Write([]byte("data: [DONE]\n"))
		})
		defer srv.Close()

		client, err := NewClient(Provider{Name: "test", BaseURL: srv.URL, Model: "m"})
		if err != nil {
			t.Fatal(err)
		}
		s := tmpSession(t)
		inputs := []string{"forget then stop"}
		i := 0
		a := &Agent{
			client:  client,
			tools:   BuildTools(s),
			session: s,
			input: func() (string, bool) {
				if i >= len(inputs) {
					return "", false
				}
				v := inputs[i]
				i++
				return v, true
			},
		}
		if err := a.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
		var toolNames []string
		for _, m := range s.Messages {
			if m.Role == "tool" {
				toolNames = append(toolNames, m.ToolName)
			}
		}
		if !reflect.DeepEqual(toolNames, []string{toolForget}) {
			t.Fatalf("expected only forget tool to run, got %+v", toolNames)
		}
	})

	t.Run("Run auto-continues after task registration round", func(t *testing.T) {
		srv, calls := newSSEServer(t, func(call int, w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			if call == 1 {
				_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"t1\",\"type\":\"function\",\"function\":{\"name\":\"task\",\"arguments\":\"{\\\"objective\\\":\\\"inspect files\\\",\\\"definition_of_done\\\":\\\"facts gathered\\\",\\\"current_focus\\\":\\\"round one\\\",\\\"reason\\\":\\\"register task\\\"}\"}},{\"index\":1,\"id\":\"t2\",\"type\":\"function\",\"function\":{\"name\":\"read\",\"arguments\":\"{\\\"path\\\":\\\"a.txt\\\",\\\"mode\\\":\\\"full\\\",\\\"reason\\\":\\\"read a\\\"}\"}}]}}]}\n"))
			} else {
				_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"synthesized\"}}]}\n"))
			}
			_, _ = w.Write([]byte("data: [DONE]\n"))
		})
		defer srv.Close()

		client, err := NewClient(Provider{Name: "test", BaseURL: srv.URL, Model: "m"})
		if err != nil {
			t.Fatal(err)
		}
		s := tmpSession(t)
		mustWriteFile(t, filepath.Join(s.Cwd, "a.txt"), "A")
		mustWriteFile(t, filepath.Join(s.Cwd, "b.txt"), "B")
		inputs := []string{"inspect both files"}
		i := 0
		a := &Agent{
			client:  client,
			tools:   BuildTools(s),
			session: s,
			input: func() (string, bool) {
				if i >= len(inputs) {
					return "", false
				}
				v := inputs[i]
				i++
				return v, true
			},
		}
		if err := a.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
		if atomic.LoadInt32(calls) != 2 {
			t.Fatalf("expected two chat calls, got %d", atomic.LoadInt32(calls))
		}
		var toolNames []string
		var finalAssistant string
		for _, m := range s.Messages {
			if m.Role == "tool" {
				toolNames = append(toolNames, m.ToolName)
			}
			if m.Role == "assistant" && m.Content == "synthesized" {
				finalAssistant = m.Content
			}
		}
		if !reflect.DeepEqual(toolNames, []string{toolTask, toolRead}) || s.CurrentTask == nil || finalAssistant != "synthesized" {
			t.Fatalf("expected task registration, read result, and final synthesis, got messages %+v", s.Messages)
		}
	})
}

func TestRetryableBranchesComprehensive(t *testing.T) {
	if !retryable(io.ErrUnexpectedEOF) {
		t.Fatal("io.ErrUnexpectedEOF should be retryable")
	}
	if retryable(context.DeadlineExceeded) {
		t.Fatal("context.DeadlineExceeded should not be retryable")
	}
	if !retryable(timeoutErr{}) {
		t.Fatal("timeout net.Error should be retryable")
	}
	if !retryable(errors.New("API error 503 temporary")) {
		t.Fatal("503 string should be retryable")
	}
	if !retryable(errors.New("lookup failed: no such host")) {
		t.Fatal("no such host should be retryable")
	}
}

func TestAgentIntegrationEdgeCases(t *testing.T) {
	t.Run("tool error is fed back into the next model round", func(t *testing.T) {
		var bodies []string
		srv, calls := newSSEServer(t, func(call int, w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			bodies = append(bodies, string(body))
			w.Header().Set("Content-Type", "text/event-stream")
			if call == 1 {
				_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"t1\",\"type\":\"function\",\"function\":{\"name\":\"read\",\"arguments\":\"{\\\"path\\\":\\\"missing.txt\\\",\\\"mode\\\":\\\"full\\\",\\\"reason\\\":\\\"force read failure\\\"}\"}}]}}]}\n"))
			} else {
				_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"RECOVERED_FROM_TOOL_ERROR\"}}]}\n"))
			}
			_, _ = w.Write([]byte("data: [DONE]\n"))
		})
		defer srv.Close()

		client, err := NewClient(Provider{Name: "test", BaseURL: srv.URL, Model: "m"})
		if err != nil {
			t.Fatal(err)
		}
		s := tmpSession(t)
		inputs := []string{"try the missing file and recover"}
		i := 0
		a := &Agent{
			client:  client,
			tools:   BuildTools(s),
			session: s,
			input: func() (string, bool) {
				if i >= len(inputs) {
					return "", false
				}
				v := inputs[i]
				i++
				return v, true
			},
		}
		uiOut := captureStdout(t, func() {
			if err := a.Run(context.Background()); err != nil {
				t.Fatal(err)
			}
		})
		artifactBase := writeRuntimeTraceArtifacts(t, s, uiOut, map[string]any{
			"chat_calls":   atomic.LoadInt32(calls),
			"request_body": bodies,
		})
		t.Logf("runtime trace artifacts: %s.{meta.json,ui.log,session.json}", artifactBase)

		if atomic.LoadInt32(calls) != 2 {
			t.Fatalf("expected 2 chat calls, got %d", atomic.LoadInt32(calls))
		}
		if len(bodies) != 2 || !strings.Contains(bodies[1], "no such file or directory") {
			t.Fatalf("expected second request body to contain tool error context, got %+v", bodies)
		}
		foundToolErr := false
		foundFinal := false
		for _, m := range s.Messages {
			if m.Role == "tool" && strings.Contains(m.Content, "no such file or directory") {
				foundToolErr = true
			}
			if m.Role == "assistant" && m.Content == "RECOVERED_FROM_TOOL_ERROR" {
				foundFinal = true
			}
		}
		if !foundToolErr || !foundFinal {
			t.Fatalf("expected tool error and final recovery message, got %+v", s.Messages)
		}
	})

	t.Run("unknown tool is surfaced and the model can recover next round", func(t *testing.T) {
		var bodies []string
		srv, calls := newSSEServer(t, func(call int, w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			bodies = append(bodies, string(body))
			w.Header().Set("Content-Type", "text/event-stream")
			if call == 1 {
				_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"t1\",\"type\":\"function\",\"function\":{\"name\":\"ghost_tool\",\"arguments\":\"{}\"}}]}}]}\n"))
			} else {
				_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"RECOVERED_UNKNOWN_TOOL\"}}]}\n"))
			}
			_, _ = w.Write([]byte("data: [DONE]\n"))
		})
		defer srv.Close()

		client, err := NewClient(Provider{Name: "test", BaseURL: srv.URL, Model: "m"})
		if err != nil {
			t.Fatal(err)
		}
		s := tmpSession(t)
		inputs := []string{"call an unknown tool then recover"}
		i := 0
		a := &Agent{
			client:  client,
			tools:   BuildTools(s),
			session: s,
			input: func() (string, bool) {
				if i >= len(inputs) {
					return "", false
				}
				v := inputs[i]
				i++
				return v, true
			},
		}
		uiOut := captureStdout(t, func() {
			if err := a.Run(context.Background()); err != nil {
				t.Fatal(err)
			}
		})
		artifactBase := writeRuntimeTraceArtifacts(t, s, uiOut, map[string]any{
			"chat_calls":   atomic.LoadInt32(calls),
			"request_body": bodies,
		})
		t.Logf("runtime trace artifacts: %s.{meta.json,ui.log,session.json}", artifactBase)

		if atomic.LoadInt32(calls) != 2 {
			t.Fatalf("expected 2 chat calls, got %d", atomic.LoadInt32(calls))
		}
		if len(bodies) != 2 || !strings.Contains(bodies[1], "tool not found") {
			t.Fatalf("expected second request body to contain tool-not-found context, got %+v", bodies)
		}
		foundToolNotFound := false
		for _, m := range s.Messages {
			if m.Role == "tool" && strings.Contains(m.Content, "tool not found") {
				foundToolNotFound = true
			}
		}
		if !foundToolNotFound || !strings.Contains(uiOut, "RECOVERED_UNKNOWN_TOOL") {
			t.Fatalf("expected session/UI trace to show unknown-tool recovery, got messages=%+v ui=%q", s.Messages, uiOut)
		}
	})

	t.Run("mixed multi-tool batch preserves order and carries all results forward", func(t *testing.T) {
		var bodies []string
		srv, calls := newSSEServer(t, func(call int, w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			bodies = append(bodies, string(body))
			w.Header().Set("Content-Type", "text/event-stream")
			if call == 1 {
				_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"t1\",\"type\":\"function\",\"function\":{\"name\":\"read\",\"arguments\":\"{\\\"path\\\":\\\"a.txt\\\",\\\"mode\\\":\\\"full\\\",\\\"reason\\\":\\\"read a\\\"}\"}},{\"index\":1,\"id\":\"t2\",\"type\":\"function\",\"function\":{\"name\":\"read\",\"arguments\":\"{\\\"path\\\":\\\"missing.txt\\\",\\\"mode\\\":\\\"full\\\",\\\"reason\\\":\\\"read missing\\\"}\"}},{\"index\":2,\"id\":\"t3\",\"type\":\"function\",\"function\":{\"name\":\"read\",\"arguments\":\"{\\\"path\\\":\\\"b.txt\\\",\\\"mode\\\":\\\"full\\\",\\\"reason\\\":\\\"read b\\\"}\"}}]}}]}\n"))
			} else {
				_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"BATCH_RECOVERED\"}}]}\n"))
			}
			_, _ = w.Write([]byte("data: [DONE]\n"))
		})
		defer srv.Close()

		client, err := NewClient(Provider{Name: "test", BaseURL: srv.URL, Model: "m"})
		if err != nil {
			t.Fatal(err)
		}
		s := tmpSession(t)
		mustWriteFile(t, filepath.Join(s.Cwd, "a.txt"), "A")
		mustWriteFile(t, filepath.Join(s.Cwd, "b.txt"), "B")
		inputs := []string{"read both and handle the missing middle one"}
		i := 0
		a := &Agent{
			client:  client,
			tools:   BuildTools(s),
			session: s,
			input: func() (string, bool) {
				if i >= len(inputs) {
					return "", false
				}
				v := inputs[i]
				i++
				return v, true
			},
		}
		uiOut := captureStdout(t, func() {
			if err := a.Run(context.Background()); err != nil {
				t.Fatal(err)
			}
		})
		artifactBase := writeRuntimeTraceArtifacts(t, s, uiOut, map[string]any{
			"chat_calls":   atomic.LoadInt32(calls),
			"request_body": bodies,
		})
		t.Logf("runtime trace artifacts: %s.{meta.json,ui.log,session.json}", artifactBase)

		if atomic.LoadInt32(calls) != 2 {
			t.Fatalf("expected 2 chat calls, got %d", atomic.LoadInt32(calls))
		}
		var toolContents []string
		var finalAssistant bool
		for _, m := range s.Messages {
			if m.Role == "tool" {
				toolContents = append(toolContents, m.Content)
			}
			if m.Role == "assistant" && m.Content == "BATCH_RECOVERED" {
				finalAssistant = true
			}
		}
		if len(toolContents) != 3 {
			t.Fatalf("expected 3 tool messages, got %+v", toolContents)
		}
		if !strings.Contains(toolContents[0], "1\tA") ||
			!strings.Contains(toolContents[1], "no such file or directory") ||
			!strings.Contains(toolContents[2], "1\tB") {
			t.Fatalf("unexpected tool content order: %+v", toolContents)
		}
		if len(bodies) != 2 || !strings.Contains(bodies[1], "1\\tA") || !strings.Contains(bodies[1], "no such file or directory") || !strings.Contains(bodies[1], "1\\tB") {
			t.Fatalf("expected second request body to include all tool results, got %+v", bodies)
		}
		if !finalAssistant {
			t.Fatalf("expected final assistant synthesis, got %+v", s.Messages)
		}
	})
}

func TestVerifyAndMemoryStateHelpersComprehensive(t *testing.T) {
	t.Run("detectVerifyCommand markers", func(t *testing.T) {
		cases := []struct {
			file string
			cmd  string
		}{
			{"go.mod", "go build ./..."},
			{"Cargo.toml", "cargo check"},
			{"tsconfig.json", "tsc --noEmit"},
			{"package.json", "npm run build --if-present"},
			{"Makefile", "make"},
			{"pyproject.toml", "python -m py_compile $(find . -name '*.py' | head -20)"},
		}
		for _, tc := range cases {
			t.Run(tc.file, func(t *testing.T) {
				dir := t.TempDir()
				mustWriteFile(t, filepath.Join(dir, tc.file), "x")
				got, err := detectVerifyCommand(dir)
				if err != nil {
					t.Fatal(err)
				}
				if got != tc.cmd {
					t.Fatalf("got %q want %q", got, tc.cmd)
				}
			})
		}
	})

	t.Run("exec verify mode auto-detects command", func(t *testing.T) {
		s := tmpSession(t)
		project := filepath.Join(s.Cwd, "verify-go")
		if err := os.MkdirAll(project, 0755); err != nil {
			t.Fatal(err)
		}
		mustWriteFile(t, filepath.Join(project, "go.mod"), "module example.com/test\n\ngo 1.26.2\n")
		out, err := ExecTool(s).Fn([]byte(`{"mode":"verify","dir":"verify-go","reason":"check build"}`))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "$ go build ./...") || !strings.Contains(out, "expected: no errors") {
			t.Fatalf("unexpected verify output: %q", out)
		}
	})

	t.Run("ContextMessages includes task and fallback breadcrumb", func(t *testing.T) {
		s := tmpSession(t)
		s.CurrentTask = &Task{Objective: "obj", DefinitionOfDone: "done", CurrentFocus: "focus"}
		s.Append(Msg{Role: "user", Content: "raw", Parked: true, ParkSummary: "gist", ParkReason: "because"})
		ctx := s.ContextMessages()
		if len(ctx) < 3 {
			t.Fatalf("expected stored msg + task + decay blocks, got %+v", ctx)
		}
		if !strings.Contains(ctx[0].Content, "parked") || !strings.Contains(ctx[1].Content, "[task]") {
			t.Fatalf("unexpected context projection: %+v", ctx)
		}
	})

	t.Run("DecayBlocks ttl zero and skipped states", func(t *testing.T) {
		s := tmpSession(t)
		s.Append(Msg{Role: "system", Content: "sys"})
		s.Append(Msg{Role: "user", Content: "active"})
		s.Append(Msg{Role: "user", Content: "already parked", Parked: true})
		s.Append(Msg{Role: "user", Content: "ttl zero", TTL: 0})
		s.Append(Msg{Role: "user", Content: "forgotten", Forgotten: true, ForgetReason: "gone"})
		for i := range s.Messages {
			switch s.Messages[i].Content {
			case "active":
				s.Messages[i].TTL = 2
			case "ttl zero":
				s.Messages[i].TTL = 0
			case "forgotten":
				s.Messages[i].TTL = 0
			}
		}
		s.DecayBlocks()
		if s.Turn != 1 {
			t.Fatalf("expected turn increment, got %d", s.Turn)
		}
		foundAutoPark := false
		for _, m := range s.Messages {
			if m.Content == "ttl zero" && m.Parked {
				foundAutoPark = true
			}
		}
		if !foundAutoPark {
			t.Fatalf("expected ttl-zero block to auto-park, got %+v", s.Messages)
		}
	})

	t.Run("DecayStatusBlock empty state", func(t *testing.T) {
		s := tmpSession(t)
		out := s.DecayStatusBlock()
		if !strings.Contains(out, "[memory — turn 0 | active: 0 | parked: 0") {
			t.Fatalf("unexpected decay status output: %q", out)
		}
	})

	t.Run("DecayStatusBlock shows pressure markers and parked ids", func(t *testing.T) {
		s := tmpSession(t)
		s.Turn = 4
		s.Append(Msg{Role: "user", Content: "one", TTL: 3})
		s.Append(Msg{Role: "assistant", Content: "two", TTL: 1})
		s.Append(Msg{Role: "tool", Content: "three", TTL: 0})
		if err := s.Park(s.Messages[2].ID, "gist", "done"); err != nil {
			t.Fatal(err)
		}
		out := s.DecayStatusBlock()
		if !strings.Contains(out, "#"+s.Messages[0].ID+" user ttl=3") ||
			!strings.Contains(out, "#"+s.Messages[1].ID+" assistant ttl=1 !") ||
			!strings.Contains(out, "parked: "+s.Messages[2].ID) ||
			!strings.Contains(out, "park <id> | forget <id> | refresh <id> | recall <id|query>") {
			t.Fatalf("unexpected decay status detail: %q", out)
		}
	})

	t.Run("direct memory method edge cases", func(t *testing.T) {
		s := tmpSession(t)
		initSessionMessages(s)
		if err := s.Park("", "x", ""); err == nil {
			t.Fatal("expected park reason error")
		}
		if err := s.Park("", "x", "why"); err == nil || !strings.Contains(err.Error(), "cannot park system message") {
			t.Fatalf("expected system park error, got %v", err)
		}
		if err := s.Refresh("", 0); err == nil || !strings.Contains(err.Error(), "cannot refresh system message") {
			t.Fatalf("expected system refresh error, got %v", err)
		}
		if err := s.Forget("", "why"); err == nil || !strings.Contains(err.Error(), "cannot forget system message") {
			t.Fatalf("expected system forget error, got %v", err)
		}

		s2 := tmpSession(t)
		if err := s2.Park("missing", "x", "why"); err == nil || !strings.Contains(err.Error(), "not found") {
			t.Fatalf("expected park not-found error, got %v", err)
		}
		if err := s2.Refresh("missing", 1); err == nil || !strings.Contains(err.Error(), "not found") {
			t.Fatalf("expected refresh not-found error, got %v", err)
		}
		if err := s2.Forget("missing", "why"); err == nil || !strings.Contains(err.Error(), "not found") {
			t.Fatalf("expected forget not-found error, got %v", err)
		}

		s2.Append(Msg{Role: "user", Content: "repark me"})
		id := s2.Messages[len(s2.Messages)-1].ID
		if err := s2.Park(id, "gist1", "why1"); err != nil {
			t.Fatal(err)
		}
		if err := s2.Park(id, "gist2", "why2"); err != nil {
			t.Fatal(err)
		}
		if rec := s2.ParkedBlocks[id]; rec.Summary != "gist2" || rec.Reason != "why2" || rec.OriginalContent != "repark me" {
			t.Fatalf("re-park should update metadata but keep original, got %+v", rec)
		}

		s3 := tmpSession(t)
		s3.ParkedBlocks["m9"] = ParkedBlock{ID: "m9", OriginalContent: "x"}
		if err := s3.Forget("m9", "drop parked-only"); err != nil {
			t.Fatal(err)
		}
		if _, ok := s3.ParkedBlocks["m9"]; ok {
			t.Fatal("parked-only forget should remove record")
		}

		s4 := tmpSession(t)
		s4.Append(Msg{Role: "user", Content: "dup query"})
		id4 := s4.Messages[len(s4.Messages)-1].ID
		if err := s4.Park(id4, "same", "same"); err != nil {
			t.Fatal(err)
		}
		recs := s4.Recall([]string{id4}, "dup query")
		if len(recs) != 1 {
			t.Fatalf("recall should dedupe id/query overlap, got %+v", recs)
		}
	})

	t.Run("autoPark ignores empty content", func(t *testing.T) {
		s := tmpSession(t)
		m := &Msg{ID: "m1", Role: "tool", Content: ""}
		s.autoPark(m)
		if m.Parked || len(s.ParkedBlocks) != 0 {
			t.Fatalf("empty-content autoPark should no-op, got msg=%+v parked=%+v", m, s.ParkedBlocks)
		}
	})
}

func TestMainEndToEnd(t *testing.T) {
	srv, _ := newSSEServer(t, func(_ int, w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"done\"}}]}\n"))
		_, _ = w.Write([]byte("data: [DONE]\n"))
	})
	defer srv.Close()

	providersPath := writeProvidersFile(t, `{"providers":[{"name":"test","base_url":"`+srv.URL+`","model":"m"}]}`)
	sessionPath := filepath.Join(t.TempDir(), "session.json")
	t.Setenv("AXON_PROVIDERS_PATH", providersPath)
	t.Setenv("AXON_SESSION_PATH", sessionPath)

	oldStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = r
	defer func() {
		os.Stdin = oldStdin
	}()

	go func() {
		_, _ = w.Write([]byte("hello\n"))
		_ = w.Close()
	}()

	out := captureStdout(t, func() {
		main()
	})
	if !strings.Contains(out, "test · m") || !strings.Contains(out, "done") {
		t.Fatalf("unexpected main output: %q", out)
	}
	data, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "hello") || !strings.Contains(string(data), "done") {
		t.Fatalf("session file should persist conversation, got %s", data)
	}
}

func TestMainErrorBranches(t *testing.T) {
	t.Run("provider file parse error", func(t *testing.T) {
		t.Setenv("AXON_PROVIDERS_PATH", writeProvidersFile(t, "{"))
		t.Setenv("AXON_SESSION_PATH", filepath.Join(t.TempDir(), "session.json"))
		out := captureStdout(t, func() {
			main()
		})
		if !strings.Contains(out, "✗") {
			t.Fatalf("expected ui error output, got %q", out)
		}
	})

	t.Run("resolve provider error", func(t *testing.T) {
		t.Setenv("AXON_PROVIDERS_PATH", writeProvidersFile(t, `{"providers":[]}`))
		t.Setenv("AXON_SESSION_PATH", filepath.Join(t.TempDir(), "session.json"))
		out := captureStdout(t, func() {
			main()
		})
		if !strings.Contains(out, "no provider configured") {
			t.Fatalf("expected resolve-provider error, got %q", out)
		}
	})

	t.Run("new client error", func(t *testing.T) {
		t.Setenv("AXON_PROVIDERS_PATH", writeProvidersFile(t, `{"providers":[{"name":"broken","base_url":"","model":"m"}]}`))
		t.Setenv("AXON_SESSION_PATH", filepath.Join(t.TempDir(), "session.json"))
		out := captureStdout(t, func() {
			main()
		})
		if !strings.Contains(out, `provider "broken" has no base_url`) {
			t.Fatalf("expected new-client error, got %q", out)
		}
	})

	t.Run("agent run error is surfaced", func(t *testing.T) {
		srv, _ := newSSEServer(t, func(_ int, w http.ResponseWriter, r *http.Request) {
			http.Error(w, "bad request", http.StatusBadRequest)
		})
		defer srv.Close()

		t.Setenv("AXON_PROVIDERS_PATH", writeProvidersFile(t, `{"providers":[{"name":"test","base_url":"`+srv.URL+`","model":"m"}]}`))
		t.Setenv("AXON_SESSION_PATH", filepath.Join(t.TempDir(), "session.json"))

		oldStdin := os.Stdin
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		os.Stdin = r
		defer func() {
			os.Stdin = oldStdin
		}()

		go func() {
			_, _ = w.Write([]byte("hello\n"))
			_ = w.Close()
		}()

		out := captureStdout(t, func() {
			main()
		})
		if !strings.Contains(out, "API error 400") {
			t.Fatalf("expected agent-run error output, got %q", out)
		}
	})
}
