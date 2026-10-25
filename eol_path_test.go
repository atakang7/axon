package axon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func crlfFile(t *testing.T, body string) (*Session, string) {
	t.Helper()
	s := newTestSession(t)
	p := filepath.Join(t.TempDir(), "x.js")
	if err := os.WriteFile(p, []byte(strings.ReplaceAll(body, "\n", "\r\n")), 0644); err != nil {
		t.Fatal(err)
	}
	return s, p
}

func assertAllCRLF(t *testing.T, p string) string {
	t.Helper()
	b, _ := os.ReadFile(p)
	got := string(b)
	if strings.Count(got, "\n") != strings.Count(got, "\r\n") {
		t.Fatalf("mixed line endings written:\n%q", got)
	}
	return strings.ReplaceAll(got, "\r\n", "\n")
}

func TestMultiLineReplaceMatchesACRLFFile(t *testing.T) {
	s, p := crlfFile(t, "function a() {\n  return 1;\n}\nfunction b() {}\n")
	// The model copies what read showed it: LF lines.
	if _, err := writeReplaceStringMode(s, p, "function a() {\n  return 1;\n}", "function a() {\n  return 2;\n}"); err != nil {
		t.Fatal(err)
	}
	if got := assertAllCRLF(t, p); got != "function a() {\n  return 2;\n}\nfunction b() {}\n" {
		t.Fatalf("content %q", got)
	}
}

func TestLineEditsKeepCRLF(t *testing.T) {
	s, p := crlfFile(t, "one\ntwo\nthree\n")
	if _, err := writeReplaceLinesMode(s, p, 2, 2, "TWO\nTWO-B"); err != nil {
		t.Fatal(err)
	}
	if _, err := writeInsertAtMode(s, p, 1, "zero"); err != nil {
		t.Fatal(err)
	}
	if got := assertAllCRLF(t, p); got != "zero\none\nTWO\nTWO-B\nthree\n" {
		t.Fatalf("content %q", got)
	}
}

func TestSaveKeepsAnExistingFilesCRLF(t *testing.T) {
	s, p := crlfFile(t, "a\nb\nc\n")
	if _, err := writeSaveMode(s, p, "a\nb\nc\nd\n"); err != nil {
		t.Fatal(err)
	}
	assertAllCRLF(t, p)
}

func TestLFFilesAreUntouchedByEOLHandling(t *testing.T) {
	s := newTestSession(t)
	p := filepath.Join(t.TempDir(), "x.go")
	os.WriteFile(p, []byte("a\nb\n"), 0644)
	if _, err := writeReplaceStringMode(s, p, "a\nb", "A\nB"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "A\nB\n" {
		t.Fatalf("content %q", b)
	}
	// A mostly-LF file with one stray CRLF stays LF.
	if _, crlf := splitEOL("a\nb\nc\r\nd\ne\n"); crlf {
		t.Fatal("a mostly-LF file was treated as CRLF")
	}
}

func TestGitBashPathsResolveOnWindows(t *testing.T) {
	cases := map[string]string{
		"/c/Users/atakan/projects/x.js": `C:\Users\atakan\projects\x.js`,
		"/d":                            `D:\`,
		"/c/":                           `C:\`,
		"/tmp/x":                        "/tmp/x", // not a drive: unchanged
		`C:\already\windows`:            `C:\already\windows`,
		"relative/path":                 "relative/path",
	}
	for in, want := range cases {
		if got := msysPath(in, "windows"); got != want {
			t.Errorf("msysPath(%q) = %q, want %q", in, got, want)
		}
	}
	if got := msysPath("/c/Users", "linux"); got != "/c/Users" {
		t.Errorf("non-Windows paths must not change: %q", got)
	}
}
