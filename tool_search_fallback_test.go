package axon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fallbackRepo is a tree with source, a vendored copy, a binary and a big line.
func fallbackRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"auth/token.go":         "package auth\n\nfunc Expired(exp, now int64) bool {\n\treturn exp > now\n}\n",
		"auth/token_test.go":    "package auth\n\n// Expired is tested here\n",
		"web/app.js":            "function expired() { return true }\n",
		"node_modules/x/x.go":   "func Expired() {}\n",
		".git/config":           "Expired\n",
		"bin/tool.exe":          "Expired\x00\x01",
		"docs/notes.md":         "EXPIRED in caps\n",
		"auth/generated_big.go": "// " + strings.Repeat("Expired ", 200) + "\n",
	}
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(p), 0755)
		if err := os.WriteFile(p, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func runFallback(t *testing.T, dir string, p searchInput, literal bool) string {
	t.Helper()
	if p.Path == "" {
		p.Path = "."
	}
	out, err := searchFallback(context.Background(), newFakeWorkspace(dir), testLimits(), p, literal)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestBuiltInSearchFindsWhatRgWouldAndSaysWhichEngineRan(t *testing.T) {
	dir := fallbackRepo(t)
	out := runFallback(t, dir, searchInput{Query: "Expired"}, true)
	for _, want := range []string{"auth/token.go:3:func Expired(exp, now int64) bool {", "auth/token_test.go:3:", "docs/notes.md:1:EXPIRED in caps", "rg is not installed"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	for _, never := range []string{"node_modules", ".git/config", "tool.exe"} {
		if strings.Contains(out, never) {
			t.Fatalf("searched %s:\n%s", never, out)
		}
	}
	if strings.Contains(out, strings.Repeat("Expired ", 100)) {
		t.Fatal("a very long line was not capped")
	}
}

func TestBuiltInSearchHonoursCaseRegexGlobsAndLimits(t *testing.T) {
	dir := fallbackRepo(t)
	if out := runFallback(t, dir, searchInput{Query: "EXPIRED", CaseSensitive: true}, true); strings.Contains(out, "token.go") || !strings.Contains(out, "notes.md") {
		t.Fatalf("case-sensitive literal:\n%s", out)
	}
	if out := runFallback(t, dir, searchInput{Query: `func\s+Expired\(`}, false); !strings.Contains(out, "auth/token.go:3:") || strings.Contains(out, "app.js") {
		t.Fatalf("regex:\n%s", out)
	}
	if out := runFallback(t, dir, searchInput{Query: "expired", Globs: []string{"*.js"}}, true); !strings.Contains(out, "web/app.js") || strings.Contains(out, "token.go") {
		t.Fatalf("include glob:\n%s", out)
	}
	if out := runFallback(t, dir, searchInput{Query: "Expired", Globs: []string{"!*_test.go", "!docs/**"}}, true); strings.Contains(out, "token_test.go") || strings.Contains(out, "notes.md") || !strings.Contains(out, "auth/token.go") {
		t.Fatalf("exclude globs:\n%s", out)
	}
	if out := runFallback(t, dir, searchInput{Query: "nothing like this"}, true); !strings.Contains(out, "no matches") {
		t.Fatalf("no match:\n%s", out)
	}
	if _, err := searchFallback(context.Background(), newFakeWorkspace(dir), testLimits(), searchInput{Query: "(", Path: "."}, false); err == nil {
		t.Fatal("an invalid regex was accepted")
	}
	lim := testLimits()
	lim.SearchOutputBytes = 120
	out, err := searchFallback(context.Background(), newFakeWorkspace(dir), lim, searchInput{Query: "e", Path: "."}, true)
	if err != nil || !strings.Contains(out, "[output truncated]") {
		t.Fatalf("output cap: %v\n%s", err, out)
	}
}

func TestSearchToolFallsBackWhenRgIsMissing(t *testing.T) {
	dir := fallbackRepo(t)
	t.Setenv("PATH", t.TempDir())
	out, err := SearchTool(newFakeWorkspace(dir), testLimits()).Fn(context.Background(), mustJSON(t, map[string]any{"query": "Expired", "path": "auth"}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "auth/token.go:3:") || strings.Contains(out, "web/app.js") {
		t.Fatalf("search without rg:\n%s", out)
	}
}
