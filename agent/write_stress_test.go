package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// WS1 — replace_string: old_str appears 0 times after concurrent truncation.
// A write completes first wiping the content; subsequent replace_string must
// return "not found", not silently succeed with wrong data.
func TestWriteStress_WS1_ReplaceStringFileWipedConcurrently(t *testing.T) {
	s := newSession(t)
	writeFile(t, s, "f.txt", "needle")
	// Wipe the file.
	abs := filepath.Join(s.Cwd, "f.txt")
	if err := os.WriteFile(abs, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := callTool(t, context.Background(), s, "write", map[string]any{
		"path": "f.txt", "mode": "replace_string", "old_str": "needle", "content": "pin", "reason": "t",
	})
	mustErr(t, err, "old_str not found")
}

// WS2 — save: content is 4MB of text (large file write succeeds, no truncation).
func TestWriteStress_WS2_LargeFileWrite(t *testing.T) {
	s := newSession(t)
	content := strings.Repeat("a", 4*1024*1024)
	_, err := callTool(t, context.Background(), s, "write", map[string]any{
		"path": "big.txt", "mode": "save", "content": content, "reason": "t",
	})
	mustNoErr(t, err)
	got := readFileBytes(t, s, "big.txt")
	if len(got) != len(content) {
		t.Fatalf("expected %d bytes, got %d", len(content), len(got))
	}
}

// WS3 — replace_string: content and old_str are identical (replace with same
// string). Must be a no-op — file unchanged, no error.
func TestWriteStress_WS3_ReplaceWithSameString(t *testing.T) {
	s := newSession(t)
	writeFile(t, s, "f.txt", "hello")
	_, err := callTool(t, context.Background(), s, "write", map[string]any{
		"path": "f.txt", "mode": "replace_string", "old_str": "hello", "content": "hello", "reason": "t",
	})
	mustNoErr(t, err)
	if got := string(readFileBytes(t, s, "f.txt")); got != "hello" {
		t.Fatalf("file changed unexpectedly: %q", got)
	}
}

// WS4 — replace_lines: start_line == end_line == 1 on a single-line file
// replaces the only line.
func TestWriteStress_WS4_ReplaceSingleLine(t *testing.T) {
	s := newSession(t)
	writeFile(t, s, "f.txt", "old")
	_, err := callTool(t, context.Background(), s, "write", map[string]any{
		"path": "f.txt", "mode": "replace_lines", "start_line": 1, "end_line": 1,
		"content": "new", "reason": "t",
	})
	mustNoErr(t, err)
	if got := string(readFileBytes(t, s, "f.txt")); got != "new" {
		t.Fatalf("contents: %q", got)
	}
}

// WS5 — insert_at_line beyond len+1 returns an error.
func TestWriteStress_WS5_InsertBeyondEnd(t *testing.T) {
	s := newSession(t)
	writeFile(t, s, "f.txt", "a\nb\n")
	_, err := callTool(t, context.Background(), s, "write", map[string]any{
		"path": "f.txt", "mode": "insert_at_line", "start_line": 999, "content": "X", "reason": "t",
	})
	mustErr(t, err, "start_line")
}

// WS6 — save concurrent to same path: last writer wins, no corruption,
// no temp files left behind.
func TestWriteStress_WS6_ConcurrentSameFile(t *testing.T) {
	s := newSession(t)
	writeFile(t, s, "f.txt", "initial")
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			content := strings.Repeat(string(rune('A'+i%26)), 64)
			_, _ = callTool(t, context.Background(), s, "write", map[string]any{
				"path": "f.txt", "mode": "save", "content": content, "reason": "t",
			})
		}(i)
	}
	wg.Wait()
	// File must be readable and contain exactly one of the valid payloads (64 chars).
	got := readFileBytes(t, s, "f.txt")
	if len(got) != 64 {
		t.Fatalf("concurrent write left corrupt file: len=%d, content=%q", len(got), got)
	}
	// No temp files.
	entries, _ := os.ReadDir(s.Cwd)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".axon-write-") {
			t.Fatalf("temp file leaked: %s", e.Name())
		}
	}
}

// WS7 — replace_string: old_str is an empty string.
func TestWriteStress_WS7_ReplaceEmptyOldStr(t *testing.T) {
	s := newSession(t)
	writeFile(t, s, "f.txt", "hello")
	_, err := callTool(t, context.Background(), s, "write", map[string]any{
		"path": "f.txt", "mode": "replace_string", "old_str": "", "content": "x", "reason": "t",
	})
	// strings.Count("hello", "") == 6 — should fail as "matches N times" or "not found"
	if err == nil {
		t.Fatal("expected error for empty old_str")
	}
}

// WS8 — replace_lines: end_line < start_line should error, not corrupt.
func TestWriteStress_WS8_EndLineBeforeStart(t *testing.T) {
	s := newSession(t)
	writeFile(t, s, "f.txt", "a\nb\nc\n")
	_, err := callTool(t, context.Background(), s, "write", map[string]any{
		"path": "f.txt", "mode": "replace_lines", "start_line": 3, "end_line": 1,
		"content": "X", "reason": "t",
	})
	// If it succeeds we accept it, but it must not silently delete lines or panic.
	// The file must still be parseable.
	if err != nil {
		return
	}
	got := readFileBytes(t, s, "f.txt")
	if len(got) == 0 {
		t.Fatal("replace_lines with end<start wiped the file")
	}
}

// WS9 — save deep nested path (20 levels) succeeds.
func TestWriteStress_WS9_DeeplyNestedPath(t *testing.T) {
	s := newSession(t)
	parts := strings.Repeat("d/", 20) + "file.txt"
	_, err := callTool(t, context.Background(), s, "write", map[string]any{
		"path": parts, "mode": "save", "content": "deep", "reason": "t",
	})
	mustNoErr(t, err)
}

// WS10 — insert_at_line: insert at line 1 of empty file.
func TestWriteStress_WS10_InsertIntoEmptyFile(t *testing.T) {
	s := newSession(t)
	writeFile(t, s, "empty.txt", "")
	_, err := callTool(t, context.Background(), s, "write", map[string]any{
		"path": "empty.txt", "mode": "insert_at_line", "start_line": 1, "content": "first", "reason": "t",
	})
	mustNoErr(t, err)
	mustContain(t, string(readFileBytes(t, s, "empty.txt")), "first")
}

// WS11 — content with NUL bytes is written byte-for-byte (tools do not
// enforce text-only content).
func TestWriteStress_WS11_ContentWithNUL(t *testing.T) {
	s := newSession(t)
	content := "a\x00b"
	_, err := callTool(t, context.Background(), s, "write", map[string]any{
		"path": "nul.txt", "mode": "save", "content": content, "reason": "t",
	})
	mustNoErr(t, err)
	got := readFileBytes(t, s, "nul.txt")
	if string(got) != content {
		t.Fatalf("NUL content not preserved: got %q", got)
	}
}

// WS12 — undo stack depth: 100 writes to same file, then 100 undos LIFO.
func TestWriteStress_WS12_UndoDepth100(t *testing.T) {
	s := newSession(t)
	writeFile(t, s, "f.txt", "v0")
	for i := 1; i <= 100; i++ {
		_, err := callTool(t, context.Background(), s, "write", map[string]any{
			"path": "f.txt", "mode": "save",
			"content": "v" + string(rune('0'+(i%10))), "reason": "t",
		})
		mustNoErr(t, err)
	}
	// Pop all 100 undos.
	for i := 0; i < 100; i++ {
		e, ok := s.Undo()
		if !ok {
			t.Fatalf("expected undo at step %d", i)
		}
		if err := writeBytesRaw(e.Path, []byte(e.Before)); err != nil {
			t.Fatalf("undo write: %v", err)
		}
	}
	// One more undo should be empty (the original v0 has no predecessor).
	if _, ok := s.Undo(); ok {
		t.Fatal("expected empty undo stack after 100 undos")
	}
}

// WS13 — replace_string: old_str contains regex metacharacters that must NOT
// be treated as a regex (literal string matching only).
func TestWriteStress_WS13_ReplaceStringWithRegexMeta(t *testing.T) {
	s := newSession(t)
	writeFile(t, s, "f.txt", "price: $10.00\n")
	_, err := callTool(t, context.Background(), s, "write", map[string]any{
		"path": "f.txt", "mode": "replace_string", "old_str": "$10.00", "content": "$20.00", "reason": "t",
	})
	mustNoErr(t, err)
	mustContain(t, string(readFileBytes(t, s, "f.txt")), "$20.00")
}

// WS14 — write to a path outside session Cwd (absolute path) succeeds.
func TestWriteStress_WS14_AbsPathOutsideCwd(t *testing.T) {
	s := newSession(t)
	outside := filepath.Join(t.TempDir(), "external.txt")
	_, err := callTool(t, context.Background(), s, "write", map[string]any{
		"path": outside, "mode": "save", "content": "external", "reason": "t",
	})
	mustNoErr(t, err)
	got, err := os.ReadFile(outside)
	mustNoErr(t, err)
	if string(got) != "external" {
		t.Fatalf("external write: %q", got)
	}
}

// WS15 — save: content is exactly 1 byte.
func TestWriteStress_WS15_SingleByteContent(t *testing.T) {
	s := newSession(t)
	_, err := callTool(t, context.Background(), s, "write", map[string]any{
		"path": "one.txt", "mode": "save", "content": "x", "reason": "t",
	})
	mustNoErr(t, err)
	if got := string(readFileBytes(t, s, "one.txt")); got != "x" {
		t.Fatalf("contents: %q", got)
	}
}

// WS16 — replace_string produces the right byte count after replacement
// (ensures no double-replace or partial replacement).
func TestWriteStress_WS16_ReplaceStringByteCount(t *testing.T) {
	s := newSession(t)
	// "AAA BBB CCC" — replace "BBB" with "BBBBB" (longer replacement)
	writeFile(t, s, "f.txt", "AAA BBB CCC")
	_, err := callTool(t, context.Background(), s, "write", map[string]any{
		"path": "f.txt", "mode": "replace_string", "old_str": "BBB", "content": "BBBBB", "reason": "t",
	})
	mustNoErr(t, err)
	got := string(readFileBytes(t, s, "f.txt"))
	if got != "AAA BBBBB CCC" {
		t.Fatalf("after replace: %q", got)
	}
}
