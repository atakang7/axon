package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// X1 — every tool requires reason. Exercises one mode per tool.
func TestX1_ReasonRequired(t *testing.T) {
	s := newSession(t)
	writeFile(t, s, "f.txt", "hi\n")
	cases := []struct {
		name string
		args map[string]any
	}{
		{"read", map[string]any{"path": "f.txt", "mode": "full"}},
		{"write", map[string]any{"path": "f.txt", "mode": "save", "content": "x"}},
		{"search", map[string]any{"query": "x", "mode": "literal"}},
		{"exec", map[string]any{"mode": "run", "command": "echo x", "tail_lines": 5}},
		{"bash_output", map[string]any{"shell_id": "bash_1"}},
		{"kill_shell", map[string]any{"shell_id": "bash_1"}},
	}
	for _, c := range cases {
		_, err := callTool(t, context.Background(), s, c.name, c.args)
		if err == nil || !contains(err.Error(), "reason is required") {
			t.Errorf("%s: expected reason-required error, got %v", c.name, err)
		}
	}
}

// X2 — invalid JSON args produce an error, not a panic.
func TestX2_InvalidJSON(t *testing.T) {
	s := newSession(t)
	tools, err := BuildTools(s, nil)
	if err != nil {
		t.Fatalf("BuildTools: %v", err)
	}
	for _, tool := range tools {
		_, err := tool.Fn(context.Background(), json.RawMessage("{not json}"))
		if err == nil {
			t.Errorf("%s: expected error on invalid JSON", tool.Name)
		}
	}
}

// X3 — unknown mode produces a clean error.
func TestX3_UnknownMode(t *testing.T) {
	s := newSession(t)
	writeFile(t, s, "f.txt", "x\n")
	cases := []struct {
		name string
		args map[string]any
	}{
		{"read", map[string]any{"path": "f.txt", "mode": "bogus", "reason": "t"}},
		{"write", map[string]any{"path": "f.txt", "mode": "bogus", "content": "x", "reason": "t"}},
		{"search", map[string]any{"query": "x", "mode": "bogus", "reason": "t"}},
		{"exec", map[string]any{"mode": "bogus", "reason": "t"}},
	}
	for _, c := range cases {
		_, err := callTool(t, context.Background(), s, c.name, c.args)
		if err == nil {
			t.Errorf("%s: expected error on bogus mode", c.name)
		}
	}
}

// X4 — relative path resolves against session.Cwd.
func TestX4_RelativePathRespectsSessionCwd(t *testing.T) {
	s := newSession(t)
	writeFile(t, s, "marker.txt", "yes\n")
	out, err := callTool(t, context.Background(), s, "read", map[string]any{
		"path": "marker.txt", "mode": "full", "reason": "t",
	})
	mustNoErr(t, err)
	mustContain(t, out, "yes")
}

// X5 — absolute path bypasses session.Cwd.
func TestX5_AbsolutePathBypass(t *testing.T) {
	s := newSession(t)
	otherDir := t.TempDir()
	abs := filepath.Join(otherDir, "b.txt")
	if err := os.WriteFile(abs, []byte("absolute\n"), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := callTool(t, context.Background(), s, "read", map[string]any{
		"path": abs, "mode": "full", "reason": "t",
	})
	mustNoErr(t, err)
	mustContain(t, out, "absolute")
}

// X6 — concurrent writes to different files do not interfere and leave no
// temp files behind.
func TestX6_ConcurrentWritesDifferentFiles(t *testing.T) {
	s := newSession(t)
	var wg sync.WaitGroup
	errs := make(chan error, 3)
	for i, name := range []string{"a.txt", "b.txt", "c.txt"} {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			_, err := callTool(t, context.Background(), s, "write", map[string]any{
				"path": name, "mode": "save",
				"content": "n=" + string(rune('0'+i)) + "\n", "reason": "t",
			})
			errs <- err
		}(i, name)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		mustNoErr(t, err)
	}
	// Look for stray .axon-write-* files.
	entries, _ := os.ReadDir(s.Cwd)
	for _, e := range entries {
		if startsWith(e.Name(), ".axon-write-") {
			t.Fatalf("temp file leaked: %s", e.Name())
		}
	}
}

// X9 — formatter that errors does not corrupt the file. With a syntactically
// invalid Go file, gofmt fails — but the bytes on disk should still be the
// exact bytes we sent.
func TestX9_FormatterFailureDoesNotCorruptFile(t *testing.T) {
	s := newSession(t)
	bad := "package x\nfunc (\n"
	_, err := callTool(t, context.Background(), s, "write", map[string]any{
		"path": "bad.go", "mode": "save", "content": bad, "reason": "t",
	})
	mustNoErr(t, err)
	got := string(readFileBytes(t, s, "bad.go"))
	if got != bad {
		t.Fatalf("formatter mutated bad file:\nwant %q\n got %q", bad, got)
	}
}

// X10 — Undo on empty stack returns false.
func TestX10_UndoEmpty(t *testing.T) {
	s := newSession(t)
	if _, ok := s.Undo(); ok {
		t.Fatal("expected no edits to undo")
	}
}

// X11 — Undo is LIFO across multiple edits.
func TestX11_UndoLIFO(t *testing.T) {
	s := newSession(t)
	writeFile(t, s, "f.txt", "v0")
	for i, c := range []string{"v1", "v2", "v3"} {
		_, err := callTool(t, context.Background(), s, "write", map[string]any{
			"path": "f.txt", "mode": "save", "content": c, "reason": "t",
		})
		mustNoErr(t, err)
		_ = i
	}
	want := []string{"v2", "v1", "v0"}
	for _, expected := range want {
		e, ok := s.Undo()
		if !ok {
			t.Fatalf("expected another undo, file should be %q", expected)
		}
		if err := writeBytesRaw(e.Path, []byte(e.Before)); err != nil {
			t.Fatalf("undo write: %v", err)
		}
		if got := string(readFileBytes(t, s, "f.txt")); got != expected {
			t.Fatalf("after undo: want %q, got %q", expected, got)
		}
	}
}

// helpers — kept tiny so tests don't pull strings unnecessarily.
func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
func startsWith(s, p string) bool { return len(s) >= len(p) && s[:len(p)] == p }
