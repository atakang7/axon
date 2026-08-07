package tools

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atakang7/axon/internal/config"
)

func searchIn(t *testing.T) (*dirWorkspace, Tool) {
	t.Helper()
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg not installed")
	}
	ws := &dirWorkspace{dir: t.TempDir()}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(ws.dir, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.go", "package a\n\nfunc Alpha() {}\n")
	write("b.go", "package a\n\nfunc Beta() { Alpha() }\n")
	write("notes.txt", "alpha appears here too\n")
	return ws, SearchTool(ws, config.LoadLimits())
}

// Literal mode matches exact text and is case-insensitive by default.
func TestSearchLiteral(t *testing.T) {
	_, tool := searchIn(t)
	out := call(t, tool, map[string]any{"query": "Alpha", "mode": "literal"})
	if !strings.Contains(out, "a.go") || !strings.Contains(out, "b.go") {
		t.Fatalf("literal search missed a file:\n%s", out)
	}
	if !strings.Contains(out, "notes.txt") {
		t.Fatalf("default search should be case-insensitive, so 'alpha' in notes.txt matches:\n%s", out)
	}
}

// A query with no matches is a result, not an error. ripgrep exits 1 for this
// and the shared runner must not surface that as a failure.
func TestSearchNoMatchesIsNotAnError(t *testing.T) {
	_, tool := searchIn(t)
	raw, _ := json.Marshal(map[string]any{"query": "ThisAppearsNowhere"})
	out, err := tool.Fn(context.Background(), raw)
	if err != nil {
		t.Fatalf("no-match search returned an error: %v", err)
	}
	if !strings.Contains(out, "no matches") {
		t.Fatalf("expected 'no matches', got:\n%s", out)
	}
}

// Globs restrict the file set.
func TestSearchGlobFilter(t *testing.T) {
	_, tool := searchIn(t)
	out := call(t, tool, map[string]any{"query": "alpha", "globs": []string{"*.txt"}})
	if strings.Contains(out, "a.go") {
		t.Fatalf("glob filter did not exclude .go files:\n%s", out)
	}
	if !strings.Contains(out, "notes.txt") {
		t.Fatalf("glob filter excluded the file it should have kept:\n%s", out)
	}
}

// Trace finds a symbol's definition and its callers.
func TestSearchTrace(t *testing.T) {
	_, tool := searchIn(t)
	out := call(t, tool, map[string]any{"query": "Alpha", "mode": "trace"})
	if !strings.Contains(out, "a.go") || !strings.Contains(out, "b.go") {
		t.Fatalf("trace missed the definition or the caller:\n%s", out)
	}
}

// case_sensitive narrows the match set.
func TestSearchCaseSensitive(t *testing.T) {
	_, tool := searchIn(t)
	out := call(t, tool, map[string]any{"query": "Alpha", "case_sensitive": true})
	if strings.Contains(out, "notes.txt") {
		t.Fatalf("case_sensitive search matched lowercase text:\n%s", out)
	}
}
