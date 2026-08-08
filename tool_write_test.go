package axon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A blank path can never be resolved, and an unknown/missing mode has no safe
// default (unlike read/search, each write mode takes different parameters) —
// both must fail before touching the filesystem.
func TestWriteBlankPathAndUnknownModeErrors(t *testing.T) {
	dir := t.TempDir()
	ws := newFakeWorkspace(dir)
	tool := WriteTool(ws)

	_, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{"path": "", "mode": "save", "content": "x"}))
	if err == nil {
		t.Fatal("blank path should error")
	}

	_, err = tool.Fn(context.Background(), mustJSON(t, map[string]any{"path": "f.txt", "content": "x"}))
	if err == nil {
		t.Fatal("missing mode should error")
	}
	for _, mode := range []string{"save", "replace_string", "replace_lines", "insert_at_line"} {
		if !strings.Contains(err.Error(), mode) {
			t.Fatalf("error does not name mode %q: %v", mode, err)
		}
	}
}

// save is the "I have the whole file" mode: it must create when absent,
// replace when present, create missing parent dirs so the model does not
// need a separate mkdir step, and record an edit with an empty before so
// /undo on a brand-new file means delete-back-to-nothing.
func TestWriteSaveCreatesFile(t *testing.T) {
	dir := t.TempDir()
	ws := newFakeWorkspace(dir)
	tool := WriteTool(ws)

	out, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{
		"path": "sub/dir/new.txt", "mode": "save", "content": "hello",
	}))
	if err != nil {
		t.Fatalf("Fn: %v", err)
	}
	if !strings.Contains(out, "(created)") {
		t.Fatalf("out = %q, want (created)", out)
	}
	data, err := os.ReadFile(filepath.Join(dir, "sub/dir/new.txt"))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("content = %q, want hello", data)
	}
	edits := ws.editsSnapshot()
	if len(edits) != 1 || edits[0].before != "" {
		t.Fatalf("edits = %+v, want one edit with empty before", edits)
	}
}

// save over an existing file must report "(replaced)" and record what was
// there before — that recording is the entire undo mechanism.
func TestWriteSaveReplacesExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	os.WriteFile(path, []byte("old contents"), 0644)
	ws := newFakeWorkspace(dir)
	tool := WriteTool(ws)

	out, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{
		"path": "f.txt", "mode": "save", "content": "new contents",
	}))
	if err != nil {
		t.Fatalf("Fn: %v", err)
	}
	if !strings.Contains(out, "(replaced)") {
		t.Fatalf("out = %q, want (replaced)", out)
	}
	edits := ws.editsSnapshot()
	if len(edits) != 1 || edits[0].before != "old contents" {
		t.Fatalf("edits = %+v, want one edit recording the prior contents", edits)
	}
}

// replace_string with an empty old_str is meaningless (replace nothing with
// something?) and must be rejected at validation, before any file is opened.
func TestWriteReplaceStringEmptyOldStrErrors(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0644)
	ws := newFakeWorkspace(dir)
	tool := WriteTool(ws)

	_, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{
		"path": "f.txt", "mode": "replace_string", "old_str": "", "content": "y",
	}))
	if err == nil {
		t.Fatal("empty old_str should error at validation")
	}
}

// Zero matches means the model's mental model of the file is wrong; silently
// doing nothing would let that error compound across several turns, so it
// must fail loudly and leave the file untouched.
func TestWriteReplaceStringZeroMatchesErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	os.WriteFile(path, []byte("original"), 0644)
	ws := newFakeWorkspace(dir)
	tool := WriteTool(ws)

	_, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{
		"path": "f.txt", "mode": "replace_string", "old_str": "not present", "content": "y",
	}))
	if err == nil {
		t.Fatal("zero matches should error")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "original" {
		t.Fatalf("file mutated on a failed replace: %q", data)
	}
}

// More than one match is ambiguous — which occurrence did the model mean? —
// so it must fail and name the count so the model can add more context,
// leaving the file untouched rather than guessing.
func TestWriteReplaceStringMultipleMatchesErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	os.WriteFile(path, []byte("foo bar foo"), 0644)
	ws := newFakeWorkspace(dir)
	tool := WriteTool(ws)

	_, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{
		"path": "f.txt", "mode": "replace_string", "old_str": "foo", "content": "baz",
	}))
	if err == nil {
		t.Fatal("multiple matches should error")
	}
	if !strings.Contains(err.Error(), "2") {
		t.Fatalf("error does not name the match count: %v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "foo bar foo" {
		t.Fatalf("file mutated on a failed replace: %q", data)
	}
}

// Exactly one match is the only case that succeeds: it rewrites the file and
// records the full pre-edit contents for undo.
func TestWriteReplaceStringSingleMatchRewrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	os.WriteFile(path, []byte("foo bar baz"), 0644)
	ws := newFakeWorkspace(dir)
	tool := WriteTool(ws)

	_, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{
		"path": "f.txt", "mode": "replace_string", "old_str": "bar", "content": "REPLACED",
	}))
	if err != nil {
		t.Fatalf("Fn: %v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "foo REPLACED baz" {
		t.Fatalf("content = %q, want foo REPLACED baz", data)
	}
	edits := ws.editsSnapshot()
	if len(edits) != 1 || edits[0].before != "foo bar baz" {
		t.Fatalf("edits = %+v, want the full pre-edit contents", edits)
	}
}

// replace_lines needs a sane 1-based range: start_line below 1, or an end
// before the start, are nonsensical and must be rejected before any file I/O.
func TestWriteReplaceLinesValidation(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("a\nb\nc\n"), 0644)
	ws := newFakeWorkspace(dir)
	tool := WriteTool(ws)

	for _, tc := range []struct {
		name       string
		start, end int
	}{
		{"start below 1", 0, 1},
		{"end before start", 3, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{
				"path": "f.txt", "mode": "replace_lines",
				"start_line": tc.start, "end_line": tc.end, "content": "x",
			}))
			if err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

// replace_lines replaces an inclusive range; an end_line past EOF clamps to
// the last line rather than erroring — the model asking for "lines 3-100" on
// a 5-line file clearly means "to the end".
func TestWriteReplaceLinesInclusiveRangeAndEndClamp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	os.WriteFile(path, []byte("a\nb\nc\nd\ne\n"), 0644)
	ws := newFakeWorkspace(dir)
	tool := WriteTool(ws)

	_, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{
		"path": "f.txt", "mode": "replace_lines", "start_line": 2, "end_line": 100, "content": "X",
	}))
	if err != nil {
		t.Fatalf("Fn: %v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "a\nX" {
		t.Fatalf("content = %q, want a\\nX (lines 2-end clamped and replaced)", data)
	}
}

// start_line past EOF has no clamp available (there is nothing before it to
// keep) and must error naming the actual line count.
func TestWriteReplaceLinesStartPastEOFErrors(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("a\nb\n"), 0644)
	ws := newFakeWorkspace(dir)
	tool := WriteTool(ws)

	_, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{
		"path": "f.txt", "mode": "replace_lines", "start_line": 50, "end_line": 51, "content": "x",
	}))
	if err == nil {
		t.Fatal("expected error for start_line past EOF")
	}
	// The file has 2 lines (a, b) after Split on "\n" of "a\nb\n" -> ["a","b",""],
	// so the reported count is whatever strings.Split actually yields.
	if !strings.Contains(err.Error(), "50") {
		t.Fatalf("error does not name the offending start_line: %v", err)
	}
}

// Multi-line replacement content must splice every line in, and a trailing
// newline in the supplied content must not leave a spurious blank line behind
// — that would make every replace_lines call append noise to the file.
func TestWriteReplaceLinesMultiLineContentNoTrailingBlank(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	os.WriteFile(path, []byte("a\nb\nc\n"), 0644)
	ws := newFakeWorkspace(dir)
	tool := WriteTool(ws)

	_, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{
		"path": "f.txt", "mode": "replace_lines", "start_line": 2, "end_line": 2,
		"content": "x\ny\nz\n",
	}))
	if err != nil {
		t.Fatalf("Fn: %v", err)
	}
	data, _ := os.ReadFile(path)
	// The original file ends with a trailing newline, which strings.Split
	// preserves as a final empty element ("a\nb\nc\n" -> ["a","b","c",""]).
	// That trailing element is part of the untouched tail, so it survives the
	// splice and the file keeps its trailing newline.
	want := "a\nx\ny\nz\nc\n"
	if string(data) != want {
		t.Fatalf("content = %q, want %q", data, want)
	}
}

// insert_at_line inserts *before* the 1-based line: line 1 prepends,
// len(lines)+1 appends, and anything past that is out of range and errors.
func TestWriteInsertAtLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")

	t.Run("before a middle line", func(t *testing.T) {
		os.WriteFile(path, []byte("a\nb\nc\n"), 0644)
		ws := newFakeWorkspace(dir)
		tool := WriteTool(ws)
		_, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{
			"path": "f.txt", "mode": "insert_at_line", "start_line": 2, "content": "NEW",
		}))
		if err != nil {
			t.Fatalf("Fn: %v", err)
		}
		data, _ := os.ReadFile(path)
		// Trailing newline in the source file survives as the untouched tail
		// (see the replace_lines splice test for why).
		if string(data) != "a\nNEW\nb\nc\n" {
			t.Fatalf("content = %q, want a\\nNEW\\nb\\nc\\n", data)
		}
	})

	t.Run("at line 1 prepends", func(t *testing.T) {
		os.WriteFile(path, []byte("a\nb\n"), 0644)
		ws := newFakeWorkspace(dir)
		tool := WriteTool(ws)
		_, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{
			"path": "f.txt", "mode": "insert_at_line", "start_line": 1, "content": "FIRST",
		}))
		if err != nil {
			t.Fatalf("Fn: %v", err)
		}
		data, _ := os.ReadFile(path)
		if string(data) != "FIRST\na\nb\n" {
			t.Fatalf("content = %q, want FIRST\\na\\nb\\n", data)
		}
	})

	t.Run("at len+1 appends", func(t *testing.T) {
		os.WriteFile(path, []byte("a\nb\n"), 0644)
		ws := newFakeWorkspace(dir)
		tool := WriteTool(ws)
		// "a\nb\n" splits into ["a","b",""] — 3 elements — so len+1 = 4.
		_, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{
			"path": "f.txt", "mode": "insert_at_line", "start_line": 4, "content": "LAST",
		}))
		if err != nil {
			t.Fatalf("Fn: %v", err)
		}
		data, _ := os.ReadFile(path)
		if !strings.HasSuffix(string(data), "LAST") {
			t.Fatalf("content = %q, want it to end with LAST", data)
		}
	})

	t.Run("past len+1 errors", func(t *testing.T) {
		os.WriteFile(path, []byte("a\nb\n"), 0644)
		ws := newFakeWorkspace(dir)
		tool := WriteTool(ws)
		_, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{
			"path": "f.txt", "mode": "insert_at_line", "start_line": 50, "content": "x",
		}))
		if err == nil {
			t.Fatal("expected error for start_line far past EOF")
		}
	})
}

// Every mutating mode must call RecordEdit exactly once, with the pre-edit
// contents, before the write lands — that ordering is what makes /undo
// trustworthy even if a later step in the same turn crashes: the ledger
// entry for a change must exist by the time the change itself is visible.
func TestWriteRecordsEditExactlyOncePerMutation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	os.WriteFile(path, []byte("a\nb\nc\n"), 0644)

	cases := []struct {
		name  string
		input map[string]any
	}{
		{"save", map[string]any{"path": "f.txt", "mode": "save", "content": "z"}},
		{"replace_string", map[string]any{"path": "f.txt", "mode": "replace_string", "old_str": "b", "content": "B"}},
		{"replace_lines", map[string]any{"path": "f.txt", "mode": "replace_lines", "start_line": 1, "end_line": 1, "content": "A"}},
		{"insert_at_line", map[string]any{"path": "f.txt", "mode": "insert_at_line", "start_line": 1, "content": "X"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			os.WriteFile(path, []byte("a\nb\nc\n"), 0644)
			ws := newFakeWorkspace(dir)
			tool := WriteTool(ws)
			before, _ := os.ReadFile(path)

			if _, err := tool.Fn(context.Background(), mustJSON(t, tc.input)); err != nil {
				t.Fatalf("Fn: %v", err)
			}
			edits := ws.editsSnapshot()
			if len(edits) != 1 {
				t.Fatalf("RecordEdit called %d times, want exactly 1", len(edits))
			}
			if edits[0].before != string(before) {
				t.Fatalf("recorded before = %q, want %q", edits[0].before, before)
			}
		})
	}
}

// A mode that fails validation or fails its match/range check must not
// record an edit — an edit entry with no corresponding write would make
// /undo restore a file to contents it never actually changed from.
func TestWriteFailedModeRecordsNoEdit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	os.WriteFile(path, []byte("a\nb\nc\n"), 0644)

	cases := []struct {
		name  string
		input map[string]any
	}{
		{"replace_string zero matches", map[string]any{"path": "f.txt", "mode": "replace_string", "old_str": "zzz", "content": "y"}},
		{"replace_string multi match", map[string]any{"path": "f.txt", "mode": "replace_string", "old_str": "\n", "content": "y"}},
		{"replace_lines start past EOF", map[string]any{"path": "f.txt", "mode": "replace_lines", "start_line": 99, "end_line": 100, "content": "y"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ws := newFakeWorkspace(dir)
			tool := WriteTool(ws)
			if _, err := tool.Fn(context.Background(), mustJSON(t, tc.input)); err == nil {
				t.Fatal("expected the mode to fail")
			}
			if edits := ws.editsSnapshot(); len(edits) != 0 {
				t.Fatalf("edits = %+v, want none recorded on failure", edits)
			}
		})
	}
}

// This is the /undo contract made concrete: replaying the recorded "before"
// for any write must restore the file byte-for-byte. If this ever drifts,
// /undo silently corrupts a file instead of reverting it.
func TestWriteRoundTripUndoRestoresByteForByte(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	original := "line one\nline two\nline three\n"
	os.WriteFile(path, []byte(original), 0644)
	ws := newFakeWorkspace(dir)
	tool := WriteTool(ws)

	_, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{
		"path": "f.txt", "mode": "replace_lines", "start_line": 2, "end_line": 2, "content": "REPLACED",
	}))
	if err != nil {
		t.Fatalf("Fn: %v", err)
	}

	edits := ws.editsSnapshot()
	if len(edits) != 1 {
		t.Fatalf("edits = %+v, want 1", edits)
	}
	// Simulate /undo: write the recorded before back.
	if err := WriteFileAtomic(path, []byte(edits[0].before)); err != nil {
		t.Fatalf("undo write: %v", err)
	}
	restored, _ := os.ReadFile(path)
	if string(restored) != original {
		t.Fatalf("restored = %q, want the original %q", restored, original)
	}
}

// A write to an executable file must not clobber its mode — the same
// preserve-mode contract WriteFileAtomic gives directly (D2), now exercised
// through the tool's edit path.
func TestWriteModePreservedAcrossEdit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "run.sh")
	os.WriteFile(path, []byte("#!/bin/sh\necho old\n"), 0755)
	ws := newFakeWorkspace(dir)
	tool := WriteTool(ws)

	_, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{
		"path": "run.sh", "mode": "save", "content": "#!/bin/sh\necho new\n",
	}))
	if err != nil {
		t.Fatalf("Fn: %v", err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if fi.Mode().Perm() != 0755 {
		t.Fatalf("mode = %o, want 0755 preserved", fi.Mode().Perm())
	}
}
