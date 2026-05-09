package main

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
)

// SS1 — literal query that is the empty string.
func TestSearchStress_SS1_EmptyQuery(t *testing.T) {
	skipIfNoRG(t)
	s := newSession(t)
	writeFile(t, s, "a.txt", "hello\n")
	// rg with empty pattern matches every line — must not panic.
	out, err := callTool(t, context.Background(), s, "search", map[string]any{
		"query": "", "mode": "literal", "reason": "t",
	})
	_ = out
	_ = err // error or success both acceptable; no panic
}

// SS2 — regex that is intentionally invalid: clean error or warning, no panic.
func TestSearchStress_SS2_InvalidRegex(t *testing.T) {
	skipIfNoRG(t)
	s := newSession(t)
	writeFile(t, s, "a.txt", "hello\n")
	_, _ = callTool(t, context.Background(), s, "search", map[string]any{
		"query": "(unclosed", "mode": "regex", "reason": "t",
	})
}

// SS3 — query string containing shell metacharacters ($, `, \, ") is passed
// safely to rg (not interpolated by sh -c).
func TestSearchStress_SS3_QueryWithShellMeta(t *testing.T) {
	skipIfNoRG(t)
	s := newSession(t)
	writeFile(t, s, "a.txt", "price $100 `whoami`\n")
	out, err := callTool(t, context.Background(), s, "search", map[string]any{
		"query": "$100", "mode": "literal", "case_sensitive": true, "reason": "t",
	})
	mustNoErr(t, err)
	mustContain(t, out, "$100")
	// If `whoami` were injected and executed, we'd see a system username.
	// The output should just be the literal file match, not command output.
	mustNotContain(t, out, "root")
}

// SS4 — query with path traversal ("../../etc/passwd") is just a literal
// search — must not read /etc/passwd.
func TestSearchStress_SS4_QueryWithPathTraversal(t *testing.T) {
	skipIfNoRG(t)
	s := newSession(t)
	writeFile(t, s, "a.txt", "../../etc/passwd\n")
	out, err := callTool(t, context.Background(), s, "search", map[string]any{
		"query": "../../etc/passwd", "mode": "literal", "reason": "t",
	})
	mustNoErr(t, err)
	mustContain(t, out, "../../etc/passwd")
	mustNotContain(t, out, "root:x:0:0")
}

// SS5 — max_matches=1 with 1000 matches: at most 1 hit returned.
func TestSearchStress_SS5_MaxMatchesOne(t *testing.T) {
	skipIfNoRG(t)
	s := newSession(t)
	writeFile(t, s, "a.txt", strings.Repeat("x\n", 1000))
	out, err := callTool(t, context.Background(), s, "search", map[string]any{
		"query": "x", "mode": "literal", "max_matches": 1, "reason": "t",
	})
	mustNoErr(t, err)
	hits := strings.Count(out, "a.txt:")
	if hits > 1 {
		t.Fatalf("expected <=1 hit, got %d", hits)
	}
}

// SS6 — max_matches=0 is handled (no hang or panic).
func TestSearchStress_SS6_MaxMatchesZero(t *testing.T) {
	skipIfNoRG(t)
	s := newSession(t)
	writeFile(t, s, "a.txt", "hello\n")
	_, _ = callTool(t, context.Background(), s, "search", map[string]any{
		"query": "hello", "mode": "literal", "max_matches": 0, "reason": "t",
	})
}

// SS7 — trace on a symbol that does not exist anywhere: both sections present,
// no panic.
func TestSearchStress_SS7_TraceNoExist(t *testing.T) {
	skipIfNoRG(t)
	s := newSession(t)
	writeFile(t, s, "a.go", "package x\nfunc Foo() {}\n")
	out, err := callTool(t, context.Background(), s, "search", map[string]any{
		"query": "DOESNOTEXIST_XYZ", "mode": "trace", "reason": "t",
	})
	mustNoErr(t, err)
	mustContain(t, out, "DEFINED:")
	mustContain(t, out, "CALLED FROM:")
}

// SS8 — search across 1000 small files: completes without error.
func TestSearchStress_SS8_ThousandFiles(t *testing.T) {
	skipIfNoRG(t)
	s := newSession(t)
	for i := 0; i < 1000; i++ {
		writeFile(t, s, "f"+sprintf(i)+".txt", "needle\n")
	}
	out, err := callTool(t, context.Background(), s, "search", map[string]any{
		"query": "needle", "mode": "literal", "reason": "t",
	})
	mustNoErr(t, err)
	mustContain(t, out, "needle")
}

// SS9 — concurrent searches on the same tree: no data race.
func TestSearchStress_SS9_ConcurrentSearches(t *testing.T) {
	skipIfNoRG(t)
	s := newSession(t)
	for i := 0; i < 20; i++ {
		writeFile(t, s, "f"+sprintf(i)+".txt", "target\n")
	}
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := callTool(t, context.Background(), s, "search", map[string]any{
				"query": "target", "mode": "literal", "reason": "t",
			})
			if err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// SS10 — rg on a file with a NUL byte (binary detection is rg's job here):
// no panic regardless of what rg decides to do.
func TestSearchStress_SS10_FileWithNULByte(t *testing.T) {
	skipIfNoRG(t)
	s := newSession(t)
	abs := s.Cwd + "/mixed.bin"
	_ = os.WriteFile(abs, []byte("hello\x00world\n"), 0644)
	_, _ = callTool(t, context.Background(), s, "search", map[string]any{
		"query": "hello", "mode": "literal", "reason": "t",
	})
}

// SS11 — globs with multiple patterns: only matching files returned.
func TestSearchStress_SS11_MultiGlob(t *testing.T) {
	skipIfNoRG(t)
	s := newSession(t)
	writeFile(t, s, "a.go", "match\n")
	writeFile(t, s, "b.ts", "match\n")
	writeFile(t, s, "c.py", "match\n")
	out, err := callTool(t, context.Background(), s, "search", map[string]any{
		"query": "match", "mode": "literal",
		"globs": []string{"*.go", "*.ts"}, "reason": "t",
	})
	mustNoErr(t, err)
	mustContain(t, out, "a.go")
	mustContain(t, out, "b.ts")
	mustNotContain(t, out, "c.py")
}

// SS12 — regex with Unicode: correctly matches multibyte chars.
func TestSearchStress_SS12_UnicodeRegex(t *testing.T) {
	skipIfNoRG(t)
	s := newSession(t)
	writeFile(t, s, "u.txt", "héllo 世界\n")
	out, err := callTool(t, context.Background(), s, "search", map[string]any{
		"query": "世界", "mode": "literal", "reason": "t",
	})
	mustNoErr(t, err)
	mustContain(t, out, "世界")
}

// SS13 — parseRgLine: line with no colon-digit-colon boundary returns !ok.
func TestSearchStress_SS13_ParseRgLineNoAnchor(t *testing.T) {
	cases := []string{
		"nodot",
		"file.go",
		"a:b:c",
	}
	for _, in := range cases {
		_, _, _, ok := parseRgLine(in)
		if ok {
			t.Errorf("parseRgLine(%q) = ok, want !ok", in)
		}
	}
}

// SS14 — parseRgLine: file path with multiple colons is parsed by first-digit anchor.
func TestSearchStress_SS14_ParseRgLineColonPath(t *testing.T) {
	// "a:b:10:text" — first :N: anchor is ":10:" so file="a:b", line=10, text="text"
	f, l, txt, ok := parseRgLine("a:b:10:text")
	if !ok {
		t.Fatal("expected ok")
	}
	if f != "a:b" || l != 10 || txt != "text" {
		t.Fatalf("got file=%q line=%d text=%q", f, l, txt)
	}
}

// SS15 — search path override to a subdirectory: only files in that subdir hit.
func TestSearchStress_SS15_PathSubdir(t *testing.T) {
	skipIfNoRG(t)
	s := newSession(t)
	writeFile(t, s, "root.txt", "needle\n")
	writeFile(t, s, "sub/sub.txt", "needle\n")
	out, err := callTool(t, context.Background(), s, "search", map[string]any{
		"query": "needle", "mode": "literal", "path": "sub", "reason": "t",
	})
	mustNoErr(t, err)
	mustContain(t, out, "sub.txt")
	mustNotContain(t, out, "root.txt")
}
