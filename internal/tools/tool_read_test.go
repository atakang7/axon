package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atakang7/axon/internal/config"
)

// dirWorkspace is the whole fake needed to exercise a filesystem tool. That it
// fits in a few lines is the point of the narrow Workspace interface: a tool
// can be tested without a Session, a conversation, or a provider.
type dirWorkspace struct {
	dir   string
	edits int
}

func (w *dirWorkspace) Dir() string { return w.dir }
func (w *dirWorkspace) ResolvePath(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(w.dir, p)
}
func (w *dirWorkspace) RecordEdit(_, _, _ string) { w.edits++ }

func call(t *testing.T, tool Tool, args any) string {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	out, err := tool.Fn(context.Background(), raw)
	if err != nil {
		t.Fatalf("%s: %v", tool.Name, err)
	}
	return out
}

// Caps must come from the Limits value handed to the constructor, not from the
// process environment. This is what lets two agents in one process be tuned
// differently, and what lets this test run without touching os.Environ.
func TestReadHonoursInjectedLimits(t *testing.T) {
	ws := &dirWorkspace{dir: t.TempDir()}
	big := filepath.Join(ws.dir, "big.txt")
	if err := os.WriteFile(big, []byte(strings.Repeat("x", 4096)), 0644); err != nil {
		t.Fatal(err)
	}

	tight := ReadTool(ws, config.Limits{ReadLines: 1, ReadMaxBytes: 100})
	if out := call(t, tight, map[string]any{"path": "big.txt", "mode": "full"}); !strings.Contains(out, "refused") {
		t.Fatalf("full read ignored the injected byte cap: %q", out)
	}

	roomy := ReadTool(ws, config.Limits{ReadLines: 1, ReadMaxBytes: 1 << 20})
	if out := call(t, roomy, map[string]any{"path": "big.txt", "mode": "full"}); strings.Contains(out, "refused") {
		t.Fatalf("a higher cap on a second tool instance was not honoured: %q", out)
	}
}

// The default slice length comes from Limits too, and a per-call limit wins.
func TestReadSliceUsesInjectedDefault(t *testing.T) {
	ws := &dirWorkspace{dir: t.TempDir()}
	path := filepath.Join(ws.dir, "lines.txt")
	if err := os.WriteFile(path, []byte("a\nb\nc\nd\ne\n"), 0644); err != nil {
		t.Fatal(err)
	}
	tool := ReadTool(ws, config.Limits{ReadLines: 2, ReadMaxBytes: 1 << 20})

	if got := strings.Count(call(t, tool, map[string]any{"path": "lines.txt"}), "\n") + 1; got != 2 {
		t.Fatalf("slice returned %d lines, want the injected default of 2", got)
	}
	if got := strings.Count(call(t, tool, map[string]any{"path": "lines.txt", "limit": 4}), "\n") + 1; got != 4 {
		t.Fatalf("explicit limit ignored: got %d lines, want 4", got)
	}
}

// Reading a binary file must be refused rather than dumped into context.
func TestReadRefusesBinary(t *testing.T) {
	ws := &dirWorkspace{dir: t.TempDir()}
	if err := os.WriteFile(filepath.Join(ws.dir, "a.bin"), []byte{0x7f, 'E', 'L', 'F', 0x00, 0x01, 0x02}, 0644); err != nil {
		t.Fatal(err)
	}
	tool := ReadTool(ws, config.LoadLimits())
	if out := call(t, tool, map[string]any{"path": "a.bin", "mode": "full"}); !strings.Contains(out, "binary file refused") {
		t.Fatalf("binary content was not refused: %q", out)
	}
}

// A write records an undo entry through the Workspace, and nothing else.
func TestWriteRecordsEditThroughWorkspace(t *testing.T) {
	ws := &dirWorkspace{dir: t.TempDir()}
	tool := WriteTool(ws)

	call(t, tool, map[string]any{"path": "new.txt", "mode": "save", "content": "hello\n"})
	if ws.edits != 1 {
		t.Fatalf("write recorded %d edits, want 1", ws.edits)
	}
	body, err := os.ReadFile(filepath.Join(ws.dir, "new.txt"))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(body) != "hello\n" {
		t.Fatalf("file content %q, want %q", body, "hello\n")
	}
}
