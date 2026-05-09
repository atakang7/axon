package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// newSession creates a Session rooted at a fresh temp dir. The session is
// not persisted to disk — these tests do not exercise session save/load.
func newSession(t *testing.T) *Session {
	t.Helper()
	dir := t.TempDir()
	return &Session{Cwd: dir, ParkedBlocks: map[string]ParkedBlock{}}
}

// callTool finds a tool by name on a freshly built tool set and invokes it
// with the given JSON arguments. ctx is passed through so cancellation tests
// can use it.
func callTool(t *testing.T, ctx context.Context, s *Session, name string, args map[string]any) (string, error) {
	t.Helper()
	tools := BuildTools(s)
	for _, tool := range tools {
		if tool.Name == name {
			raw, err := json.Marshal(args)
			if err != nil {
				t.Fatalf("marshal args: %v", err)
			}
			return tool.Fn(ctx, raw)
		}
	}
	t.Fatalf("tool %q not registered", name)
	return "", nil
}

// writeFile writes content to <session.Cwd>/relpath, creating parents.
func writeFile(t *testing.T, s *Session, relpath, content string) string {
	t.Helper()
	abs := filepath.Join(s.Cwd, relpath)
	if dir := filepath.Dir(abs); dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	if err := os.WriteFile(abs, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", abs, err)
	}
	return abs
}

// writeFileMode writes content with a specific mode.
func writeFileMode(t *testing.T, s *Session, relpath, content string, mode os.FileMode) string {
	t.Helper()
	abs := writeFile(t, s, relpath, content)
	if err := os.Chmod(abs, mode); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	return abs
}

// readFile reads <session.Cwd>/relpath.
func readFileBytes(t *testing.T, s *Session, relpath string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(s.Cwd, relpath))
	if err != nil {
		t.Fatalf("read %s: %v", relpath, err)
	}
	return b
}

// fileMode returns the perm bits of <session.Cwd>/relpath.
func fileMode(t *testing.T, s *Session, relpath string) os.FileMode {
	t.Helper()
	info, err := os.Stat(filepath.Join(s.Cwd, relpath))
	if err != nil {
		t.Fatalf("stat %s: %v", relpath, err)
	}
	return info.Mode().Perm()
}

// mustContain fails the test if haystack does not contain needle.
func mustContain(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("expected output to contain %q\n--- got ---\n%s\n--- end ---", needle, haystack)
	}
}

// mustNotContain fails if the substring is present.
func mustNotContain(t *testing.T, haystack, needle string) {
	t.Helper()
	if strings.Contains(haystack, needle) {
		t.Fatalf("expected output NOT to contain %q\n--- got ---\n%s\n--- end ---", needle, haystack)
	}
}

// mustErr asserts that err is non-nil and its message contains substring.
func mustErr(t *testing.T, err error, substr string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error containing %q, got nil", substr)
	}
	if !strings.Contains(err.Error(), substr) {
		t.Fatalf("expected error containing %q, got %v", substr, err)
	}
}

// mustNoErr fails the test if err is non-nil.
func mustNoErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// rgAvailable reports whether ripgrep is on PATH. Search tests skip if absent.
func rgAvailable() bool {
	_, err := exec.LookPath("rg")
	return err == nil
}
