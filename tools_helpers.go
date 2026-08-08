package axon

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"unicode/utf8"
)

// tools_helpers.go — shared helpers used by every tool: atomic writes,
// binary-file refusal, output capping.

// ---------------------------------------------------------------------------
// Shared utilities
// ---------------------------------------------------------------------------

// WriteFileAtomic writes atomically: tmp file in the same dir, then rename. The
// rename is atomic on POSIX, so a crash mid-write never leaves a half-written
// file at `path` — readers see either the old contents or the new, never a
// truncated mix. Same-dir tmp matters: cross-filesystem renames degrade to
// copy+unlink and lose the atomicity guarantee.
//
// File mode handling: if the destination already exists, its mode is preserved
// (executable bits, group-readable scripts, etc.). New files default to 0644.
// Callers that want the file formatted afterwards run a formatter themselves;
// the agent can simply call gofmt through exec.
func WriteFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if dir == "" {
		dir = "."
	}
	mode := os.FileMode(0644)
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		mode = info.Mode().Perm()
	}
	tmp, err := os.CreateTemp(dir, ".axon-write-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		cleanup()
		return err
	}
	return nil
}

// binaryFileRefusal sniffs the first 8KB of path for binary indicators. NUL
// bytes are the strong signal (compiled executables, archives, images). We
// also flag content that is not valid UTF-8 and has a high ratio of control
// bytes (excluding tab/CR/LF) — that catches binary formats that happen to
// have no NULs in their header, like some compressed streams.
//
// Refuse up front with a one-line message that names size, so the model can
// decide whether to use a deliberate tool (strings, hexdump via exec) instead.
func binaryFileRefusal(abs string) (string, bool) {
	f, err := os.Open(abs)
	if err != nil {
		return "", false
	}
	defer f.Close()
	var buf [8192]byte
	n, _ := f.Read(buf[:])
	if n == 0 {
		return "", false
	}
	sample := buf[:n]
	hasNUL := false
	ctrl := 0
	for _, b := range sample {
		switch {
		case b == 0:
			hasNUL = true
		case b < 0x09, b == 0x0B, b == 0x0C, (b > 0x0D && b < 0x20), b == 0x7F:
			ctrl++
		}
	}
	// Decision tree:
	//   NUL byte                                → binary (strong signal).
	//   ctrl-byte ratio > 10% of sample         → binary. Bytes like
	//       0x01..0x08 are technically valid single-byte ASCII per
	//       utf8.Valid, so we cannot rely on UTF-8 validity alone — a
	//       sample full of 0x01..0x08 still passes utf8.Valid but is
	//       obviously not text.
	//   invalid UTF-8 AND any ctrl bytes        → binary. Catches binary
	//       streams that happen to be sub-10% control but are clearly not
	//       text encodings.
	// Files that are valid UTF-8 with no control bytes — including Latin-1
	// content that happens to also be valid UTF-8 — pass through.
	binary := hasNUL || ctrl*100 > n*10 || (!utf8.Valid(sample) && ctrl > 0)
	if !binary {
		return "", false
	}
	fi, _ := f.Stat()
	size := int64(-1)
	if fi != nil {
		size = fi.Size()
	}
	return fmt.Sprintf("[binary file refused: %s (%d bytes). Loading binary content into context burns tokens with no value. If you need bytes, use exec with `strings`, `xxd`, or `file` deliberately.]", abs, size), true
}

// limitBuf is a write-capped buffer used to bound tool output size.
//
// It is mutex-guarded because os/exec writes into it from an internal copier
// goroutine. When a command is killed on timeout that goroutine can still be
// running while the caller reads the result, so an unguarded bytes.Buffer here
// is a live data race, not a theoretical one.
type limitBuf struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (b *limitBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	remaining := b.limit - b.buf.Len()
	if remaining <= 0 {
		b.truncated = true
		return len(p), nil
	}
	if len(p) > remaining {
		b.truncated = true
		b.buf.Write(p[:remaining])
		return len(p), nil
	}
	return b.buf.Write(p)
}

// snapshot returns what has been captured so far, and whether anything was
// dropped for exceeding the cap. Safe to call while writes are still arriving.
func (b *limitBuf) snapshot() (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String(), b.truncated
}
