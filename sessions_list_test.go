package axon

import (
	"context"
	"path/filepath"
	"testing"
)

// TestSwitchSessionPreservesMessages is a regression test: SwitchSession used
// to rebuild the system prompt by wiping the whole Messages slice, which
// discarded every user, assistant and tool message loaded from disk. After
// switching, the model saw a fresh conversation and could not recall earlier
// turns. The fix rebuilds the leading system message in place and leaves the
// rest of the history intact.
func TestSwitchSessionPreservesMessages(t *testing.T) {
	dir := t.TempDir()
	primary := filepath.Join(dir, "primary.json")
	other := filepath.Join(dir, "other.json")

	// First agent: run one turn so the session file on disk holds a real
	// user/assistant exchange, then close it. baseConfig pins its own temp
	// path, so override the env afterwards to point at primary.
	cfg := baseConfig(t)
	t.Setenv("AXON_SESSION_PATH", primary)
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := a.Step(context.Background(), "hello"); err != nil {
		t.Fatalf("Step: %v", err)
	}
	a.Close()

	// Second agent pointed at a different file, then switch onto primary.
	// The switch must load the saved conversation, not start fresh.
	cfg2 := baseConfig(t)
	t.Setenv("AXON_SESSION_PATH", other)
	b, err := New(cfg2)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer b.Close()

	if err := b.SwitchSession(primary); err != nil {
		t.Fatalf("SwitchSession: %v", err)
	}

	msgs := b.Session().Messages
	if len(msgs) < 3 {
		t.Fatalf("after SwitchSession: %d messages, want >= 3 (system + user + assistant)", len(msgs))
	}
	if msgs[0].Role != "system" {
		t.Fatalf("first message role = %q, want system", msgs[0].Role)
	}
	if msgs[1].Role != "user" || msgs[1].Content != "hello" {
		t.Fatalf("second message = {%s, %q}, want {user, hello}", msgs[1].Role, msgs[1].Content)
	}
	if msgs[2].Role != "assistant" || msgs[2].Content != "ok" {
		t.Fatalf("third message = {%s, %q}, want {assistant, ok}", msgs[2].Role, msgs[2].Content)
	}
}
