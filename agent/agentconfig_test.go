package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeAgentConfig writes a YAML file under a temp $AXON_AGENTS_DIR and returns
// the agent name (basename without extension).
func writeAgentConfig(t *testing.T, name, body string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("AXON_AGENTS_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, name+".yaml"), []byte(body), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

// A1 — empty/default name returns a zero config (built-in default agent).
func TestAgentConfig_DefaultIsZero(t *testing.T) {
	cfg, err := LoadAgentConfig("")
	if err != nil {
		t.Fatalf("LoadAgentConfig(\"\"): %v", err)
	}
	if cfg.Name != "default" {
		t.Fatalf("expected default Name, got %q", cfg.Name)
	}
	if len(cfg.Tools) != 0 || len(cfg.DisableBuiltins) != 0 {
		t.Fatalf("default config should be empty")
	}
}

// A2 — missing agent file produces a clear error, not a panic.
func TestAgentConfig_MissingFile(t *testing.T) {
	t.Setenv("AXON_AGENTS_DIR", t.TempDir())
	_, err := LoadAgentConfig("nope")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected 'not found' error, got %v", err)
	}
}

// A3 — disable_builtins removes a built-in from BuildTools output.
func TestAgentConfig_DisableBuiltins(t *testing.T) {
	writeAgentConfig(t, "readonly", `
name: readonly
disable_builtins: [write, exec, bash_output, kill_shell]
`)
	cfg, err := LoadAgentConfig("readonly")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	tools, err := BuildTools(newSession(t), cfg)
	if err != nil {
		t.Fatalf("BuildTools: %v", err)
	}
	for _, tool := range tools {
		switch tool.Name {
		case toolWrite, toolExec, toolBashOutput, toolKillShell:
			t.Errorf("disabled built-in %q still present", tool.Name)
		}
	}
	// Sanity: read and search must still be there.
	found := map[string]bool{}
	for _, tool := range tools {
		found[tool.Name] = true
	}
	for _, must := range []string{toolRead, toolSearch, toolTask} {
		if !found[must] {
			t.Errorf("built-in %q unexpectedly removed", must)
		}
	}
}

// A4 — a shell custom tool appears in BuildTools and runs the command.
func TestAgentConfig_ShellToolRuns(t *testing.T) {
	writeAgentConfig(t, "echoer", `
name: echoer
tools:
  - name: shout
    type: shell
    description: Echo a message in upper case.
    schema:
      type: object
      required: [msg]
      properties:
        msg: { type: string }
    command: printf '%s' {{.msg | shellQuote}} | tr '[:lower:]' '[:upper:]'
`)
	cfg, err := LoadAgentConfig("echoer")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	tools, err := BuildTools(newSession(t), cfg)
	if err != nil {
		t.Fatalf("BuildTools: %v", err)
	}
	var shout *Tool
	for i, tool := range tools {
		if tool.Name == "shout" {
			shout = &tools[i]
			break
		}
	}
	if shout == nil {
		t.Fatal("shout tool missing from built tool set")
	}
	raw, _ := json.Marshal(map[string]any{"msg": "hello world"})
	out, err := shout.Fn(context.Background(), raw)
	if err != nil {
		t.Fatalf("shout.Fn: %v\noutput: %s", err, out)
	}
	if out != "HELLO WORLD" {
		t.Fatalf("expected HELLO WORLD, got %q", out)
	}
}

// A5 — shellQuote prevents argument injection.
func TestAgentConfig_ShellQuoteIsSafe(t *testing.T) {
	writeAgentConfig(t, "safe", `
name: safe
tools:
  - name: dangerous
    type: shell
    description: Tries to echo arg.
    schema:
      type: object
      required: [arg]
      properties:
        arg: { type: string }
    command: printf '%s' {{.arg | shellQuote}}
`)
	cfg, err := LoadAgentConfig("safe")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	tools, err := BuildTools(newSession(t), cfg)
	if err != nil {
		t.Fatalf("BuildTools: %v", err)
	}
	var tool *Tool
	for i := range tools {
		if tools[i].Name == "dangerous" {
			tool = &tools[i]
		}
	}
	if tool == nil {
		t.Fatal("tool missing")
	}
	// An arg containing $(date) and ; rm -rf / must be treated literally.
	payload := "$(date); echo PWNED"
	raw, _ := json.Marshal(map[string]any{"arg": payload})
	out, err := tool.Fn(context.Background(), raw)
	if err != nil {
		t.Fatalf("Fn: %v", err)
	}
	if out != payload {
		t.Fatalf("shellQuote leaked: expected %q, got %q", payload, out)
	}
}

// A6 — duplicate tool name in config is rejected at load time.
func TestAgentConfig_DuplicateToolRejected(t *testing.T) {
	writeAgentConfig(t, "dup", `
name: dup
tools:
  - { name: foo, type: shell, description: a, command: "true" }
  - { name: foo, type: shell, description: b, command: "true" }
`)
	_, err := LoadAgentConfig("dup")
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}

// A7 — custom tool that collides with a built-in name is rejected.
func TestAgentConfig_CollisionWithBuiltinRejected(t *testing.T) {
	writeAgentConfig(t, "collide", `
name: collide
tools:
  - { name: read, type: shell, description: x, command: "true" }
`)
	_, err := LoadAgentConfig("collide")
	if err == nil || !strings.Contains(err.Error(), "built-in") {
		t.Fatalf("expected built-in collision error, got %v", err)
	}
}

// A8 — type: mcp is rejected with a clear message until implemented.
func TestAgentConfig_MCPReserved(t *testing.T) {
	writeAgentConfig(t, "mcp", `
name: mcp
tools:
  - { name: linear, type: mcp, description: x }
`)
	_, err := LoadAgentConfig("mcp")
	if err == nil || !strings.Contains(err.Error(), "mcp") {
		t.Fatalf("expected mcp reserved error, got %v", err)
	}
}
