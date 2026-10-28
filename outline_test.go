package axon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOutlineListsDeclarationsWithLines(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "pkg"), 0755)
	os.MkdirAll(filepath.Join(root, "node_modules", "x"), 0755)
	os.WriteFile(filepath.Join(root, "pkg", "a.go"), []byte("package pkg\n\n// T is a type.\ntype T struct{}\n\nfunc (T) M() {}\n\nfunc F() {\n}\n"), 0644)
	os.WriteFile(filepath.Join(root, "pkg", "b.py"), []byte("import os\n\nclass C:\n    def m(self):\n        pass\n"), 0644)
	os.WriteFile(filepath.Join(root, "node_modules", "x", "i.js"), []byte("function hidden() {}\n"), 0644)
	tool := ReadTool(newFakeWorkspace(root), testLimits())

	out, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{"path": ".", "mode": "outline"}))
	if err != nil {
		t.Fatalf("Fn: %v", err)
	}
	for _, want := range []string{"pkg/a.go", "4: type T struct", "6: func (T) M()", "8: func F()", "pkg/b.py", "3: class C:", "4:     def m(self)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("outline lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "hidden") {
		t.Fatalf("outline walked node_modules:\n%s", out)
	}

	tree, _ := tool.Fn(context.Background(), mustJSON(t, map[string]any{"path": "."}))
	if !strings.Contains(tree, "pkg/") || !strings.Contains(tree, "  a.go") || strings.Contains(tree, "node_modules") {
		t.Fatalf("tree:\n%s", tree)
	}
}

func TestReadOfTextAlreadyInViewReturnsAPointer(t *testing.T) {
	a := &Agent{session: &Session{}}
	body := strings.Repeat("line of the file\n", 40)
	a.session.Append(Msg{Role: "system", Content: "sys"})
	a.session.Append(Msg{Role: "user", Content: "go"})
	a.session.Append(Msg{Role: "assistant", ToolCalls: []ToolCall{{ID: "c1", Type: "function"}}})
	a.session.Append(Msg{Role: "tool", ToolCallID: "c1", ToolName: toolRead, Content: body})
	last := &a.session.Messages[len(a.session.Messages)-1]
	last.Content = "[#" + last.ID + "]\n" + body

	got := a.dedupRead(Msg{Role: "tool", ToolName: toolRead, Content: body})
	if !strings.Contains(got.Content, "unchanged") || !strings.Contains(got.Content, "#"+last.ID) {
		t.Fatalf("repeat read not deduplicated: %q", got.Content)
	}
	other := a.dedupRead(Msg{Role: "tool", ToolName: toolRead, Content: body + "changed\n"})
	if other.Content != body+"changed\n" {
		t.Fatal("a different text must pass through")
	}
}

func TestWorkspaceBriefNamesCommandsAndInstructions(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n"), 0644)
	os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"scripts":{"test":"vitest run"}}`), 0644)
	os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("Run make check before you finish.\n"), 0644)
	os.MkdirAll(filepath.Join(root, "src"), 0755)
	brief := workspaceBrief(root)
	for _, want := range []string{"src/ (0)", "go test ./...", "npm run test (vitest run)", "AGENTS.md", "Run make check"} {
		if !strings.Contains(brief, want) {
			t.Fatalf("brief lacks %q:\n%s", want, brief)
		}
	}
}
