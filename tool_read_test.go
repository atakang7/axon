package axon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	return b
}

// A blank path is the one input read can never recover from — there is
// nothing to resolve — so it must fail validation before any filesystem call.
func TestReadBlankPathErrors(t *testing.T) {
	ws := newFakeWorkspace(t.TempDir())
	tool := ReadTool(ws, testLimits())

	for _, path := range []string{"", "   "} {
		_, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{"path": path}))
		if err == nil {
			t.Fatalf("path=%q: expected error", path)
		}
	}
}

// Omitting mode must not be an error and must not silently do nothing — the
// cheap common case (read some lines) has to be one argument away, so it
// defaults to slice.
func TestReadDefaultModeIsSlice(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	os.WriteFile(path, []byte("one\ntwo\nthree\n"), 0644)
	ws := newFakeWorkspace(dir)
	tool := ReadTool(ws, testLimits())

	out, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{"path": "f.txt"}))
	if err != nil {
		t.Fatalf("Fn: %v", err)
	}
	if !strings.Contains(out, "1\tone") {
		t.Fatalf("default mode did not produce a numbered slice: %q", out)
	}
}

// An unknown mode must name the two valid modes rather than fail silently or
// vaguely — the model has to be able to self-correct from the error text
// alone, with no other documentation in front of it.
func TestReadUnknownModeErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	os.WriteFile(path, []byte("x\n"), 0644)
	ws := newFakeWorkspace(dir)
	tool := ReadTool(ws, testLimits())

	_, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{"path": "f.txt", "mode": "bogus"}))
	if err == nil {
		t.Fatal("expected error for unknown mode")
	}
	if !strings.Contains(err.Error(), "slice") || !strings.Contains(err.Error(), "full") {
		t.Fatalf("error does not name both valid modes: %v", err)
	}
}

// The slice contract: [offset, offset+limit) with 1-based line numbers. Get
// this wrong and every "read lines 10-20" request from the model quietly
// returns the wrong window with no indication anything is off.
func TestReadSliceRange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	lines := []string{"a", "b", "c", "d", "e"}
	os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0644)
	ws := newFakeWorkspace(dir)
	tool := ReadTool(ws, testLimits())

	out, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{
		"path": "f.txt", "mode": "slice", "offset": 2, "limit": 2,
	}))
	if err != nil {
		t.Fatalf("Fn: %v", err)
	}
	want := "2\tb\n3\tc"
	if out != want {
		t.Fatalf("out = %q, want %q", out, want)
	}
}

// offset<1 must clamp up to 1 (a model that miscounts and asks for line 0 or
// -1 should still get the start of the file, not an error or empty range),
// and limit<=0 must fall back to the configured default rather than returning
// nothing.
func TestReadSliceClampsOffsetAndDefaultsLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	os.WriteFile(path, []byte("a\nb\nc\n"), 0644)
	ws := newFakeWorkspace(dir)
	lim := testLimits()
	lim.ReadLines = 2
	tool := ReadTool(ws, lim)

	out, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{
		"path": "f.txt", "mode": "slice", "offset": -5, "limit": 0,
	}))
	if err != nil {
		t.Fatalf("Fn: %v", err)
	}
	want := "1\ta\n2\tb" // offset clamped to 1, limit defaulted to injected 2
	if out != want {
		t.Fatalf("out = %q, want %q", out, want)
	}
}

// Asking past EOF is a normal thing for a model iterating through a file to
// do once it hits the end; it must come back as a clearly empty result, not
// an error that looks like something went wrong.
func TestReadSliceOffsetPastEOF(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	os.WriteFile(path, []byte("a\nb\n"), 0644)
	ws := newFakeWorkspace(dir)
	tool := ReadTool(ws, testLimits())

	out, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{
		"path": "f.txt", "mode": "slice", "offset": 50, "limit": 10,
	}))
	if err != nil {
		t.Fatalf("Fn: %v", err)
	}
	if out != "[empty range]" {
		t.Fatalf("out = %q, want [empty range]", out)
	}
}

// An unbounded line (a minified JS bundle, a generated file) would otherwise
// push tens of thousands of tokens into context off a single "line". The
// per-line display cap keeps one long line from blowing the whole read's
// budget.
func TestReadSliceTruncatesLongLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	long := strings.Repeat("x", 9000)
	os.WriteFile(path, []byte(long+"\n"), 0644)
	ws := newFakeWorkspace(dir)
	tool := ReadTool(ws, testLimits())

	out, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{
		"path": "f.txt", "mode": "slice", "offset": 1, "limit": 1,
	}))
	if err != nil {
		t.Fatalf("Fn: %v", err)
	}
	if !strings.Contains(out, "...[line truncated]") {
		t.Fatalf("long line was not truncated: len=%d", len(out))
	}
	if strings.Contains(out, strings.Repeat("x", 9000)) {
		t.Fatal("full 9000-char line leaked through untruncated")
	}
}

// A file with no trailing newline is common (many editors, many generated
// files) and its last line must not be silently dropped.
func TestReadSliceFileWithoutTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	os.WriteFile(path, []byte("a\nb\nlast"), 0644)
	ws := newFakeWorkspace(dir)
	tool := ReadTool(ws, testLimits())

	out, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{
		"path": "f.txt", "mode": "slice", "offset": 1, "limit": 10,
	}))
	if err != nil {
		t.Fatalf("Fn: %v", err)
	}
	if !strings.Contains(out, "3\tlast") {
		t.Fatalf("last line without trailing newline missing from output: %q", out)
	}
}

// full must header its output with an approximate token count and number
// every line, so the model can decide up front whether reading the whole file
// is worth the budget.
func TestReadFullHeaderAndNumbering(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	os.WriteFile(path, []byte("a\nb\nc\n"), 0644)
	ws := newFakeWorkspace(dir)
	tool := ReadTool(ws, testLimits())

	out, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{"path": "f.txt", "mode": "full"}))
	if err != nil {
		t.Fatalf("Fn: %v", err)
	}
	if !strings.HasPrefix(out, "[full read: ~") {
		t.Fatalf("missing full-read header: %q", out)
	}
	if !strings.Contains(out, "1\ta") || !strings.Contains(out, "2\tb") || !strings.Contains(out, "3\tc") {
		t.Fatalf("full read did not number every line: %q", out)
	}
}

// A full read over the configured byte cap must refuse rather than load an
// enormous file into memory and context — and it is a result the model can
// react to, not a Go error that aborts the tool call.
func TestReadFullOverMaxBytesRefuses(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	os.WriteFile(path, []byte(strings.Repeat("x", 100)), 0644)
	ws := newFakeWorkspace(dir)
	lim := testLimits()
	lim.ReadMaxBytes = 10
	tool := ReadTool(ws, lim)

	out, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{"path": "f.txt", "mode": "full"}))
	if err != nil {
		t.Fatalf("expected nil error for a refusal result, got: %v", err)
	}
	if !strings.Contains(out, "full read refused") {
		t.Fatalf("out = %q, want a refusal message", out)
	}
}

// A directory path must return a listing regardless of what mode was
// requested — mode only makes sense for a file, and the model should not have
// to know that in advance to read a directory.
func TestReadDirectoryListing(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "sub"), 0755)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0644)
	ws := newFakeWorkspace(dir)
	tool := ReadTool(ws, testLimits())

	for _, mode := range []string{"", "slice", "full", "bogus"} {
		out, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{"path": ".", "mode": mode}))
		if err != nil {
			t.Fatalf("mode=%q: Fn: %v", mode, err)
		}
		if !strings.Contains(out, "sub/") {
			t.Fatalf("mode=%q: dir not listed with trailing slash: %q", mode, out)
		}
		if !strings.Contains(out, "a.txt") {
			t.Fatalf("mode=%q: file not listed: %q", mode, out)
		}
		if !strings.Contains(out, "2 entries") {
			t.Fatalf("mode=%q: missing entry-count header: %q", mode, out)
		}
	}
}

// An empty directory is a distinct, legitimate case from "not found" and must
// say so plainly rather than returning an ambiguous empty string.
func TestReadEmptyDirectoryListing(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty")
	os.MkdirAll(empty, 0755)
	ws := newFakeWorkspace(dir)
	tool := ReadTool(ws, testLimits())

	out, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{"path": "empty"}))
	if err != nil {
		t.Fatalf("Fn: %v", err)
	}
	if !strings.Contains(out, "empty]") {
		t.Fatalf("out = %q, want the empty-directory form", out)
	}
}

// A binary file must be refused before either read mode ever runs, and the
// refusal is a result, not a Go error — the same contract as the full-read
// size cap.
func TestReadBinaryFileRefusedBeforeMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bin.dat")
	os.WriteFile(path, append([]byte("MZ"), 0, 1, 2, 3), 0644)
	ws := newFakeWorkspace(dir)
	tool := ReadTool(ws, testLimits())

	out, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{"path": "bin.dat", "mode": "full"}))
	if err != nil {
		t.Fatalf("expected nil error for a binary refusal, got: %v", err)
	}
	if !strings.Contains(out, "binary file refused") {
		t.Fatalf("out = %q, want a binary refusal", out)
	}
}

// A path that resolves to nothing on disk must error — there is no sensible
// result string for "this file does not exist".
func TestReadNonexistentPathErrors(t *testing.T) {
	ws := newFakeWorkspace(t.TempDir())
	tool := ReadTool(ws, testLimits())

	_, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{"path": "missing.txt"}))
	if err == nil {
		t.Fatal("expected error for a nonexistent path")
	}
}

// A relative path must be resolved through Workspace.ResolvePath — the
// workspace's own notion of "here" — never through the process's actual
// current directory. An agent's workspace and the runtime process's cwd are
// not guaranteed to be the same thing, and if read silently fell back to
// os.Getwd, a workspace-scoped agent would leak reads outside its sandbox.
func TestReadRelativePathResolvedThroughWorkspace(t *testing.T) {
	elsewhere := t.TempDir()
	os.WriteFile(filepath.Join(elsewhere, "f.txt"), []byte("from workspace\n"), 0644)
	ws := newFakeWorkspace(elsewhere)
	tool := ReadTool(ws, testLimits())

	out, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{"path": "f.txt", "mode": "full"}))
	if err != nil {
		t.Fatalf("Fn: %v", err)
	}
	if !strings.Contains(out, "from workspace") {
		t.Fatalf("relative path was not resolved against the workspace dir: %q", out)
	}
}
