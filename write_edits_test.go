package axon

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteEditsApplyAcrossFilesInOneCall(t *testing.T) {
	root := t.TempDir()
	a, b := filepath.Join(root, "a.txt"), filepath.Join(root, "b.txt")
	os.WriteFile(a, []byte("one\ntwo\nthree\n"), 0644)
	os.WriteFile(b, []byte("alpha\nbeta\n"), 0644)
	tool := WriteTool(newFakeWorkspace(root))

	out, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{
		"path": "a.txt",
		"edits": []map[string]any{
			{"old_str": "one", "content": "1"},
			{"old_str": "1\ntwo", "content": "1\n2"}, // sees the edit before it
			{"path": "b.txt", "old_str": "beta", "content": "BETA"},
		},
	}))
	if err != nil {
		t.Fatalf("Fn: %v", err)
	}
	if !strings.Contains(out, "applied 3 edits") {
		t.Fatalf("out = %q", out)
	}
	if got, _ := os.ReadFile(a); string(got) != "1\n2\nthree\n" {
		t.Fatalf("a.txt = %q", got)
	}
	if got, _ := os.ReadFile(b); string(got) != "alpha\nBETA\n" {
		t.Fatalf("b.txt = %q", got)
	}
}

func TestWriteEditsAreAllOrNone(t *testing.T) {
	root := t.TempDir()
	a, b := filepath.Join(root, "a.txt"), filepath.Join(root, "b.txt")
	os.WriteFile(a, []byte("one\n"), 0644)
	os.WriteFile(b, []byte("x\nx\n"), 0644)
	tool := WriteTool(newFakeWorkspace(root))

	_, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{
		"edits": []map[string]any{
			{"path": "a.txt", "old_str": "one", "content": "1"},
			{"path": "b.txt", "old_str": "x", "content": "y"},
			{"path": "b.txt", "old_str": "missing", "content": "z"},
		},
	}))
	if err == nil {
		t.Fatal("want an error")
	}
	for _, want := range []string{"edit 2", "matches 2 times", "edit 3", "not found"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q lacks %q", err, want)
		}
	}
	if got, _ := os.ReadFile(a); string(got) != "one\n" {
		t.Fatalf("a.txt changed although the call failed: %q", got)
	}
}

func TestWriteReportsBrokenSyntax(t *testing.T) {
	root := t.TempDir()
	tool := WriteTool(newFakeWorkspace(root))
	out, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{
		"path": "x.go", "mode": "save", "content": "package x\n\nfunc F( {\n",
	}))
	if err != nil {
		t.Fatalf("Fn: %v", err)
	}
	if !strings.Contains(out, "check: FAILED") || !strings.Contains(out, "go syntax") {
		t.Fatalf("broken Go was not reported: %q", out)
	}
	out, _ = tool.Fn(context.Background(), mustJSON(t, map[string]any{
		"path": "c.json", "mode": "save", "content": "{\n  \"a\": 1,\n}\n",
	}))
	if !strings.Contains(out, "json") || !strings.Contains(out, "line 3") {
		t.Fatalf("broken JSON was not reported: %q", out)
	}
	out, _ = tool.Fn(context.Background(), mustJSON(t, map[string]any{
		"path": "ok.json", "mode": "save", "content": "{\"a\": 1}\n",
	}))
	if strings.Contains(out, "check:") {
		t.Fatalf("valid JSON should add nothing: %q", out)
	}
}

func TestWriteBuildsTheGoPackageItChanged(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not on PATH")
	}
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n\ngo 1.21\n"), 0644)
	tool := WriteTool(newFakeWorkspace(root))
	out, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{
		"path": "x.go", "mode": "save", "content": "package x\n\nfunc F() int { return undefinedName }\n",
	}))
	if err != nil {
		t.Fatalf("Fn: %v", err)
	}
	if !strings.Contains(out, "check: FAILED") || !strings.Contains(out, "undefinedName") {
		t.Fatalf("a type error was not reported: %q", out)
	}
}
