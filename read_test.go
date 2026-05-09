package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// R1 — skeleton on Go method receiver. KNOWN-FAIL marker case.
// We want both Foo and Bar visible; current regex misses method receivers.
func TestRead_R1_SkeletonGoMethodReceiver(t *testing.T) {
	s := newSession(t)
	writeFile(t, s, "m.go", "package x\nfunc (r *T) Foo() {}\nfunc Bar() {}\n")
	out, err := callTool(t, context.Background(), s, "read", map[string]any{
		"path": "m.go", "mode": "skeleton", "reason": "test",
	})
	mustNoErr(t, err)
	mustContain(t, out, "Bar")
	if !strings.Contains(out, "Foo") {
		t.Fatalf("known regex gap: skeleton missed method-receiver func Foo. output:\n%s", out)
	}
}

// R2 — skeleton on TS arrow function: at minimum, the export const line should match.
func TestRead_R2_SkeletonTSArrow(t *testing.T) {
	s := newSession(t)
	writeFile(t, s, "a.ts", "export const handler = async (req) => { return 1 }\n")
	out, err := callTool(t, context.Background(), s, "read", map[string]any{
		"path": "a.ts", "mode": "skeleton", "reason": "test",
	})
	mustNoErr(t, err)
	if strings.Contains(out, "[skeleton found no signatures]") {
		t.Fatalf("skeleton missed export const arrow function. output:\n%s", out)
	}
}

// R3 — slice past EOF returns empty range without error.
func TestRead_R3_SlicePastEOF(t *testing.T) {
	s := newSession(t)
	writeFile(t, s, "s.txt", "a\nb\nc\nd\ne\n")
	out, err := callTool(t, context.Background(), s, "read", map[string]any{
		"path": "s.txt", "mode": "slice", "offset": 100, "limit": 10, "reason": "test",
	})
	mustNoErr(t, err)
	mustContain(t, out, "[empty range]")
}

// R4 — slice with offset=0 is coerced to 1.
func TestRead_R4_SliceOffsetZero(t *testing.T) {
	s := newSession(t)
	writeFile(t, s, "s.txt", "a\nb\nc\n")
	out, err := callTool(t, context.Background(), s, "read", map[string]any{
		"path": "s.txt", "mode": "slice", "offset": 0, "limit": 10, "reason": "test",
	})
	mustNoErr(t, err)
	mustContain(t, out, "1\ta")
	mustContain(t, out, "3\tc")
}

// R5 — full read over byte cap is refused with cap message.
func TestRead_R5_FullOverCap(t *testing.T) {
	s := newSession(t)
	t.Setenv("AXON_READ_MAX_BYTES", "1024")
	abs := filepath.Join(s.Cwd, "big.txt")
	// Real text — must be over 1024 bytes but not look binary.
	text := strings.Repeat("hello\n", 1000) // 6000 bytes
	if err := os.WriteFile(abs, []byte(text), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := callTool(t, context.Background(), s, "read", map[string]any{
		"path": "big.txt", "mode": "full", "reason": "test",
	})
	mustNoErr(t, err)
	mustContain(t, out, "full read refused")
	mustContain(t, out, "6000")
}

// R6 — full read on a single very long line under the cap succeeds.
func TestRead_R6_FullSingleLineUnderCap(t *testing.T) {
	s := newSession(t)
	t.Setenv("AXON_READ_MAX_BYTES", "4194304")
	content := strings.Repeat("a", 1500000) + "\n"
	writeFile(t, s, "oneline.txt", content)
	out, err := callTool(t, context.Background(), s, "read", map[string]any{
		"path": "oneline.txt", "mode": "full", "reason": "test",
	})
	mustNoErr(t, err)
	mustContain(t, out, "1\t")
}

// R7 — slice on a single very long line. KNOWN-FAIL marker:
// bufio.Scanner's 1 MiB token limit aborts.
func TestRead_R7_SliceLongLine(t *testing.T) {
	s := newSession(t)
	content := strings.Repeat("a", 1500000) + "\n"
	writeFile(t, s, "oneline.txt", content)
	_, err := callTool(t, context.Background(), s, "read", map[string]any{
		"path": "oneline.txt", "mode": "slice", "offset": 1, "limit": 1, "reason": "test",
	})
	if err != nil {
		t.Fatalf("known weakness: scanner buffer overflow on long line: %v", err)
	}
}

// R8 — binary file (NUL byte) is refused.
func TestRead_R8_BinaryNUL(t *testing.T) {
	s := newSession(t)
	writeFile(t, s, "bin.dat", "AB\x00CD")
	out, err := callTool(t, context.Background(), s, "read", map[string]any{
		"path": "bin.dat", "mode": "full", "reason": "test",
	})
	mustNoErr(t, err)
	mustContain(t, out, "binary file refused")
}

// R9 — binary file with no NUL but high control-byte ratio is refused.
func TestRead_R9_BinaryControlBytes(t *testing.T) {
	s := newSession(t)
	// 200 bytes of 0x01..0x08 — invalid UTF-8 + entirely control bytes.
	buf := make([]byte, 200)
	for i := range buf {
		buf[i] = byte(1 + (i % 8))
	}
	abs := filepath.Join(s.Cwd, "ctrl.bin")
	if err := os.WriteFile(abs, buf, 0644); err != nil {
		t.Fatal(err)
	}
	out, err := callTool(t, context.Background(), s, "read", map[string]any{
		"path": "ctrl.bin", "mode": "full", "reason": "test",
	})
	mustNoErr(t, err)
	mustContain(t, out, "binary file refused")
}

// R10 — UTF-8 multibyte content is NOT refused.
func TestRead_R10_UTF8NotRefused(t *testing.T) {
	s := newSession(t)
	writeFile(t, s, "utf.txt", strings.Repeat("héllo 世界\n", 100))
	out, err := callTool(t, context.Background(), s, "read", map[string]any{
		"path": "utf.txt", "mode": "full", "reason": "test",
	})
	mustNoErr(t, err)
	mustNotContain(t, out, "binary file refused")
	mustContain(t, out, "世界")
}

// R11 — empty file produces no error, no binary refusal.
func TestRead_R11_EmptyFile(t *testing.T) {
	s := newSession(t)
	writeFile(t, s, "empty.txt", "")
	out, err := callTool(t, context.Background(), s, "read", map[string]any{
		"path": "empty.txt", "mode": "full", "reason": "test",
	})
	mustNoErr(t, err)
	mustNotContain(t, out, "binary file refused")
}

// R12 — symlink to a missing target returns an error string, no panic.
func TestRead_R12_DanglingSymlink(t *testing.T) {
	s := newSession(t)
	link := filepath.Join(s.Cwd, "dangling")
	if err := os.Symlink(filepath.Join(s.Cwd, "no_such"), link); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	_, err := callTool(t, context.Background(), s, "read", map[string]any{
		"path": "dangling", "mode": "full", "reason": "test",
	})
	if err == nil {
		t.Fatal("expected error from dangling symlink")
	}
}

// R13 — directory passed as path returns an error, no panic.
func TestRead_R13_DirectoryAsPath(t *testing.T) {
	s := newSession(t)
	if err := os.Mkdir(filepath.Join(s.Cwd, "d"), 0755); err != nil {
		t.Fatal(err)
	}
	_, err := callTool(t, context.Background(), s, "read", map[string]any{
		"path": "d", "mode": "full", "reason": "test",
	})
	if err == nil {
		t.Fatal("expected error reading a directory")
	}
}

// R14 — missing reason yields the standard error.
func TestRead_R14_MissingReason(t *testing.T) {
	s := newSession(t)
	writeFile(t, s, "x.txt", "x\n")
	_, err := callTool(t, context.Background(), s, "read", map[string]any{
		"path": "x.txt", "mode": "full",
	})
	mustErr(t, err, "reason is required")
}

// R15 — path traversal outside session.Cwd is allowed (no jail). Documents
// behavior. Test passes when read succeeds; if a future jail is added, this
// test will start failing and must be updated intentionally.
func TestRead_R15_PathTraversalAllowed(t *testing.T) {
	s := newSession(t)
	outsideDir := t.TempDir()
	target := filepath.Join(outsideDir, "outside.txt")
	if err := os.WriteFile(target, []byte("secret\n"), 0644); err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(s.Cwd, target)
	if err != nil {
		t.Fatal(err)
	}
	out, err := callTool(t, context.Background(), s, "read", map[string]any{
		"path": rel, "mode": "full", "reason": "test",
	})
	mustNoErr(t, err)
	mustContain(t, out, "secret")
}
