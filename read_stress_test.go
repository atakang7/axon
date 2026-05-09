package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// RS1 — full read exactly at the byte cap boundary (cap-1, cap, cap+1).
func TestReadStress_RS1_ExactByteBoundary(t *testing.T) {
	for _, tc := range []struct {
		name  string
		extra int
		want  string
	}{
		{"at_cap_minus_1", -1, "1\t"},
		{"at_cap", 0, "1\t"},
		{"at_cap_plus_1", +1, "full read refused"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newSession(t)
			// cap is 64 bytes, content is one long line
			cap := 64
			t.Setenv("AXON_READ_MAX_BYTES", "64")
			content := strings.Repeat("a", cap+tc.extra)
			writeFile(t, s, "f.txt", content)
			out, err := callTool(t, context.Background(), s, "read", map[string]any{
				"path": "f.txt", "mode": "full", "reason": "t",
			})
			mustNoErr(t, err)
			mustContain(t, out, tc.want)
		})
	}
}

// RS2 — slice: offset > 1<<31 does not panic (integer overflow guard).
func TestReadStress_RS2_HugeOffset(t *testing.T) {
	s := newSession(t)
	writeFile(t, s, "f.txt", "a\nb\nc\n")
	out, err := callTool(t, context.Background(), s, "read", map[string]any{
		"path": "f.txt", "mode": "slice", "offset": 1<<31 - 1, "limit": 1, "reason": "t",
	})
	mustNoErr(t, err)
	mustContain(t, out, "[empty range]")
}

// RS3 — slice: limit=0 is coerced to the default limit, so all lines are returned.
// Documents that limit=0 is NOT an "empty read" — callers must use a large offset.
func TestReadStress_RS3_LimitZeroCoercedToDefault(t *testing.T) {
	s := newSession(t)
	writeFile(t, s, "f.txt", "a\nb\nc\n")
	out, err := callTool(t, context.Background(), s, "read", map[string]any{
		"path": "f.txt", "mode": "slice", "offset": 1, "limit": 0, "reason": "t",
	})
	mustNoErr(t, err)
	// limit=0 is replaced with readLimit() which returns all content.
	mustContain(t, out, "1\ta")
	mustContain(t, out, "3\tc")
}

// RS4 — slice: negative limit does not loop forever.
func TestReadStress_RS4_NegativeLimit(t *testing.T) {
	s := newSession(t)
	writeFile(t, s, "f.txt", "a\nb\nc\n")
	// Should error or return empty range — never block.
	out, _ := callTool(t, context.Background(), s, "read", map[string]any{
		"path": "f.txt", "mode": "slice", "offset": 1, "limit": -1, "reason": "t",
	})
	// Any response is acceptable — we just need no hang and no panic.
	_ = out
}

// RS5 — full: file with only a single newline byte.
func TestReadStress_RS5_SingleNewline(t *testing.T) {
	s := newSession(t)
	writeFile(t, s, "nl.txt", "\n")
	out, err := callTool(t, context.Background(), s, "read", map[string]any{
		"path": "nl.txt", "mode": "full", "reason": "t",
	})
	mustNoErr(t, err)
	mustNotContain(t, out, "binary file refused")
}

// RS6 — full: file with ONLY a carriage-return (bare \r, no \n) — not binary.
func TestReadStress_RS6_BareCarriageReturn(t *testing.T) {
	s := newSession(t)
	abs := filepath.Join(s.Cwd, "cr.txt")
	if err := os.WriteFile(abs, []byte("a\rb\rc\r"), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := callTool(t, context.Background(), s, "read", map[string]any{
		"path": "cr.txt", "mode": "full", "reason": "t",
	})
	mustNoErr(t, err)
	mustNotContain(t, out, "binary file refused")
}

// RS7 — binary detection: a file that is exactly 8192 bytes of NUL (boundary
// of the probe read).
func TestReadStress_RS7_ProbeExact8192NUL(t *testing.T) {
	s := newSession(t)
	abs := filepath.Join(s.Cwd, "probe.bin")
	if err := os.WriteFile(abs, make([]byte, 8192), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := callTool(t, context.Background(), s, "read", map[string]any{
		"path": "probe.bin", "mode": "full", "reason": "t",
	})
	mustNoErr(t, err)
	mustContain(t, out, "binary file refused")
}

// RS8 — binary detection: 8191 text bytes followed by one NUL (NUL is at edge).
func TestReadStress_RS8_NULatEdgeOfProbe(t *testing.T) {
	s := newSession(t)
	buf := make([]byte, 8192)
	for i := 0; i < 8191; i++ {
		buf[i] = 'a'
	}
	buf[8191] = 0x00
	abs := filepath.Join(s.Cwd, "edge.bin")
	if err := os.WriteFile(abs, buf, 0644); err != nil {
		t.Fatal(err)
	}
	out, err := callTool(t, context.Background(), s, "read", map[string]any{
		"path": "edge.bin", "mode": "full", "reason": "t",
	})
	mustNoErr(t, err)
	mustContain(t, out, "binary file refused")
}

// RS9 — binary detection: exactly 10% ctrl bytes (boundary — should NOT refuse).
func TestReadStress_RS9_CtrlRatioAtBoundary(t *testing.T) {
	s := newSession(t)
	// 100 bytes, 10 of which are 0x01 (ctrl) = exactly 10%.
	// The check is ctrl*100 > n*10 → 10*100 > 100*10 → 1000 > 1000 → false.
	// So 10% exactly should NOT be refused.
	buf := make([]byte, 100)
	for i := 0; i < 90; i++ {
		buf[i] = 'a'
	}
	for i := 90; i < 100; i++ {
		buf[i] = 0x01
	}
	abs := filepath.Join(s.Cwd, "edge.bin")
	if err := os.WriteFile(abs, buf, 0644); err != nil {
		t.Fatal(err)
	}
	out, err := callTool(t, context.Background(), s, "read", map[string]any{
		"path": "edge.bin", "mode": "full", "reason": "t",
	})
	mustNoErr(t, err)
	mustNotContain(t, out, "binary file refused")
}

// RS10 — binary detection: 10% + 1 ctrl bytes (just over boundary — MUST refuse).
func TestReadStress_RS10_CtrlRatioJustOverBoundary(t *testing.T) {
	s := newSession(t)
	// 100 bytes, 11 ctrl = 11% > 10%.
	buf := make([]byte, 100)
	for i := 0; i < 89; i++ {
		buf[i] = 'a'
	}
	for i := 89; i < 100; i++ {
		buf[i] = 0x01
	}
	abs := filepath.Join(s.Cwd, "over.bin")
	if err := os.WriteFile(abs, buf, 0644); err != nil {
		t.Fatal(err)
	}
	out, err := callTool(t, context.Background(), s, "read", map[string]any{
		"path": "over.bin", "mode": "full", "reason": "t",
	})
	mustNoErr(t, err)
	mustContain(t, out, "binary file refused")
}

// RS11 — concurrent reads of the same file do not corrupt output.
func TestReadStress_RS11_ConcurrentSameFile(t *testing.T) {
	s := newSession(t)
	writeFile(t, s, "shared.txt", strings.Repeat("hello\n", 100))
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := callTool(t, context.Background(), s, "read", map[string]any{
				"path": "shared.txt", "mode": "full", "reason": "t",
			})
			if err != nil {
				errs <- err
				return
			}
			if !strings.Contains(out, "hello") {
				errs <- &errorf{"concurrent read: output corrupted: %q", out}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// RS12 — skeleton on a file with 10000 functions: no hang, no OOM.
func TestReadStress_RS12_SkeletonManyFunctions(t *testing.T) {
	s := newSession(t)
	var sb strings.Builder
	sb.WriteString("package x\n")
	for i := 0; i < 10000; i++ {
		sb.WriteString("func F")
		for _, d := range []int{i / 1000 % 10, i / 100 % 10, i / 10 % 10, i % 10} {
			sb.WriteByte(byte('0' + d))
		}
		sb.WriteString("() {}\n")
	}
	writeFile(t, s, "big.go", sb.String())
	out, err := callTool(t, context.Background(), s, "read", map[string]any{
		"path": "big.go", "mode": "skeleton", "reason": "t",
	})
	mustNoErr(t, err)
	mustContain(t, out, "F0000")
	mustContain(t, out, "F9999")
}

// RS13 — slice: window larger than file returns all lines without padding.
func TestReadStress_RS13_SliceLargerThanFile(t *testing.T) {
	s := newSession(t)
	writeFile(t, s, "f.txt", "a\nb\nc\n")
	out, err := callTool(t, context.Background(), s, "read", map[string]any{
		"path": "f.txt", "mode": "slice", "offset": 1, "limit": 1000000, "reason": "t",
	})
	mustNoErr(t, err)
	mustContain(t, out, "1\ta")
	mustContain(t, out, "3\tc")
}

// RS14 — file shrinks between stat and read (TOCTOU): no panic.
func TestReadStress_RS14_FileShrinksDuringRead(t *testing.T) {
	s := newSession(t)
	abs := filepath.Join(s.Cwd, "race.txt")
	content := strings.Repeat("line\n", 10000)
	if err := os.WriteFile(abs, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	// Truncate the file in a goroutine while the read is in flight.
	go func() {
		_ = os.Truncate(abs, 0)
	}()
	// Either succeeds or returns a clean error — must not panic.
	_, _ = callTool(t, context.Background(), s, "read", map[string]any{
		"path": "race.txt", "mode": "full", "reason": "t",
	})
}

// RS15 — path is a NUL byte in the middle of the string.
func TestReadStress_RS15_PathWithNULByte(t *testing.T) {
	s := newSession(t)
	_, err := callTool(t, context.Background(), s, "read", map[string]any{
		"path": "foo\x00bar", "mode": "full", "reason": "t",
	})
	// Must return an error, not panic.
	if err == nil {
		t.Fatal("expected error for path with NUL byte")
	}
}

// RS16 — full read: file larger than 8192 bytes is still detected as binary if
// it has a NUL in the first probe window.
func TestReadStress_RS16_LargeFileWithNULinProbe(t *testing.T) {
	s := newSession(t)
	buf := make([]byte, 100000)
	for i := 0; i < len(buf); i++ {
		buf[i] = 'a'
	}
	buf[100] = 0x00 // NUL inside first 8192 bytes
	abs := filepath.Join(s.Cwd, "big.bin")
	if err := os.WriteFile(abs, buf, 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AXON_READ_MAX_BYTES", "1000000")
	out, err := callTool(t, context.Background(), s, "read", map[string]any{
		"path": "big.bin", "mode": "full", "reason": "t",
	})
	mustNoErr(t, err)
	mustContain(t, out, "binary file refused")
}

type errorf struct {
	msg  string
	args interface{}
}

func (e *errorf) Error() string { return e.msg }
