package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// requireRipgrep skips a test outright when rg is not on PATH, so the suite
// stays portable to environments that never installed it, while still
// running for real everywhere rg is actually present.
func requireRipgrep(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg not on PATH")
	}
}

// A blank query has nothing to search for and must fail before rg ever runs.
func TestSearchBlankQueryErrors(t *testing.T) {
	requireRipgrep(t)
	ws := newFakeWorkspace(t.TempDir())
	tool := SearchTool(ws, testLimits())

	_, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{"query": ""}))
	if err == nil {
		t.Fatal("blank query should error")
	}
}

// mode defaults to literal — the safer, more predictable choice for a model
// that has not deliberately opted into regex semantics — and an unrecognised
// mode must error.
func TestSearchDefaultModeIsLiteralAndUnknownModeErrors(t *testing.T) {
	requireRipgrep(t)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("a.c\nabc\n"), 0644)
	ws := newFakeWorkspace(dir)
	tool := SearchTool(ws, testLimits())

	out, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{"query": "a.c"}))
	if err != nil {
		t.Fatalf("Fn: %v", err)
	}
	if strings.Contains(out, "abc") {
		t.Fatalf("default mode matched abc for query a.c — not literal: %q", out)
	}
	if !strings.Contains(out, "a.c") {
		t.Fatalf("literal query a.c did not match its exact text: %q", out)
	}

	_, err = tool.Fn(context.Background(), mustJSON(t, map[string]any{"query": "x", "mode": "bogus"}))
	if err == nil {
		t.Fatal("unknown mode should error")
	}
}

// literal must treat regex metacharacters as plain text — a search for "a.c"
// must not accidentally match "abc" the way an unescaped regex dot would.
func TestSearchLiteralTreatsMetacharactersLiterally(t *testing.T) {
	requireRipgrep(t)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("only abc here\n"), 0644)
	ws := newFakeWorkspace(dir)
	tool := SearchTool(ws, testLimits())

	out, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{"query": "a.c", "mode": "literal"}))
	if err != nil {
		t.Fatalf("Fn: %v", err)
	}
	if !strings.Contains(out, "no matches") {
		t.Fatalf("literal a.c matched abc: %q", out)
	}
}

// regex mode must actually apply the pattern rather than treating it as
// fixed text.
func TestSearchRegexMatchesByPattern(t *testing.T) {
	requireRipgrep(t)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("func Foo() {}\nfunc barBaz() {}\n"), 0644)
	ws := newFakeWorkspace(dir)
	tool := SearchTool(ws, testLimits())

	out, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{
		"query": `func [A-Z]\w+`, "mode": "regex", "case_sensitive": true,
	}))
	if err != nil {
		t.Fatalf("Fn: %v", err)
	}
	if !strings.Contains(out, "func Foo") {
		t.Fatalf("regex did not match func Foo: %q", out)
	}
	if strings.Contains(out, "barBaz") {
		t.Fatalf("regex matched a line it should not have: %q", out)
	}
}

// Default is case-insensitive (the more forgiving default for exploratory
// search); case_sensitive:true must flip that off.
func TestSearchCaseSensitivity(t *testing.T) {
	requireRipgrep(t)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("Hello World\n"), 0644)
	ws := newFakeWorkspace(dir)
	tool := SearchTool(ws, testLimits())

	out, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{"query": "hello"}))
	if err != nil {
		t.Fatalf("Fn: %v", err)
	}
	if !strings.Contains(out, "Hello World") {
		t.Fatalf("case-insensitive default missed the match: %q", out)
	}

	out, err = tool.Fn(context.Background(), mustJSON(t, map[string]any{"query": "hello", "case_sensitive": true}))
	if err != nil {
		t.Fatalf("Fn: %v", err)
	}
	if !strings.Contains(out, "no matches") {
		t.Fatalf("case_sensitive:true still matched a different-case string: %q", out)
	}
}

// globs must filter by extension, and a blank glob in the list must be
// dropped rather than passed to rg (an empty -g argument is at best a no-op
// and at worst a footgun).
func TestSearchGlobsFilterByExtension(t *testing.T) {
	requireRipgrep(t)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "f.go"), []byte("needle\n"), 0644)
	os.WriteFile(filepath.Join(dir, "f.md"), []byte("needle\n"), 0644)
	ws := newFakeWorkspace(dir)
	tool := SearchTool(ws, testLimits())

	out, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{
		"query": "needle", "globs": []string{"*.go", ""},
	}))
	if err != nil {
		t.Fatalf("Fn: %v", err)
	}
	if !strings.Contains(out, "f.go") {
		t.Fatalf("glob filter excluded the matching extension: %q", out)
	}
	if strings.Contains(out, "f.md") {
		t.Fatalf("glob filter did not exclude f.md: %q", out)
	}
}

// path defaults to "." and is honoured when given; results run with
// cmd.Dir = ws.Dir(), so a relative search root is resolved against the
// workspace, not the process's own cwd.
func TestSearchPathDefaultAndHonoured(t *testing.T) {
	requireRipgrep(t)
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "sub"), 0755)
	os.WriteFile(filepath.Join(dir, "top.txt"), []byte("needle\n"), 0644)
	os.WriteFile(filepath.Join(dir, "sub", "nested.txt"), []byte("needle\n"), 0644)
	ws := newFakeWorkspace(dir)
	tool := SearchTool(ws, testLimits())

	out, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{"query": "needle"}))
	if err != nil {
		t.Fatalf("Fn: %v", err)
	}
	if !strings.Contains(out, "top.txt") || !strings.Contains(out, "nested.txt") {
		t.Fatalf("default path=. did not search the whole workspace: %q", out)
	}

	out, err = tool.Fn(context.Background(), mustJSON(t, map[string]any{"query": "needle", "path": "sub"}))
	if err != nil {
		t.Fatalf("Fn: %v", err)
	}
	if strings.Contains(out, "top.txt") {
		t.Fatalf("path=sub leaked results from outside the given root: %q", out)
	}
	if !strings.Contains(out, "nested.txt") {
		t.Fatalf("path=sub missed a match inside it: %q", out)
	}
}

// .git must be excluded (searching version-control internals is never
// useful) while ordinary dotfiles must still be searched — a search that
// silently skipped .env or .github would miss real content.
func TestSearchExcludesGitButIncludesDotfiles(t *testing.T) {
	requireRipgrep(t)
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".git"), 0755)
	os.WriteFile(filepath.Join(dir, ".git", "config"), []byte("needle\n"), 0644)
	os.WriteFile(filepath.Join(dir, ".envrc"), []byte("needle\n"), 0644)
	ws := newFakeWorkspace(dir)
	tool := SearchTool(ws, testLimits())

	out, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{"query": "needle"}))
	if err != nil {
		t.Fatalf("Fn: %v", err)
	}
	if strings.Contains(out, ".git/config") {
		t.Fatalf(".git was not excluded: %q", out)
	}
	if !strings.Contains(out, ".envrc") {
		t.Fatalf("a dotfile outside .git was not searched: %q", out)
	}
}

// rg's exit status 1 means "ran fine, found nothing" — a result, not a
// failure — so it must come back as a nil error with an explicit "no
// matches" marker the model can read plainly rather than an ambiguous empty
// string.
func TestSearchNoMatchesIsNilErrorResult(t *testing.T) {
	requireRipgrep(t)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("nothing relevant\n"), 0644)
	ws := newFakeWorkspace(dir)
	tool := SearchTool(ws, testLimits())

	out, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{"query": "zzz_not_there"}))
	if err != nil {
		t.Fatalf("expected nil error on no matches, got: %v", err)
	}
	if !strings.Contains(out, "no matches") {
		t.Fatalf("out = %q, want a no-matches marker", out)
	}
}

// max_matches caps how many results come back; omitted, it must fall back to
// the configured default rather than returning everything unbounded.
func TestSearchMaxMatchesCapsAndDefaults(t *testing.T) {
	requireRipgrep(t)
	dir := t.TempDir()
	var lines []string
	for i := 0; i < 20; i++ {
		lines = append(lines, "needle")
	}
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte(strings.Join(lines, "\n")+"\n"), 0644)
	ws := newFakeWorkspace(dir)

	lim := testLimits()
	lim.SearchMaxMatches = 3
	tool := SearchTool(ws, lim)

	out, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{"query": "needle"}))
	if err != nil {
		t.Fatalf("Fn: %v", err)
	}
	// Count rg's own match lines ("f.txt:N:needle"), not raw occurrences of
	// "needle" in the whole output — the header echoes the query itself
	// ("query: needle"), which would otherwise inflate the count by one.
	if got := strings.Count(out, "f.txt:"); got != 3 {
		t.Fatalf("default max_matches: got %d match lines, want 3 (Limits.SearchMaxMatches)", got)
	}

	out, err = tool.Fn(context.Background(), mustJSON(t, map[string]any{"query": "needle", "max_matches": 7}))
	if err != nil {
		t.Fatalf("Fn: %v", err)
	}
	if got := strings.Count(out, "f.txt:"); got != 7 {
		t.Fatalf("explicit max_matches: got %d match lines, want 7", got)
	}
}

// Output over the configured byte cap must be truncated with an explicit
// marker — an unbounded search result on a huge match set would otherwise
// blow the model's context budget on a single tool call.
func TestSearchOutputOverByteCapTruncates(t *testing.T) {
	requireRipgrep(t)
	dir := t.TempDir()
	var lines []string
	for i := 0; i < 500; i++ {
		lines = append(lines, "needle padding padding padding padding padding")
	}
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte(strings.Join(lines, "\n")+"\n"), 0644)
	ws := newFakeWorkspace(dir)

	lim := testLimits()
	lim.SearchOutputBytes = 200
	lim.SearchMaxMatches = 100000
	tool := SearchTool(ws, lim)

	out, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{"query": "needle"}))
	if err != nil {
		t.Fatalf("Fn: %v", err)
	}
	if !strings.Contains(out, "[output truncated]") {
		t.Fatalf("output over the byte cap was not marked truncated: len=%d", len(out))
	}
}

// A search that cannot finish within SearchTimeout must fail with a plain
// "search timed out" error rather than surfacing whatever raw error rg
// itself produced when killed.
func TestSearchTimeoutReturnsOwnError(t *testing.T) {
	requireRipgrep(t)
	dir := t.TempDir()
	// A large file with no matches forces rg to scan to the end; combined
	// with an aggressively short timeout, this reliably exceeds it without
	// depending on external timing.
	var b strings.Builder
	for i := 0; i < 200000; i++ {
		b.WriteString("no match on this line at all, just padding text\n")
	}
	os.WriteFile(filepath.Join(dir, "big.txt"), []byte(b.String()), 0644)
	ws := newFakeWorkspace(dir)

	lim := testLimits()
	lim.SearchTimeout = 1 * time.Nanosecond
	tool := SearchTool(ws, lim)

	_, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{"query": "zzz_never_matches"}))
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if !strings.Contains(err.Error(), "search timed out") {
		t.Fatalf("error = %v, want the search-timed-out message, not rg's raw error", err)
	}
}

// A cancelled parent context must surface ctx.Err() — distinct from the
// SearchTimeout path — so a caller cancelling a whole turn (Ctrl-C) gets its
// own cancellation reason back, not a generic timeout message.
func TestSearchParentCancellationReturnsCtxErr(t *testing.T) {
	requireRipgrep(t)
	dir := t.TempDir()
	var b strings.Builder
	for i := 0; i < 200000; i++ {
		b.WriteString("no match on this line at all, just padding text\n")
	}
	os.WriteFile(filepath.Join(dir, "big.txt"), []byte(b.String()), 0644)
	ws := newFakeWorkspace(dir)

	lim := testLimits()
	lim.SearchTimeout = 5 * time.Second
	tool := SearchTool(ws, lim)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := tool.Fn(ctx, mustJSON(t, map[string]any{"query": "zzz_never_matches"}))
	if err == nil {
		t.Fatal("expected an error from a pre-cancelled context")
	}
	if err != context.Canceled {
		t.Fatalf("error = %v, want context.Canceled surfaced directly", err)
	}
}

// With rg missing from PATH, runRg must fail with the specific errNoRipgrep
// sentinel — a generic "executable file not found" would leave the model
// unable to tell a missing dependency from an unrelated command failure.
func TestSearchMissingRipgrepReturnsSentinel(t *testing.T) {
	dir := t.TempDir()
	ws := newFakeWorkspace(dir)
	lim := testLimits()

	t.Setenv("PATH", t.TempDir()) // a PATH with no rg on it anywhere

	_, _, err := runRg(context.Background(), ws, lim, []string{"--version"})
	if err != errNoRipgrep {
		t.Fatalf("err = %v, want errNoRipgrep", err)
	}
}

// The result header always names the query and the search path, regardless
// of whether anything matched — the model needs that context to interpret
// the result without re-reading its own tool call.
func TestSearchHeaderNamesQueryAndPath(t *testing.T) {
	requireRipgrep(t)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x\n"), 0644)
	ws := newFakeWorkspace(dir)
	tool := SearchTool(ws, testLimits())

	out, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{"query": "needle_absent", "path": "."}))
	if err != nil {
		t.Fatalf("Fn: %v", err)
	}
	if !strings.Contains(out, "query: needle_absent") || !strings.Contains(out, "path: .") {
		t.Fatalf("header missing query/path: %q", out)
	}
}
