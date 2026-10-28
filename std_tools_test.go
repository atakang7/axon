package axon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func stdSession(t *testing.T) (*Session, string) {
	t.Helper()
	dir := t.TempDir()
	s := newTestSession(t)
	if err := s.SetCwd(dir); err != nil {
		t.Fatal(err)
	}
	return s, dir
}

func TestEditNeedsAReadAndAnUnchangedFile(t *testing.T) {
	s, dir := stdSession(t)
	path := filepath.Join(dir, "a.txt")
	os.WriteFile(path, []byte("one\ntwo\n"), 0644)
	read, edit := ReadFileTool(s, testLimits()), EditTool(s)
	args := mustJSON(t, map[string]any{"path": "a.txt", "old_string": "one", "new_string": "1"})

	if _, err := edit.Fn(context.Background(), args); err == nil || !strings.Contains(err.Error(), "has not been read") {
		t.Fatalf("edit before read: %v", err)
	}
	if _, err := read.Fn(context.Background(), mustJSON(t, map[string]any{"path": "a.txt"})); err != nil {
		t.Fatal(err)
	}
	if out, err := edit.Fn(context.Background(), args); err != nil || !strings.Contains(out, "edited a.txt") {
		t.Fatalf("edit after read: %q %v", out, err)
	}
	// The model's own edit keeps the file current: a second edit needs no read.
	if _, err := edit.Fn(context.Background(), mustJSON(t, map[string]any{"path": "a.txt", "old_string": "two", "new_string": "2"})); err != nil {
		t.Fatalf("second edit: %v", err)
	}
	// Someone else changes the file: the next edit must read it again.
	time.Sleep(10 * time.Millisecond)
	os.WriteFile(path, []byte("1\n2\nthree\n"), 0644)
	if _, err := edit.Fn(context.Background(), mustJSON(t, map[string]any{"path": "a.txt", "old_string": "three", "new_string": "3"})); err == nil || !strings.Contains(err.Error(), "changed since") {
		t.Fatalf("edit of a changed file: %v", err)
	}
}

func TestEditReplaceAllAndUniqueness(t *testing.T) {
	s, dir := stdSession(t)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("x\nx\n"), 0644)
	ReadFileTool(s, testLimits()).Fn(context.Background(), mustJSON(t, map[string]any{"path": "b.txt"}))
	edit := EditTool(s)
	if _, err := edit.Fn(context.Background(), mustJSON(t, map[string]any{"path": "b.txt", "old_string": "x", "new_string": "y"})); err == nil || !strings.Contains(err.Error(), "appears 2 times") {
		t.Fatalf("ambiguous edit: %v", err)
	}
	if _, err := edit.Fn(context.Background(), mustJSON(t, map[string]any{"path": "b.txt", "old_string": "x", "new_string": "y", "replace_all": true})); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "b.txt")); string(got) != "y\ny\n" {
		t.Fatalf("b.txt = %q", got)
	}
}

func TestWriteFileCreatesFreelyButReplacesOnlyWhatWasRead(t *testing.T) {
	s, dir := stdSession(t)
	w := WriteFileTool(s)
	if _, err := w.Fn(context.Background(), mustJSON(t, map[string]any{"path": "new.txt", "content": "hi\n"})); err != nil {
		t.Fatalf("create: %v", err)
	}
	os.WriteFile(filepath.Join(dir, "old.txt"), []byte("keep\n"), 0644)
	if _, err := w.Fn(context.Background(), mustJSON(t, map[string]any{"path": "old.txt", "content": "gone\n"})); err == nil {
		t.Fatal("replaced a file it never read")
	}
}

func TestGlobMatchesDoubleStarAndBaseNames(t *testing.T) {
	s, dir := stdSession(t)
	os.MkdirAll(filepath.Join(dir, "src", "api"), 0755)
	os.MkdirAll(filepath.Join(dir, "node_modules", "x"), 0755)
	for _, f := range []string{"main.go", "src/api/h.go", "src/api/h_test.go", "src/readme.md", "node_modules/x/i.go"} {
		os.WriteFile(filepath.Join(dir, f), []byte("x"), 0644)
	}
	g := GlobTool(s)
	out, _ := g.Fn(context.Background(), mustJSON(t, map[string]any{"pattern": "**/*.go"}))
	if out != "main.go\nsrc/api/h.go\nsrc/api/h_test.go" {
		t.Fatalf("**/*.go:\n%s", out)
	}
	out, _ = g.Fn(context.Background(), mustJSON(t, map[string]any{"pattern": "*_test.go"}))
	if out != "src/api/h_test.go" {
		t.Fatalf("*_test.go:\n%s", out)
	}
}

func TestVerificationNeedsACheckAfterTheLastChange(t *testing.T) {
	b := newRunBuilder(t)
	b.user("fix it")
	b.call(toolEdit, map[string]any{"path": "a.go"}, "edited a.go", false)
	if v := b.s.verification(); !v.wrote || v.state != "unverified" || v.note() == "" {
		t.Fatalf("after an edit: %+v", v)
	}
	b.call(toolExec, map[string]any{"command": "ls && cat a.go"}, "exit_code: 0\na.go", false)
	if v := b.s.verification(); v.state != "unverified" {
		t.Fatalf("looking is not checking: %+v", v)
	}
	b.call(toolExec, map[string]any{"command": "go test ./..."}, "exit_code: 1\nFAIL", false)
	if v := b.s.verification(); v.state != "failed" || !strings.Contains(v.note(), "failed") {
		t.Fatalf("a failing check: %+v", v)
	}
	b.call(toolExec, map[string]any{"command": "cd pkg && go test ./..."}, "exit_code: 0\nok", false)
	if v := b.s.verification(); v.state != "verified" || v.note() != "" {
		t.Fatalf("a passing check: %+v", v)
	}
	if !strings.Contains(b.s.stateBlock(), "passed") {
		t.Fatalf("state block: %s", b.s.stateBlock())
	}
}
