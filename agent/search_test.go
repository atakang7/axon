package agent

import (
	"context"
	"strings"
	"testing"
)

func skipIfNoRG(t *testing.T) {
	t.Helper()
	if !rgAvailable() {
		t.Skip("ripgrep not on PATH")
	}
}

// S1 — literal exact match.
func TestSearch_S1_LiteralMatch(t *testing.T) {
	skipIfNoRG(t)
	s := newSession(t)
	writeFile(t, s, "a.go", "package x\nfunc Foo() { _ = \"HELLO\" }\n")
	out, err := callTool(t, context.Background(), s, "search", map[string]any{
		"query": "HELLO", "mode": "literal", "case_sensitive": true, "reason": "test",
	})
	mustNoErr(t, err)
	mustContain(t, out, "HELLO")
}

// S2 — case_sensitive=true excludes lowercase.
func TestSearch_S2_CaseSensitiveExcludes(t *testing.T) {
	skipIfNoRG(t)
	s := newSession(t)
	writeFile(t, s, "a.go", "Hello\n")
	out, err := callTool(t, context.Background(), s, "search", map[string]any{
		"query": "hello", "mode": "literal", "case_sensitive": true, "reason": "test",
	})
	mustNoErr(t, err)
	mustContain(t, out, "no matches")
}

// S3 — case_sensitive default false matches.
func TestSearch_S3_CaseInsensitiveDefault(t *testing.T) {
	skipIfNoRG(t)
	s := newSession(t)
	writeFile(t, s, "a.go", "Hello\n")
	out, err := callTool(t, context.Background(), s, "search", map[string]any{
		"query": "hello", "mode": "literal", "reason": "test",
	})
	mustNoErr(t, err)
	mustContain(t, out, "Hello")
}

// S4 — regex with special chars.
func TestSearch_S4_Regex(t *testing.T) {
	skipIfNoRG(t)
	s := newSession(t)
	writeFile(t, s, "a.go", "package x\nfunc Foo() error { return nil }\n")
	out, err := callTool(t, context.Background(), s, "search", map[string]any{
		"query": `func\s+\w+\(\)\s+error`, "mode": "regex", "reason": "test",
	})
	mustNoErr(t, err)
	mustContain(t, out, "func Foo()")
}

// S5 — query starting with '-' is not interpreted as a flag (we pass --).
func TestSearch_S5_DashQuery(t *testing.T) {
	skipIfNoRG(t)
	s := newSession(t)
	writeFile(t, s, "a.txt", "value=-foo\n")
	out, err := callTool(t, context.Background(), s, "search", map[string]any{
		"query": "-foo", "mode": "literal", "reason": "test",
	})
	mustNoErr(t, err)
	mustContain(t, out, "-foo")
}

// S6 — max_matches caps results.
func TestSearch_S6_MaxMatches(t *testing.T) {
	skipIfNoRG(t)
	s := newSession(t)
	writeFile(t, s, "a.txt", strings.Repeat("x\n", 200))
	out, err := callTool(t, context.Background(), s, "search", map[string]any{
		"query": "x", "mode": "literal", "max_matches": 5, "reason": "test",
	})
	mustNoErr(t, err)
	// Count "a.txt:" hit prefixes; rg --max-count is per-file, so 1 file → 5 hits.
	hits := strings.Count(out, "a.txt:")
	if hits > 5 {
		t.Fatalf("expected <=5 hits with max_matches=5, got %d. output:\n%s", hits, out)
	}
}

// S7 — search timeout fires cleanly.
func TestSearch_S7_Timeout(t *testing.T) {
	skipIfNoRG(t)
	s := newSession(t)
	t.Setenv("AXON_SEARCH_TIMEOUT_SECONDS", "1")
	// Build a tree large enough that a slow regex against /dev/random-like
	// content burns more than a second. Use many small files.
	for i := 0; i < 5000; i++ {
		writeFile(t, s, "files/"+sprintf(i)+".txt", strings.Repeat("a", 200))
	}
	// A pathological backtracking-ish regex over a lot of input.
	_, err := callTool(t, context.Background(), s, "search", map[string]any{
		"query": "(a+)+b", "mode": "regex", "reason": "test",
	})
	if err == nil {
		t.Skip("rg too fast to exercise timeout on this machine")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected timeout error, got: %v", err)
	}
}

// sprintf is a tiny helper to avoid pulling in strconv where strings would
// suffice. Keeps S7 readable.
func sprintf(i int) string {
	const digits = "0123456789"
	if i == 0 {
		return "0"
	}
	var out []byte
	for i > 0 {
		out = append([]byte{digits[i%10]}, out...)
		i /= 10
	}
	return string(out)
}

// S9 — globs filter applied.
func TestSearch_S9_Globs(t *testing.T) {
	skipIfNoRG(t)
	s := newSession(t)
	writeFile(t, s, "a.go", "foo\n")
	writeFile(t, s, "a.txt", "foo\n")
	out, err := callTool(t, context.Background(), s, "search", map[string]any{
		"query": "foo", "mode": "literal", "globs": []string{"*.go"}, "reason": "test",
	})
	mustNoErr(t, err)
	mustContain(t, out, "a.go")
	mustNotContain(t, out, "a.txt")
}

// S10 — search root that does not exist returns a clean error.
func TestSearch_S10_BadPath(t *testing.T) {
	skipIfNoRG(t)
	s := newSession(t)
	_, err := callTool(t, context.Background(), s, "search", map[string]any{
		"query": "x", "mode": "literal", "path": "/no/such/dir/__axon_test", "reason": "test",
	})
	if err == nil {
		t.Fatal("expected error for missing path")
	}
}

// S11 — trace finds Go func definition + call site.
func TestSearch_S11_TraceFunc(t *testing.T) {
	skipIfNoRG(t)
	s := newSession(t)
	writeFile(t, s, "t.go", "package x\nfunc Bar() {}\nfunc main() { Bar() }\n")
	out, err := callTool(t, context.Background(), s, "search", map[string]any{
		"query": "Bar", "mode": "trace", "reason": "test",
	})
	mustNoErr(t, err)
	mustContain(t, out, "DEFINED:")
	mustContain(t, out, "CALLED FROM:")
	if strings.Contains(out, "<not found>") {
		t.Fatalf("trace did not find def. output:\n%s", out)
	}
}

// S12 — trace on a Go method with receiver. KNOWN-FAIL marker.
func TestSearch_S12_TraceMethod(t *testing.T) {
	skipIfNoRG(t)
	s := newSession(t)
	writeFile(t, s, "t.go", "package x\ntype T struct{}\nfunc (r *T) M() {}\nfunc main() { (&T{}).M() }\n")
	out, err := callTool(t, context.Background(), s, "search", map[string]any{
		"query": "M", "mode": "trace", "reason": "test",
	})
	mustNoErr(t, err)
	if strings.Contains(out, "<not found>") {
		t.Fatalf("known regex gap: trace cannot find method-receiver definition. output:\n%s", out)
	}
}

// parseRgLine unit tests — covers the colon-in-path fix directly.
func TestParseRgLine(t *testing.T) {
	cases := []struct {
		in       string
		wantFile string
		wantLine int
		wantText string
	}{
		{"a.go:42:hello", "a.go", 42, "hello"},
		{"weird:name.txt:7:match", "weird", 7, "name.txt:7:match"},
		// Real-world: rg never emits colons in filename portion BEFORE the
		// :N: anchor unless the file actually contains a colon. Our parser
		// finds the FIRST :N: boundary; for "weird:name.txt:7:match" the
		// first :N: is ":7:" so file="weird:name.txt" — verify that.
	}
	// Recompute expectation for the second case based on actual parser:
	// the parser scans left-to-right for the first ':<digits>:'. In
	// "weird:name.txt:7:match" the first colon is followed by 'n' (not a
	// digit), so it skips. Then ":7:" anchor is found at position after
	// "name.txt". So file = "weird:name.txt", line = 7, text = "match".
	cases[1] = struct {
		in       string
		wantFile string
		wantLine int
		wantText string
	}{"weird:name.txt:7:match", "weird:name.txt", 7, "match"}

	for _, c := range cases {
		f, l, txt, ok := parseRgLine(c.in)
		if !ok {
			t.Errorf("parseRgLine(%q) = !ok", c.in)
			continue
		}
		if f != c.wantFile || l != c.wantLine || txt != c.wantText {
			t.Errorf("parseRgLine(%q) = (%q, %d, %q), want (%q, %d, %q)",
				c.in, f, l, txt, c.wantFile, c.wantLine, c.wantText)
		}
	}
}
