package main

// tools_unit_test.go covers deterministic tool-layer behavior:
// schema construction, helper contracts, and the read/write/search/exec tool
// implementations in isolation from the full agent loop.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSchemaHelpersComprehensive(t *testing.T) {
	gotObj := obj("object", props{
		"name": strSchema("person name"),
		"age":  intSchema("person age"),
	}, []string{"name"})
	wantObj := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"name": map[string]any{"type": "string", "description": "person name"},
			"age":  map[string]any{"type": "integer", "description": "person age"},
		},
		"required": []string{"name"},
	}
	if !reflect.DeepEqual(gotObj, wantObj) {
		t.Fatalf("obj mismatch:\n got: %#v\nwant: %#v", gotObj, wantObj)
	}

	gotArr := arr(strSchema("line"))
	wantArr := map[string]any{
		"type":  "array",
		"items": map[string]any{"type": "string", "description": "line"},
	}
	if !reflect.DeepEqual(gotArr, wantArr) {
		t.Fatalf("arr mismatch:\n got: %#v\nwant: %#v", gotArr, wantArr)
	}

	if got := strSchema("s"); !reflect.DeepEqual(got, map[string]any{"type": "string", "description": "s"}) {
		t.Fatalf("strSchema mismatch: %#v", got)
	}
	if got := intSchema("i"); !reflect.DeepEqual(got, map[string]any{"type": "integer", "description": "i"}) {
		t.Fatalf("intSchema mismatch: %#v", got)
	}

	gotEnum := enumSchema("mode", "one", "two")
	wantEnum := map[string]any{
		"type":        "string",
		"description": "mode",
		"enum":        []any{"one", "two"},
	}
	if !reflect.DeepEqual(gotEnum, wantEnum) {
		t.Fatalf("enumSchema mismatch:\n got: %#v\nwant: %#v", gotEnum, wantEnum)
	}
}

func TestReasonAndNextStepHelpers(t *testing.T) {
	if err := requireReason(""); err == nil {
		t.Fatal("empty reason should fail")
	}
	if err := requireReason("   "); err == nil {
		t.Fatal("whitespace reason should fail")
	}
	if err := requireReason("inspect file structure"); err != nil {
		t.Fatalf("valid reason should pass: %v", err)
	}

	if got := reasonField(); got["type"] != "string" {
		t.Fatalf("reasonField should be string schema: %#v", got)
	}
	if got := nextStepField(); got["type"] != "string" {
		t.Fatalf("nextStepField should be string schema: %#v", got)
	}

	cases := []struct {
		name string
		tool string
		raw  string
		want bool
	}{
		{"park end", toolPark, `{"next_step":"end"}`, true},
		{"park continue", toolPark, `{"next_step":"continue"}`, false},
		{"park whitespace", toolPark, `{"next_step":" end "}`, true},
		{"park missing", toolPark, `{}`, false},
		{"park malformed", toolPark, `{`, false},
		{"recall end", toolRecall, `{"next_step":"end"}`, true},
		{"forget end", toolForget, `{"next_step":"end"}`, true},
		{"refresh end", toolRefresh, `{"next_step":"end"}`, true},
		{"non memory tool ignored", toolRead, `{"next_step":"end"}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldEndTurnAfterTool(tc.tool, json.RawMessage(tc.raw)); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestBuildToolsAndSchemasHolistic(t *testing.T) {
	s := tmpSession(t)
	tools := BuildTools(s)
	if len(tools) != 9 {
		t.Fatalf("expected 9 tools, got %d", len(tools))
	}

	seen := map[string]bool{}
	for _, tool := range tools {
		if tool.Name == "" {
			t.Fatal("tool name should not be empty")
		}
		if seen[tool.Name] {
			t.Fatalf("duplicate tool name %q", tool.Name)
		}
		seen[tool.Name] = true
		if strings.TrimSpace(tool.Description) == "" {
			t.Fatalf("tool %q should have description", tool.Name)
		}
		if tool.Schema == nil {
			t.Fatalf("tool %q should have schema", tool.Name)
		}
		if tool.Fn == nil {
			t.Fatalf("tool %q should have function", tool.Name)
		}
		if tool.Schema["type"] != "object" {
			t.Fatalf("tool %q should expose object schema, got %#v", tool.Name, tool.Schema["type"])
		}
		if tool.Schema["additionalProperties"] != false {
			t.Fatalf("tool %q should disable additionalProperties", tool.Name)
		}
	}

	readSchema := ReadTool(s).Schema
	readProps := readSchema["properties"].(map[string]any)
	if _, ok := readProps["reason"]; !ok {
		t.Fatal("read schema should expose reason")
	}
	if _, ok := readProps["mode"]; !ok {
		t.Fatal("read schema should expose mode")
	}

	writeSchema := WriteTool(s).Schema
	writeProps := writeSchema["properties"].(map[string]any)
	if _, ok := writeProps["start_line"]; !ok {
		t.Fatal("write schema should expose start_line")
	}
	if _, ok := writeProps["old_str"]; !ok {
		t.Fatal("write schema should expose old_str")
	}

	searchSchema := SearchTool(s).Schema
	searchProps := searchSchema["properties"].(map[string]any)
	if _, ok := searchProps["globs"]; !ok {
		t.Fatal("search schema should expose globs")
	}

	execSchema := ExecTool(s).Schema
	execProps := execSchema["properties"].(map[string]any)
	if _, ok := execProps["tail_lines"]; !ok {
		t.Fatal("exec schema should expose tail_lines")
	}
}

func TestWriteBytesAndLimitBuf(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "out.txt")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := writeBytes(path, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Fatalf("got %q", data)
	}

	var b limitBuf
	b.limit = 5
	n, err := b.Write([]byte("abc"))
	if err != nil || n != 3 || b.buf.String() != "abc" || b.truncated {
		t.Fatalf("first write bad: n=%d err=%v buf=%q truncated=%v", n, err, b.buf.String(), b.truncated)
	}
	n, err = b.Write([]byte("defgh"))
	if err != nil {
		t.Fatal(err)
	}
	if n != 5 {
		t.Fatalf("limitBuf should report full input length written, got %d", n)
	}
	if b.buf.String() != "abcde" {
		t.Fatalf("unexpected stored bytes: %q", b.buf.String())
	}
	if !b.truncated {
		t.Fatal("limitBuf should mark truncation")
	}
}

func TestReadToolComprehensive(t *testing.T) {
	s := tmpSession(t)
	path := filepath.Join(s.Cwd, "sample.go")
	mustWriteFile(t, path, strings.Join([]string{
		"package main",
		"",
		"import \"fmt\"",
		"",
		"type Person struct{}",
		"",
		"func Hello() {",
		"\tfmt.Println(\"hello\")",
		"}",
		"",
		"func World() {}",
	}, "\n"))
	tool := ReadTool(s)

	t.Run("requires path", func(t *testing.T) {
		_, err := tool.Fn(json.RawMessage(`{"mode":"full","reason":"missing path"}`))
		if err == nil || !strings.Contains(err.Error(), "path is required") {
			t.Fatalf("expected path error, got %v", err)
		}
	})

	t.Run("requires reason", func(t *testing.T) {
		_, err := tool.Fn(json.RawMessage(`{"path":"sample.go","mode":"full"}`))
		if err == nil || !strings.Contains(err.Error(), "reason is required") {
			t.Fatalf("expected reason error, got %v", err)
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		if _, err := tool.Fn(json.RawMessage(`{`)); err == nil {
			t.Fatal("expected invalid json error")
		}
	})

	t.Run("rejects invalid mode", func(t *testing.T) {
		_, err := tool.Fn(json.RawMessage(`{"path":"sample.go","mode":"weird","reason":"bad mode"}`))
		if err == nil || !strings.Contains(err.Error(), "mode is required") {
			t.Fatalf("expected mode error, got %v", err)
		}
	})

	t.Run("slice defaults offset and limit", func(t *testing.T) {
		t.Setenv("AXON_READ_LIMIT", "2")
		out, err := tool.Fn(json.RawMessage(`{"path":"sample.go","mode":"slice","offset":0,"reason":"default clamp"}`))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "1\tpackage main") || !strings.Contains(out, "2\t") {
			t.Fatalf("expected first two lines, got %q", out)
		}
		if strings.Contains(out, "3\timport") {
			t.Fatalf("expected limit=2 to apply, got %q", out)
		}
	})

	t.Run("slice returns explicit range", func(t *testing.T) {
		out, err := tool.Fn(json.RawMessage(`{"path":"sample.go","mode":"slice","offset":7,"limit":2,"reason":"body lines"}`))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "7\tfunc Hello() {") || !strings.Contains(out, "8\t\tfmt.Println(\"hello\")") {
			t.Fatalf("unexpected slice output: %q", out)
		}
		if strings.Contains(out, "9\t}") {
			t.Fatalf("slice exceeded requested limit: %q", out)
		}
	})

	t.Run("slice empty range", func(t *testing.T) {
		out, err := readSliceMode(path, 99, 3)
		if err != nil {
			t.Fatal(err)
		}
		if out != "[empty range]" {
			t.Fatalf("got %q", out)
		}
	})

	t.Run("slice missing file", func(t *testing.T) {
		_, err := readSliceMode(filepath.Join(s.Cwd, "missing.go"), 1, 1)
		if err == nil {
			t.Fatal("expected missing file error")
		}
	})

	t.Run("full missing file", func(t *testing.T) {
		_, err := readFullMode(filepath.Join(s.Cwd, "missing.go"))
		if err == nil {
			t.Fatal("expected missing file error")
		}
	})

	t.Run("full includes header and line numbers", func(t *testing.T) {
		out, err := tool.Fn(json.RawMessage(`{"path":"sample.go","mode":"full","reason":"need entire file"}`))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "[full read: ~") {
			t.Fatalf("missing full-read header: %q", out)
		}
		if !strings.Contains(out, "1\tpackage main") || !strings.Contains(out, "11\tfunc World() {}") {
			t.Fatalf("missing line-numbered content: %q", out)
		}
	})

	t.Run("skeleton extracts structure", func(t *testing.T) {
		out, err := tool.Fn(json.RawMessage(`{"path":"sample.go","mode":"skeleton","reason":"map file"}`))
		if err != nil {
			t.Fatal(err)
		}
		wantParts := []string{
			"[skeleton: top-level declarations",
			"1\tpackage main",
			"3\timport \"fmt\"",
			"5\ttype Person struct{}",
			"7\tfunc Hello() {",
			"11\tfunc World() {}",
		}
		for _, part := range wantParts {
			if !strings.Contains(out, part) {
				t.Fatalf("missing %q in skeleton output: %q", part, out)
			}
		}
	})

	t.Run("skeleton fallback for data file", func(t *testing.T) {
		dataPath := filepath.Join(s.Cwd, "notes.txt")
		mustWriteFile(t, dataPath, "plain text\nwith no declarations\n")
		out, err := readSkeletonMode(dataPath)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "skeleton found no signatures") {
			t.Fatalf("unexpected fallback output: %q", out)
		}
	})

	t.Run("skeleton missing file", func(t *testing.T) {
		_, err := readSkeletonMode(filepath.Join(s.Cwd, "missing.txt"))
		if err == nil {
			t.Fatal("expected missing file error")
		}
	})

	t.Run("skeleton pattern coverage across languages", func(t *testing.T) {
		jsPath := filepath.Join(s.Cwd, "sample.js")
		mustWriteFile(t, jsPath, strings.Join([]string{
			"export default function Hello() {",
			"  return 1",
			"}",
			"class Person {}",
			"const value = 1",
		}, "\n"))
		out, err := readSkeletonMode(jsPath)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "1\texport default function Hello() {") || !strings.Contains(out, "4\tclass Person {}") {
			t.Fatalf("expected JS structure hits, got %q", out)
		}
	})
}

func TestWriteToolComprehensive(t *testing.T) {
	s := tmpSession(t)
	tool := WriteTool(s)

	t.Run("requires reason", func(t *testing.T) {
		_, err := tool.Fn(json.RawMessage(`{"path":"a.txt","mode":"create","content":"x"}`))
		if err == nil || !strings.Contains(err.Error(), "reason is required") {
			t.Fatalf("expected reason error, got %v", err)
		}
	})

	t.Run("requires path", func(t *testing.T) {
		_, err := tool.Fn(json.RawMessage(`{"mode":"create","content":"x","reason":"missing path"}`))
		if err == nil || !strings.Contains(err.Error(), "path is required") {
			t.Fatalf("expected path error, got %v", err)
		}
	})

	t.Run("rejects invalid mode", func(t *testing.T) {
		_, err := tool.Fn(json.RawMessage(`{"path":"a.txt","mode":"oops","content":"x","reason":"bad mode"}`))
		if err == nil || !strings.Contains(err.Error(), "mode is required") {
			t.Fatalf("expected mode error, got %v", err)
		}
	})

	t.Run("create makes directories and records edit", func(t *testing.T) {
		out, err := tool.Fn(json.RawMessage(`{"path":"nested/dir/a.txt","mode":"create","content":"hello","reason":"create file"}`))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "created") {
			t.Fatalf("unexpected create output: %q", out)
		}
		data, err := os.ReadFile(filepath.Join(s.Cwd, "nested/dir/a.txt"))
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "hello" {
			t.Fatalf("got %q", data)
		}
		if len(s.Edits) == 0 || s.Edits[len(s.Edits)-1].After != "hello" {
			t.Fatalf("create should record edit, got %+v", s.Edits)
		}
	})

	t.Run("create rejects existing file", func(t *testing.T) {
		mustWriteFile(t, filepath.Join(s.Cwd, "exists.txt"), "x")
		_, err := tool.Fn(json.RawMessage(`{"path":"exists.txt","mode":"create","content":"y","reason":"should fail"}`))
		if err == nil || !strings.Contains(err.Error(), "file exists") {
			t.Fatalf("expected exists error, got %v", err)
		}
	})

	t.Run("overwrite works on existing and missing file", func(t *testing.T) {
		mustWriteFile(t, filepath.Join(s.Cwd, "overwrite.txt"), "before")
		out, err := tool.Fn(json.RawMessage(`{"path":"overwrite.txt","mode":"overwrite","content":"after","reason":"replace all"}`))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "overwrote") {
			t.Fatalf("unexpected overwrite output: %q", out)
		}
		data, _ := os.ReadFile(filepath.Join(s.Cwd, "overwrite.txt"))
		if string(data) != "after" {
			t.Fatalf("got %q", data)
		}

		_, err = tool.Fn(json.RawMessage(`{"path":"newdir/new.txt","mode":"overwrite","content":"fresh","reason":"create via overwrite"}`))
		if err != nil {
			t.Fatal(err)
		}
		data, _ = os.ReadFile(filepath.Join(s.Cwd, "newdir/new.txt"))
		if string(data) != "fresh" {
			t.Fatalf("got %q", data)
		}
	})

	t.Run("create and overwrite helper directory edge cases", func(t *testing.T) {
		nestedCreate := filepath.Join(s.Cwd, "deep", "create", "file.txt")
		if _, err := writeCreateMode(s, nestedCreate, "x"); err != nil {
			t.Fatalf("nested create should succeed, got %v", err)
		}
		if data, err := os.ReadFile(nestedCreate); err != nil || string(data) != "x" {
			t.Fatalf("unexpected nested create content=%q err=%v", data, err)
		}

		nestedOverwrite := filepath.Join(s.Cwd, "deep", "overwrite", "file.txt")
		if _, err := writeOverwriteMode(s, nestedOverwrite, "y"); err != nil {
			t.Fatalf("nested overwrite should succeed, got %v", err)
		}
		if data, err := os.ReadFile(nestedOverwrite); err != nil || string(data) != "y" {
			t.Fatalf("unexpected nested overwrite content=%q err=%v", data, err)
		}

		blocker := filepath.Join(s.Cwd, "parent-file")
		mustWriteFile(t, blocker, "block")
		if _, err := writeCreateMode(s, filepath.Join(blocker, "child.txt"), "z"); err == nil {
			t.Fatal("expected create helper mkdir failure")
		}
		if _, err := writeOverwriteMode(s, filepath.Join(blocker, "child.txt"), "z"); err == nil {
			t.Fatal("expected overwrite helper mkdir failure")
		}

		if _, err := writeCreateMode(s, string([]byte{'b', 'a', 'd', 0, 'x'}), "z"); err == nil {
			t.Fatal("expected create helper stat error on invalid path")
		}
	})

	t.Run("replace_string validations", func(t *testing.T) {
		_, err := writeReplaceStringMode(s, filepath.Join(s.Cwd, "missing.txt"), "a", "b")
		if err == nil {
			t.Fatal("expected missing-file error from helper")
		}

		mustWriteFile(t, filepath.Join(s.Cwd, "replace.txt"), "alpha\nbeta\n")
		_, err = tool.Fn(json.RawMessage(`{"path":"replace.txt","mode":"replace_string","content":"x","reason":"missing old"}`))
		if err == nil || !strings.Contains(err.Error(), "old_str is required") {
			t.Fatalf("expected old_str error, got %v", err)
		}

		_, err = tool.Fn(json.RawMessage(`{"path":"replace.txt","mode":"replace_string","old_str":"zzz","content":"x","reason":"miss"}`))
		if err == nil || !strings.Contains(err.Error(), "old_str not found") {
			t.Fatalf("expected not-found error, got %v", err)
		}

		mustWriteFile(t, filepath.Join(s.Cwd, "ambig.txt"), "x\nx\n")
		_, err = tool.Fn(json.RawMessage(`{"path":"ambig.txt","mode":"replace_string","old_str":"x","content":"y","reason":"ambiguous"}`))
		if err == nil || !strings.Contains(err.Error(), "matches 2") {
			t.Fatalf("expected ambiguous error, got %v", err)
		}

		out, err := tool.Fn(json.RawMessage(`{"path":"replace.txt","mode":"replace_string","old_str":"beta","content":"BETA","reason":"precise replace"}`))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "replaced 1 occurrence") {
			t.Fatalf("unexpected replace_string output: %q", out)
		}
		data, _ := os.ReadFile(filepath.Join(s.Cwd, "replace.txt"))
		if string(data) != "alpha\nBETA\n" {
			t.Fatalf("got %q", data)
		}
	})

	t.Run("replace_lines clamps end and trims trailing newline in replacement", func(t *testing.T) {
		mustWriteFile(t, filepath.Join(s.Cwd, "lines.txt"), "a\nb\nc\nd")

		_, err := writeReplaceLinesMode(s, filepath.Join(s.Cwd, "missing-lines.txt"), 1, 1, "x")
		if err == nil {
			t.Fatal("expected helper missing-file error")
		}

		_, err = tool.Fn(json.RawMessage(`{"path":"lines.txt","mode":"replace_lines","start_line":0,"end_line":2,"content":"x","reason":"bad start"}`))
		if err == nil || !strings.Contains(err.Error(), "start_line >= 1") {
			t.Fatalf("expected start_line validation error, got %v", err)
		}

		_, err = tool.Fn(json.RawMessage(`{"path":"lines.txt","mode":"replace_lines","start_line":9,"end_line":9,"content":"x","reason":"past eof"}`))
		if err == nil || !strings.Contains(err.Error(), "past end of file") {
			t.Fatalf("expected past eof error, got %v", err)
		}

		out, err := tool.Fn(json.RawMessage(`{"path":"lines.txt","mode":"replace_lines","start_line":2,"end_line":99,"content":"B\nC\n","reason":"replace tail"}`))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "replaced lines 2-4") {
			t.Fatalf("unexpected replace_lines output: %q", out)
		}
		data, _ := os.ReadFile(filepath.Join(s.Cwd, "lines.txt"))
		if string(data) != "a\nB\nC" {
			t.Fatalf("unexpected line replacement result: %q", data)
		}

		mustWriteFile(t, filepath.Join(s.Cwd, "delete-lines.txt"), "x\ny\nz")
		out, err = tool.Fn(json.RawMessage(`{"path":"delete-lines.txt","mode":"replace_lines","start_line":2,"end_line":2,"content":"","reason":"delete a line"}`))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "replaced lines 2-2") {
			t.Fatalf("unexpected delete-lines output: %q", out)
		}
		data, _ = os.ReadFile(filepath.Join(s.Cwd, "delete-lines.txt"))
		if string(data) != "x\n\nz" {
			t.Fatalf("unexpected line deletion result: %q", data)
		}
	})

	t.Run("insert_at_line at head middle and eof", func(t *testing.T) {
		mustWriteFile(t, filepath.Join(s.Cwd, "insert.txt"), "a\nb\nc")

		_, err := writeInsertAtMode(s, filepath.Join(s.Cwd, "missing-insert.txt"), 1, "x")
		if err == nil {
			t.Fatal("expected helper missing-file error")
		}

		_, err = tool.Fn(json.RawMessage(`{"path":"insert.txt","mode":"insert_at_line","start_line":0,"content":"x","reason":"bad line"}`))
		if err == nil || !strings.Contains(err.Error(), "start_line >= 1") {
			t.Fatalf("expected start_line error, got %v", err)
		}

		_, err = tool.Fn(json.RawMessage(`{"path":"insert.txt","mode":"insert_at_line","start_line":10,"content":"x","reason":"too far"}`))
		if err == nil || !strings.Contains(err.Error(), "past end of file") {
			t.Fatalf("expected past eof error, got %v", err)
		}

		_, err = tool.Fn(json.RawMessage(`{"path":"insert.txt","mode":"insert_at_line","start_line":1,"content":"HEAD\n","reason":"prepend"}`))
		if err != nil {
			t.Fatal(err)
		}
		data, _ := os.ReadFile(filepath.Join(s.Cwd, "insert.txt"))
		if string(data) != "HEAD\na\nb\nc" {
			t.Fatalf("unexpected head insert result: %q", data)
		}

		_, err = tool.Fn(json.RawMessage(`{"path":"insert.txt","mode":"insert_at_line","start_line":3,"content":"MID\n","reason":"middle insert"}`))
		if err != nil {
			t.Fatal(err)
		}
		data, _ = os.ReadFile(filepath.Join(s.Cwd, "insert.txt"))
		if string(data) != "HEAD\na\nMID\nb\nc" {
			t.Fatalf("unexpected middle insert result: %q", data)
		}

		_, err = tool.Fn(json.RawMessage(`{"path":"insert.txt","mode":"insert_at_line","start_line":6,"content":"TAIL\n","reason":"append"}`))
		if err != nil {
			t.Fatal(err)
		}
		data, _ = os.ReadFile(filepath.Join(s.Cwd, "insert.txt"))
		if string(data) != "HEAD\na\nMID\nb\nc\nTAIL" {
			t.Fatalf("unexpected eof insert result: %q", data)
		}
	})
}

func TestSearchToolComprehensive(t *testing.T) {
	requireCommand(t, "rg")
	s := tmpSession(t)

	mustWriteFile(t, filepath.Join(s.Cwd, "alpha.go"), strings.Join([]string{
		"package main",
		"",
		"func Hello() {}",
	}, "\n"))
	mustWriteFile(t, filepath.Join(s.Cwd, "beta.go"), strings.Join([]string{
		"package main",
		"",
		"func Use() {",
		"\tHello()",
		"}",
	}, "\n"))
	mustWriteFile(t, filepath.Join(s.Cwd, "gamma.txt"), "HELLO in text\nsecond line\n")

	tool := SearchTool(s)

	t.Run("requires reason", func(t *testing.T) {
		_, err := tool.Fn(json.RawMessage(`{"query":"Hello","mode":"literal"}`))
		if err == nil || !strings.Contains(err.Error(), "reason is required") {
			t.Fatalf("expected reason error, got %v", err)
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		if _, err := tool.Fn(json.RawMessage(`{`)); err == nil {
			t.Fatal("expected invalid json error")
		}
	})

	t.Run("requires query", func(t *testing.T) {
		_, err := tool.Fn(json.RawMessage(`{"mode":"literal","reason":"missing query"}`))
		if err == nil || !strings.Contains(err.Error(), "query is required") {
			t.Fatalf("expected query error, got %v", err)
		}
	})

	t.Run("rejects invalid mode", func(t *testing.T) {
		_, err := tool.Fn(json.RawMessage(`{"query":"Hello","mode":"weird","reason":"bad mode"}`))
		if err == nil || !strings.Contains(err.Error(), "mode is required") {
			t.Fatalf("expected mode error, got %v", err)
		}
	})

	t.Run("literal search is case insensitive and defaults path", func(t *testing.T) {
		out, err := tool.Fn(json.RawMessage(`{"query":"hello","mode":"literal","reason":"find token"}`))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "query: hello") || !strings.Contains(out, "alpha.go:3:func Hello() {}") || !strings.Contains(out, "gamma.txt:1:HELLO in text") {
			t.Fatalf("unexpected literal output: %q", out)
		}
	})

	t.Run("regex search works", func(t *testing.T) {
		out, err := tool.Fn(json.RawMessage(`{"query":"H.llo\\(","mode":"regex","path":".","reason":"regex locate"}`))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "beta.go:4:\tHello()") {
			t.Fatalf("unexpected regex output: %q", out)
		}
	})

	t.Run("globs narrow results", func(t *testing.T) {
		out, err := tool.Fn(json.RawMessage(`{"query":"hello","mode":"literal","path":".","globs":["*.go"],"reason":"only go files"}`))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out, "gamma.txt") {
			t.Fatalf("glob should exclude text file: %q", out)
		}
		if !strings.Contains(out, "alpha.go") {
			t.Fatalf("expected go match, got %q", out)
		}
	})

	t.Run("no matches", func(t *testing.T) {
		out, err := tool.Fn(json.RawMessage(`{"query":"missing_symbol","mode":"literal","path":".","reason":"expect none"}`))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "no matches") {
			t.Fatalf("expected no matches output, got %q", out)
		}
	})

	t.Run("invalid path surfaces rg error", func(t *testing.T) {
		_, err := runRipgrep(s, "hello", filepath.Join(s.Cwd, "definitely-missing-dir"), nil, true)
		if err == nil {
			t.Fatal("expected rg path error")
		}
	})

	t.Run("search truncation marker", func(t *testing.T) {
		t.Setenv("AXON_SEARCH_OUTPUT_LIMIT", "40")
		mustWriteFile(t, filepath.Join(s.Cwd, "many.txt"), strings.Repeat("match here\n", 20))
		out, err := tool.Fn(json.RawMessage(`{"query":"match","mode":"literal","path":".","reason":"force truncation"}`))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "[output truncated") {
			t.Fatalf("expected truncation marker, got %q", out)
		}
	})

	t.Run("search requires rg in PATH", func(t *testing.T) {
		t.Setenv("PATH", "")
		_, err := runRipgrep(s, "hello", ".", nil, true)
		if err == nil || !strings.Contains(err.Error(), "search requires rg in PATH") {
			t.Fatalf("expected missing rg error, got %v", err)
		}
	})

	t.Run("rgCollect parses hits", func(t *testing.T) {
		hits, err := rgCollect(s, `Hello`, ".", []string{"*.go"})
		if err != nil {
			t.Fatal(err)
		}
		if len(hits) < 2 {
			t.Fatalf("expected at least two hits, got %+v", hits)
		}
		found := false
		for _, h := range hits {
			if strings.HasSuffix(h.file, "beta.go") && h.line == 4 && strings.Contains(h.text, "Hello()") {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected parsed caller hit, got %+v", hits)
		}
	})

	t.Run("rgCollect no matches returns nil slice", func(t *testing.T) {
		hits, err := rgCollect(s, `NoSuchSymbol`, ".", []string{"*.go"})
		if err != nil {
			t.Fatal(err)
		}
		if hits != nil {
			t.Fatalf("expected nil hits on no match, got %+v", hits)
		}
	})

	t.Run("rgCollect ignores malformed lines", func(t *testing.T) {
		dir := t.TempDir()
		script := filepath.Join(dir, "rg")
		mustWriteFile(t, script, "#!/bin/sh\nprintf 'garbage\\nfile.go:12:ok\\nweird:abc:still-kept\\n'\n")
		if err := os.Chmod(script, 0755); err != nil {
			t.Fatal(err)
		}

		oldPath := os.Getenv("PATH")
		t.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath)
		hits, err := rgCollect(s, `ignored`, ".", nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(hits) != 2 {
			t.Fatalf("expected 2 parsed hits, got %+v", hits)
		}
		if hits[0].file != "file.go" || hits[0].line != 12 || hits[0].text != "ok" {
			t.Fatalf("unexpected first parsed hit: %+v", hits[0])
		}
		if hits[1].file != "weird" || hits[1].line != 0 || hits[1].text != "still-kept" {
			t.Fatalf("unexpected malformed-number parsed hit: %+v", hits[1])
		}
	})

	t.Run("trace returns definition and caller", func(t *testing.T) {
		out, err := tool.Fn(json.RawMessage(`{"query":"Hello","mode":"trace","path":".","globs":["*.go"],"reason":"follow symbol"}`))
		if err != nil {
			t.Fatal(err)
		}
		wantParts := []string{
			"SYMBOL: Hello",
			"DEFINED:",
			"alpha.go:3  func Hello() {}",
			"CALLED FROM:",
			"beta.go:4  Hello()",
			"[trace: regex heuristic.",
		}
		for _, part := range wantParts {
			if !strings.Contains(out, part) {
				t.Fatalf("missing %q in trace output: %q", part, out)
			}
		}
		if strings.Contains(out, "alpha.go:3  func Hello() {}") && strings.Count(out, "alpha.go:3  func Hello() {}") != 1 {
			t.Fatalf("definition line should not be duplicated as a caller: %q", out)
		}
	})

	t.Run("trace handles unknown symbol", func(t *testing.T) {
		out, err := runTrace(s, "DefinitelyMissing", ".", []string{"*.go"})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "DEFINED: <not found>") || !strings.Contains(out, "<no callers found>") {
			t.Fatalf("unexpected missing-symbol trace output: %q", out)
		}
	})

	t.Run("trace requires rg in PATH", func(t *testing.T) {
		t.Setenv("PATH", "")
		_, err := runTrace(s, "Hello", ".", []string{"*.go"})
		if err == nil || !strings.Contains(err.Error(), "trace requires rg in PATH") {
			t.Fatalf("expected missing rg error, got %v", err)
		}
	})
}

func TestExecToolComprehensive(t *testing.T) {
	requireCommand(t, "sh")
	s := tmpSession(t)
	tool := ExecTool(s)

	t.Run("invalid json", func(t *testing.T) {
		if _, err := tool.Fn(json.RawMessage(`{`)); err == nil {
			t.Fatal("expected invalid json error")
		}
	})

	t.Run("requires reason", func(t *testing.T) {
		_, err := tool.Fn(json.RawMessage(`{"mode":"run","command":"echo hi","tail_lines":1}`))
		if err == nil || !strings.Contains(err.Error(), "reason is required") {
			t.Fatalf("expected reason error, got %v", err)
		}
	})

	t.Run("requires command", func(t *testing.T) {
		_, err := tool.Fn(json.RawMessage(`{"mode":"run","tail_lines":1,"reason":"missing command"}`))
		if err == nil || !strings.Contains(err.Error(), "command is required") {
			t.Fatalf("expected command error, got %v", err)
		}
	})

	t.Run("requires positive tail lines", func(t *testing.T) {
		_, err := tool.Fn(json.RawMessage(`{"mode":"run","command":"echo hi","tail_lines":0,"reason":"bad tail"}`))
		if err == nil || !strings.Contains(err.Error(), "tail_lines is required") {
			t.Fatalf("expected tail_lines error, got %v", err)
		}
	})

	t.Run("verify mode fails when no marker exists", func(t *testing.T) {
		empty := tmpSession(t)
		outTool := ExecTool(empty)
		_, err := outTool.Fn(json.RawMessage(`{"mode":"verify","reason":"no project markers"}`))
		if err == nil || !strings.Contains(err.Error(), "could not detect build/check command") {
			t.Fatalf("expected verify-detect error, got %v", err)
		}
	})

	t.Run("rejects invalid mode", func(t *testing.T) {
		_, err := tool.Fn(json.RawMessage(`{"mode":"weird","reason":"bad mode"}`))
		if err == nil || !strings.Contains(err.Error(), "mode is required") {
			t.Fatalf("expected mode error, got %v", err)
		}
	})

	t.Run("basic success", func(t *testing.T) {
		out, err := tool.Fn(json.RawMessage(`{"mode":"run","command":"printf 'a\nb\nc\n'","tail_lines":2,"reason":"tail output","expected_outcome":"print last lines"}`))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "$ printf 'a\\nb\\nc\\n'") && !strings.Contains(out, "$ printf 'a\nb\nc\n'") {
			t.Fatalf("missing command echo: %q", out)
		}
		if !strings.Contains(out, "expected: print last lines") || !strings.Contains(out, "exit_code: 0") {
			t.Fatalf("missing expected/exit code: %q", out)
		}
		if !strings.Contains(out, "[1 earlier lines hidden]") {
			t.Fatalf("expected hidden-line count, got %q", out)
		}
		if !strings.Contains(out, "b\nc") {
			t.Fatalf("expected tailed output, got %q", out)
		}
	})

	t.Run("defaults session cwd when dir omitted", func(t *testing.T) {
		sub := filepath.Join(s.Cwd, "cwd-default")
		if err := os.MkdirAll(sub, 0755); err != nil {
			t.Fatal(err)
		}
		s.Cwd = sub
		out, err := tool.Fn(json.RawMessage(`{"mode":"run","command":"pwd","tail_lines":5,"reason":"use session cwd"}`))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "dir: "+sub) || !strings.Contains(out, sub) {
			t.Fatalf("expected session cwd to be used, got %q", out)
		}
	})

	t.Run("relative dir resolves via session cwd", func(t *testing.T) {
		mustWriteFile(t, filepath.Join(s.Cwd, "sub", "file.txt"), "x")
		out, err := tool.Fn(json.RawMessage(`{"mode":"run","command":"pwd","dir":"sub","tail_lines":5,"reason":"check dir"}`))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "dir: "+filepath.Join(s.Cwd, "sub")) {
			t.Fatalf("expected resolved dir in output, got %q", out)
		}
		if !strings.Contains(out, filepath.Join(s.Cwd, "sub")) {
			t.Fatalf("expected pwd output from subdir, got %q", out)
		}
	})

	t.Run("non zero exit code is returned in output", func(t *testing.T) {
		out, err := tool.Fn(json.RawMessage(`{"mode":"run","command":"printf 'bad\n'; exit 7","tail_lines":5,"reason":"capture failure","expected_outcome":"exit zero"}`))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "exit_code: 7") || !strings.Contains(out, "bad") {
			t.Fatalf("expected non-zero exit code output, got %q", out)
		}
	})

	t.Run("timeout note is surfaced", func(t *testing.T) {
		out, err := tool.Fn(json.RawMessage(`{"mode":"run","command":"sleep 2","tail_lines":5,"timeout_seconds":1,"reason":"force timeout"}`))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "exit_code: -1 (timed out)") {
			t.Fatalf("expected timeout note, got %q", out)
		}
	})

	t.Run("command not found still returns shell exit code", func(t *testing.T) {
		out, err := tool.Fn(json.RawMessage(`{"mode":"run","command":"definitely_missing_command_axon_test","tail_lines":10,"reason":"shell error path"}`))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "exit_code: 127") {
			t.Fatalf("expected shell not-found exit code, got %q", out)
		}
	})

	t.Run("timeout note", func(t *testing.T) {
		out, err := tool.Fn(json.RawMessage(`{"mode":"run","command":"sleep 2","tail_lines":5,"timeout_seconds":1,"reason":"force timeout"}`))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "exit_code: -1 (timed out)") {
			t.Fatalf("expected timeout marker, got %q", out)
		}
	})

	t.Run("truncation marker", func(t *testing.T) {
		t.Setenv("AXON_EXEC_OUTPUT_LIMIT", "30")
		out, err := tool.Fn(json.RawMessage(`{"mode":"run","command":"i=1; while [ $i -le 20 ]; do echo line$i; i=$((i+1)); done","tail_lines":20,"reason":"force truncation"}`))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "[output truncated at byte limit]") {
			t.Fatalf("expected truncation marker, got %q", out)
		}
	})
}

func TestTailNAndFormatExecComprehensive(t *testing.T) {
	t.Run("tailN with exact and oversized window", func(t *testing.T) {
		out, hidden := tailN("a\nb\nc", 3)
		if out != "a\nb\nc" || hidden != 0 {
			t.Fatalf("expected passthrough, got out=%q hidden=%d", out, hidden)
		}
		out, hidden = tailN("a\nb\nc\nd", 2)
		if out != "c\nd" || hidden != 2 {
			t.Fatalf("unexpected tail output: out=%q hidden=%d", out, hidden)
		}
		out, hidden = tailN("", 5)
		if out != "" || hidden != 0 {
			t.Fatalf("empty input should stay empty: out=%q hidden=%d", out, hidden)
		}
	})

	t.Run("formatExec full decoration", func(t *testing.T) {
		out := formatExec("go test ./...", "/tmp/project", 1, "tests pass", "line1\nline2\n", 8, true, "timed out")
		wantParts := []string{
			"$ go test ./...",
			"dir: /tmp/project",
			"expected: tests pass",
			"exit_code: 1 (timed out)",
			"[8 earlier lines hidden]",
			"line1\nline2",
			"[output truncated at byte limit]",
		}
		for _, part := range wantParts {
			if !strings.Contains(out, part) {
				t.Fatalf("missing %q in formatExec output: %q", part, out)
			}
		}
	})

	t.Run("formatExec without optional sections", func(t *testing.T) {
		out := formatExec("echo hi", "", 0, "", "", 0, false, "")
		if strings.Contains(out, "dir:") || strings.Contains(out, "expected:") || strings.Contains(out, "hidden") || strings.Contains(out, "truncated") {
			t.Fatalf("unexpected optional sections in %q", out)
		}
		if !strings.Contains(out, "exit_code: 0") {
			t.Fatalf("missing exit code in %q", out)
		}
	})
}

func TestTaskAndMemoryToolsComprehensive(t *testing.T) {
	t.Run("task tool registers and overwrites task", func(t *testing.T) {
		s := tmpSession(t)
		tool := TaskTool(s)

		_, err := tool.Fn(json.RawMessage(`{"objective":"  fix login  ","definition_of_done":" tests pass ","current_focus":" inspect auth ","reason":"start scoped work"}`))
		if err != nil {
			t.Fatal(err)
		}
		if s.CurrentTask == nil {
			t.Fatal("task should be registered")
		}
		if s.CurrentTask.Objective != "fix login" || s.CurrentTask.DefinitionOfDone != "tests pass" || s.CurrentTask.CurrentFocus != "inspect auth" {
			t.Fatalf("task fields should be trimmed, got %+v", s.CurrentTask)
		}
		tb := s.TaskBlock()
		if !strings.Contains(tb, "objective:          fix login") || !strings.Contains(tb, "definition of done: tests pass") {
			t.Fatalf("unexpected task block: %q", tb)
		}

		_, err = tool.Fn(json.RawMessage(`{"objective":"new task","definition_of_done":"done","current_focus":"focus","reason":"shift objective"}`))
		if err != nil {
			t.Fatal(err)
		}
		if s.CurrentTask.Objective != "new task" {
			t.Fatalf("task should overwrite previous objective, got %+v", s.CurrentTask)
		}
	})

	t.Run("task tool requires reason", func(t *testing.T) {
		s := tmpSession(t)
		_, err := TaskTool(s).Fn(json.RawMessage(`{"objective":"x","definition_of_done":"y","current_focus":"z"}`))
		if err == nil || !strings.Contains(err.Error(), "reason is required") {
			t.Fatalf("expected reason error, got %v", err)
		}
	})

	t.Run("task tool invalid json and save error", func(t *testing.T) {
		s := tmpSession(t)
		if _, err := TaskTool(s).Fn(json.RawMessage(`{`)); err == nil {
			t.Fatal("expected invalid json error")
		}
		s.path = t.TempDir()
		if _, err := TaskTool(s).Fn(json.RawMessage(`{"objective":"x","definition_of_done":"y","current_focus":"z","reason":"persist"}`)); err == nil {
			t.Fatal("expected save error when session path is directory")
		}
	})

	t.Run("park tool with next_step end succeeds and stores breadcrumb", func(t *testing.T) {
		s := tmpSession(t)
		s.Append(Msg{Role: "tool", Content: "large output"})
		id := s.Messages[len(s.Messages)-1].ID

		tool := ParkTool(s)
		out, err := tool.Fn(json.RawMessage(`{"next_step":"end","blocks":[{"id":"` + id + `","summary":"useful gist","reason":"extracted fact"}]}`))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, id+` parked: "useful gist"`) {
			t.Fatalf("unexpected park output: %q", out)
		}
		if !s.Messages[len(s.Messages)-1].Parked {
			t.Fatal("message should be parked")
		}
		if rec, ok := s.ParkedBlocks[id]; !ok || !strings.Contains(rec.Breadcrumb, "useful gist") {
			t.Fatalf("parked breadcrumb missing, got %+v", s.ParkedBlocks[id])
		}
	})

	t.Run("park tool validation", func(t *testing.T) {
		s := tmpSession(t)
		tool := ParkTool(s)

		_, err := tool.Fn(json.RawMessage(`{"blocks":[]}`))
		if err == nil || !strings.Contains(err.Error(), "blocks are required") {
			t.Fatalf("expected blocks error, got %v", err)
		}

		s.Append(Msg{Role: "tool", Content: "x"})
		id := s.Messages[len(s.Messages)-1].ID
		_, err = tool.Fn(json.RawMessage(`{"blocks":[{"id":"` + id + `","summary":"gist","reason":""}]}`))
		if err == nil || !strings.Contains(err.Error(), "reason is required") {
			t.Fatalf("expected nested reason error, got %v", err)
		}
	})

	t.Run("park tool invalid json and save error", func(t *testing.T) {
		s := tmpSession(t)
		if _, err := ParkTool(s).Fn(json.RawMessage(`{`)); err == nil {
			t.Fatal("expected invalid json error")
		}
		s.Append(Msg{Role: "tool", Content: "x"})
		id := s.Messages[len(s.Messages)-1].ID
		s.path = t.TempDir()
		if _, err := ParkTool(s).Fn(json.RawMessage(`{"blocks":[{"id":"` + id + `","summary":"gist","reason":"persist"}]}`)); err == nil {
			t.Fatal("expected save error when session path is directory")
		}
	})

	t.Run("recall tool by id and query", func(t *testing.T) {
		s := tmpSession(t)
		s.Append(Msg{Role: "user", Content: "important auth context"})
		id := s.Messages[len(s.Messages)-1].ID
		if err := s.Park(id, "auth gist", "done reading"); err != nil {
			t.Fatal(err)
		}

		tool := RecallTool(s)
		out, err := tool.Fn(json.RawMessage(`{"ids":["` + id + `"],"reason":"need original"}`))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "important auth context") || !strings.Contains(out, "summary: auth gist") {
			t.Fatalf("unexpected recall-by-id output: %q", out)
		}

		out, err = tool.Fn(json.RawMessage(`{"query":"important auth","next_step":"continue","reason":"search parked blocks"}`))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, id) {
			t.Fatalf("expected recall-by-query hit, got %q", out)
		}

		out, err = tool.Fn(json.RawMessage(`{"query":"no-such-parked-content","reason":"expect none"}`))
		if err != nil {
			t.Fatal(err)
		}
		if out != "no parked blocks matched" {
			t.Fatalf("unexpected no-match recall output: %q", out)
		}
	})

	t.Run("recall tool validation", func(t *testing.T) {
		s := tmpSession(t)
		tool := RecallTool(s)

		_, err := tool.Fn(json.RawMessage(`{"reason":"missing target"}`))
		if err == nil || !strings.Contains(err.Error(), "provide ids or query") {
			t.Fatalf("expected ids/query validation error, got %v", err)
		}

		_, err = tool.Fn(json.RawMessage(`{"ids":["m1"]}`))
		if err == nil || !strings.Contains(err.Error(), "reason is required") {
			t.Fatalf("expected reason validation error, got %v", err)
		}
	})

	t.Run("recall tool invalid json", func(t *testing.T) {
		s := tmpSession(t)
		if _, err := RecallTool(s).Fn(json.RawMessage(`{`)); err == nil {
			t.Fatal("expected invalid json error")
		}
	})

	t.Run("forget tool works and protects system messages", func(t *testing.T) {
		s := tmpSession(t)
		s.Append(Msg{Role: "user", Content: "noise"})
		id := s.Messages[len(s.Messages)-1].ID

		tool := ForgetTool(s)
		out, err := tool.Fn(json.RawMessage(`{"ids":["` + id + `"],"next_step":"end","reason":"pure noise"}`))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, id+" forgotten") {
			t.Fatalf("unexpected forget output: %q", out)
		}
		if !s.Messages[len(s.Messages)-1].Forgotten {
			t.Fatal("message should be marked forgotten")
		}

		s2 := tmpSession(t)
		initSessionMessages(s2)
		_, err = ForgetTool(s2).Fn(json.RawMessage(`{"ids":[""],"reason":"should fail"}`))
		if err == nil {
			t.Fatal("expected system-message protection error")
		}
	})

	t.Run("forget tool validation", func(t *testing.T) {
		s := tmpSession(t)
		tool := ForgetTool(s)
		_, err := tool.Fn(json.RawMessage(`{"ids":[],"reason":"none"}`))
		if err == nil || !strings.Contains(err.Error(), "ids are required") {
			t.Fatalf("expected ids error, got %v", err)
		}
	})

	t.Run("forget tool invalid json and save error", func(t *testing.T) {
		s := tmpSession(t)
		if _, err := ForgetTool(s).Fn(json.RawMessage(`{`)); err == nil {
			t.Fatal("expected invalid json error")
		}
		s.Append(Msg{Role: "user", Content: "x"})
		id := s.Messages[len(s.Messages)-1].ID
		s.path = t.TempDir()
		if _, err := ForgetTool(s).Fn(json.RawMessage(`{"ids":["` + id + `"],"reason":"persist"}`)); err == nil {
			t.Fatal("expected save error when session path is directory")
		}
	})

	t.Run("refresh tool refreshes active blocks and rejects parked blocks", func(t *testing.T) {
		s := tmpSession(t)
		s.Append(Msg{Role: "user", Content: "keep me"})
		id := s.Messages[len(s.Messages)-1].ID
		s.Messages[len(s.Messages)-1].TTL = 1

		tool := RefreshTool(s)
		out, err := tool.Fn(json.RawMessage(`{"ids":["` + id + `"],"ttl":9,"reason":"still needed"}`))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, id+" refreshed → ttl=9") {
			t.Fatalf("unexpected refresh output: %q", out)
		}
		if got := s.Messages[len(s.Messages)-1].TTL; got != 9 {
			t.Fatalf("expected ttl=9, got %d", got)
		}

		if err := s.Park(id, "gist", "done"); err != nil {
			t.Fatal(err)
		}
		_, err = tool.Fn(json.RawMessage(`{"ids":["` + id + `"],"reason":"should fail on parked"}`))
		if err == nil || !strings.Contains(err.Error(), "is parked") {
			t.Fatalf("expected parked refresh error, got %v", err)
		}
	})

	t.Run("refresh tool default ttl and validation", func(t *testing.T) {
		s := tmpSession(t)
		s.Append(Msg{Role: "user", Content: "x"})
		id := s.Messages[len(s.Messages)-1].ID
		s.Messages[len(s.Messages)-1].TTL = 1

		tool := RefreshTool(s)
		out, err := tool.Fn(json.RawMessage(`{"ids":["` + id + `"],"ttl":0,"reason":"use default ttl"}`))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, `ttl=5`) {
			t.Fatalf("expected default ttl output, got %q", out)
		}

		_, err = tool.Fn(json.RawMessage(`{"ids":[],"reason":"missing ids"}`))
		if err == nil || !strings.Contains(err.Error(), "ids are required") {
			t.Fatalf("expected ids validation error, got %v", err)
		}
	})

	t.Run("refresh tool invalid json and save error", func(t *testing.T) {
		s := tmpSession(t)
		if _, err := RefreshTool(s).Fn(json.RawMessage(`{`)); err == nil {
			t.Fatal("expected invalid json error")
		}
		s.Append(Msg{Role: "user", Content: "x"})
		id := s.Messages[len(s.Messages)-1].ID
		s.path = t.TempDir()
		if _, err := RefreshTool(s).Fn(json.RawMessage(`{"ids":["` + id + `"],"reason":"persist"}`)); err == nil {
			t.Fatal("expected save error when session path is directory")
		}
	})
}
