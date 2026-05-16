package agent

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// W1 — save creates a new file and reports (created).
func TestWrite_W1_SaveCreates(t *testing.T) {
	s := newSession(t)
	out, err := callTool(t, context.Background(), s, "write", map[string]any{
		"path": "new.txt", "mode": "save", "content": "hello\n", "reason": "test",
	})
	mustNoErr(t, err)
	mustContain(t, out, "(created)")
	if got := readFileBytes(t, s, "new.txt"); string(got) != "hello\n" {
		t.Fatalf("contents: %q", got)
	}
}

// W2 — save overwrites an existing file and reports (replaced).
func TestWrite_W2_SaveOverwrites(t *testing.T) {
	s := newSession(t)
	writeFile(t, s, "old.txt", "original")
	out, err := callTool(t, context.Background(), s, "write", map[string]any{
		"path": "old.txt", "mode": "save", "content": "new", "reason": "test",
	})
	mustNoErr(t, err)
	mustContain(t, out, "(replaced)")
	if got := readFileBytes(t, s, "old.txt"); string(got) != "new" {
		t.Fatalf("contents: %q", got)
	}
}

// W3 — save preserves the executable bit on existing files.
func TestWrite_W3_SavePreservesExecBit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix mode bits")
	}
	s := newSession(t)
	writeFileMode(t, s, "script.sh", "#!/bin/sh\necho one\n", 0755)
	_, err := callTool(t, context.Background(), s, "write", map[string]any{
		"path": "script.sh", "mode": "save", "content": "#!/bin/sh\necho two\n", "reason": "test",
	})
	mustNoErr(t, err)
	if got := fileMode(t, s, "script.sh"); got != 0755 {
		t.Fatalf("expected mode 0755 preserved, got %v", got)
	}
}

// W4 — save creates parent directories.
func TestWrite_W4_SaveCreatesParents(t *testing.T) {
	s := newSession(t)
	_, err := callTool(t, context.Background(), s, "write", map[string]any{
		"path": "a/b/c/x.txt", "mode": "save", "content": "hi", "reason": "test",
	})
	mustNoErr(t, err)
	if got := readFileBytes(t, s, "a/b/c/x.txt"); string(got) != "hi" {
		t.Fatalf("contents: %q", got)
	}
}

// W5 — replace_string with multiple matches errors and leaves the file alone.
func TestWrite_W5_ReplaceStringAmbiguous(t *testing.T) {
	s := newSession(t)
	writeFile(t, s, "f.txt", "foo foo")
	_, err := callTool(t, context.Background(), s, "write", map[string]any{
		"path": "f.txt", "mode": "replace_string", "old_str": "foo", "content": "bar", "reason": "test",
	})
	mustErr(t, err, "matches 2 times")
	if got := readFileBytes(t, s, "f.txt"); string(got) != "foo foo" {
		t.Fatalf("file should be unchanged, got %q", got)
	}
}

// W6 — replace_string not found errors and leaves the file alone.
func TestWrite_W6_ReplaceStringNotFound(t *testing.T) {
	s := newSession(t)
	writeFile(t, s, "f.txt", "abc")
	_, err := callTool(t, context.Background(), s, "write", map[string]any{
		"path": "f.txt", "mode": "replace_string", "old_str": "xyz", "content": "q", "reason": "test",
	})
	mustErr(t, err, "old_str not found")
	if got := readFileBytes(t, s, "f.txt"); string(got) != "abc" {
		t.Fatalf("file should be unchanged, got %q", got)
	}
}

// W7 — replace_string exact-once.
func TestWrite_W7_ReplaceStringExact(t *testing.T) {
	s := newSession(t)
	writeFile(t, s, "f.txt", "alpha beta gamma")
	_, err := callTool(t, context.Background(), s, "write", map[string]any{
		"path": "f.txt", "mode": "replace_string", "old_str": "beta", "content": "BETA", "reason": "test",
	})
	mustNoErr(t, err)
	if got := readFileBytes(t, s, "f.txt"); string(got) != "alpha BETA gamma" {
		t.Fatalf("contents: %q", got)
	}
}

// W8 — replace_lines with end past EOF clamps to file length.
func TestWrite_W8_ReplaceLinesEndPastEOF(t *testing.T) {
	s := newSession(t)
	writeFile(t, s, "f.txt", "a\nb\nc\n")
	_, err := callTool(t, context.Background(), s, "write", map[string]any{
		"path": "f.txt", "mode": "replace_lines", "start_line": 2, "end_line": 999, "content": "X", "reason": "test",
	})
	mustNoErr(t, err)
	if got := readFileBytes(t, s, "f.txt"); string(got) != "a\nX" {
		t.Fatalf("contents: %q", got)
	}
}

// W9 — replace_lines with start past EOF errors.
func TestWrite_W9_ReplaceLinesStartPastEOF(t *testing.T) {
	s := newSession(t)
	writeFile(t, s, "f.txt", "a\nb\nc\n")
	_, err := callTool(t, context.Background(), s, "write", map[string]any{
		"path": "f.txt", "mode": "replace_lines", "start_line": 10, "end_line": 20, "content": "X", "reason": "test",
	})
	mustErr(t, err, "past end of file")
}

// W10 — insert_at_line at line 1 prepends.
func TestWrite_W10_InsertAtLine1(t *testing.T) {
	s := newSession(t)
	writeFile(t, s, "f.txt", "a\nb\n")
	_, err := callTool(t, context.Background(), s, "write", map[string]any{
		"path": "f.txt", "mode": "insert_at_line", "start_line": 1, "content": "HEAD", "reason": "test",
	})
	mustNoErr(t, err)
	got := string(readFileBytes(t, s, "f.txt"))
	if got != "HEAD\na\nb\n" {
		t.Fatalf("contents: %q", got)
	}
}

// W11 — insert_at_line at len+1 appends.
func TestWrite_W11_InsertAtLenPlusOne(t *testing.T) {
	s := newSession(t)
	writeFile(t, s, "f.txt", "a\nb")
	_, err := callTool(t, context.Background(), s, "write", map[string]any{
		"path": "f.txt", "mode": "insert_at_line", "start_line": 3, "content": "TAIL", "reason": "test",
	})
	mustNoErr(t, err)
	got := string(readFileBytes(t, s, "f.txt"))
	mustContain(t, got, "TAIL")
}

// W14 — /undo restores byte-exact content (including trailing whitespace) AND mode.
// Bypasses the slash-handler: directly exercises the same writeBytesRaw path the
// /undo command uses, which is what we changed.
func TestWrite_W14_UndoByteExact(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix mode bits")
	}
	s := newSession(t)
	original := "#!/bin/sh   \nold  \n"
	writeFileMode(t, s, "script.sh", original, 0755)

	_, err := callTool(t, context.Background(), s, "write", map[string]any{
		"path": "script.sh", "mode": "save", "content": "#!/bin/sh\nnew\n", "reason": "test",
	})
	mustNoErr(t, err)

	// Replay /undo path: pop edit + writeBytesRaw.
	e, ok := s.Undo()
	if !ok {
		t.Fatal("expected an edit to undo")
	}
	if err := writeBytesRaw(e.Path, []byte(e.Before)); err != nil {
		t.Fatalf("undo write: %v", err)
	}

	got := readFileBytes(t, s, "script.sh")
	if !bytes.Equal(got, []byte(original)) {
		t.Fatalf("undo not byte-exact:\nwant %q\n got %q", original, got)
	}
	if mode := fileMode(t, s, "script.sh"); mode != 0755 {
		t.Fatalf("undo did not preserve 0755 mode, got %v", mode)
	}
}

// W15 — write to a 0444 file: succeeds because tmp+rename only needs the
// directory writable. Documents real behavior.
func TestWrite_W15_WriteToReadOnlyFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix mode bits")
	}
	s := newSession(t)
	writeFileMode(t, s, "ro.txt", "old", 0444)
	_, err := callTool(t, context.Background(), s, "write", map[string]any{
		"path": "ro.txt", "mode": "save", "content": "new", "reason": "test",
	})
	if err != nil {
		t.Fatalf("documented behavior: tmp+rename succeeds on 0444 file when dir is writable; got error: %v", err)
	}
	if got := string(readFileBytes(t, s, "ro.txt")); got != "new" {
		t.Fatalf("contents: %q", got)
	}
	// And the mode is preserved.
	if mode := fileMode(t, s, "ro.txt"); mode != 0444 {
		t.Fatalf("expected 0444 preserved, got %v", mode)
	}
}

// W16 — empty content save creates a 0-byte file.
func TestWrite_W16_EmptyContent(t *testing.T) {
	s := newSession(t)
	_, err := callTool(t, context.Background(), s, "write", map[string]any{
		"path": "empty.txt", "mode": "save", "content": "", "reason": "test",
	})
	mustNoErr(t, err)
	info, err := os.Stat(filepath.Join(s.Cwd, "empty.txt"))
	mustNoErr(t, err)
	if info.Size() != 0 {
		t.Fatalf("expected size 0, got %d", info.Size())
	}
}

// W17 — replace_lines preserves the rest of the file (no \n eating).
func TestWrite_W17_ReplaceLinesPreservesTail(t *testing.T) {
	s := newSession(t)
	writeFile(t, s, "f.txt", "a\nb\nc")
	_, err := callTool(t, context.Background(), s, "write", map[string]any{
		"path": "f.txt", "mode": "replace_lines", "start_line": 2, "end_line": 2, "content": "B", "reason": "test",
	})
	mustNoErr(t, err)
	got := string(readFileBytes(t, s, "f.txt"))
	if got != "a\nB\nc" {
		t.Fatalf("contents: %q", got)
	}
}

// W18 — replace_string with multi-line old_str.
func TestWrite_W18_ReplaceStringMultiline(t *testing.T) {
	s := newSession(t)
	writeFile(t, s, "f.txt", "line1\nline2\nline3\n")
	_, err := callTool(t, context.Background(), s, "write", map[string]any{
		"path": "f.txt", "mode": "replace_string", "old_str": "line1\nline2", "content": "X", "reason": "test",
	})
	mustNoErr(t, err)
	got := string(readFileBytes(t, s, "f.txt"))
	if got != "X\nline3\n" {
		t.Fatalf("contents: %q", got)
	}
}

// W19a — replace_string missing old_str.
func TestWrite_W19a_MissingOldStr(t *testing.T) {
	s := newSession(t)
	writeFile(t, s, "f.txt", "abc")
	_, err := callTool(t, context.Background(), s, "write", map[string]any{
		"path": "f.txt", "mode": "replace_string", "content": "x", "reason": "test",
	})
	mustErr(t, err, "old_str is required")
}

// W19b — replace_lines with start_line=0.
func TestWrite_W19b_StartLineZero(t *testing.T) {
	s := newSession(t)
	writeFile(t, s, "f.txt", "abc\n")
	_, err := callTool(t, context.Background(), s, "write", map[string]any{
		"path": "f.txt", "mode": "replace_lines", "start_line": 0, "end_line": 5, "content": "x", "reason": "test",
	})
	mustErr(t, err, "start_line")
}

// X7 — temp file cleanup on rename failure: when the directory is read-only
// the write fails. After failure, no .axon-write-* files should remain.
func TestWrite_X7_TempCleanupOnRenameFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix mode bits")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory write perms")
	}
	s := newSession(t)
	roDir := filepath.Join(s.Cwd, "ro")
	if err := os.Mkdir(roDir, 0555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(roDir, 0755) })

	_, err := callTool(t, context.Background(), s, "write", map[string]any{
		"path": "ro/x.txt", "mode": "save", "content": "x", "reason": "test",
	})
	if err == nil {
		t.Fatal("expected error writing into a 0555 dir")
	}

	entries, _ := os.ReadDir(roDir)
	for _, e := range entries {
		if len(e.Name()) >= len(".axon-write-") && e.Name()[:len(".axon-write-")] == ".axon-write-" {
			t.Fatalf("temp file leaked: %s", e.Name())
		}
	}
}
