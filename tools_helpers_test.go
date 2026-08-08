package axon

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// WriteFileAtomic is the single choke point every write mode goes through, and
// it is the whole basis of the /undo contract: a torn write would mean Undo
// restores garbage. A new file must land at the conventional 0644 so scripts
// the agent writes are not silently created world-writable or unreadable.
func TestWriteFileAtomicCreatesNewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new.txt")

	if err := WriteFileAtomic(path, []byte("hello")); err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("content = %q, want %q", data, "hello")
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if fi.Mode().Perm() != 0644 {
		t.Fatalf("mode = %o, want 0644", fi.Mode().Perm())
	}
}

// A rewrite must not silently change a file's mode. If it did, every edit to
// an executable script (a git hook, a build wrapper) would quietly strip its
// execute bit and break the next invocation with no error anywhere.
func TestWriteFileAtomicPreservesExistingMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "script.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho old\n"), 0755); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	if err := WriteFileAtomic(path, []byte("#!/bin/sh\necho new\n")); err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if fi.Mode().Perm() != 0755 {
		t.Fatalf("mode after rewrite = %o, want 0755 preserved", fi.Mode().Perm())
	}
}

// A leftover .axon-write-* temp file is a symptom the rename-based atomicity
// is leaking: it means either the happy path forgets to clean up, or a failure
// path bails out before removing its scratch file, both of which would litter
// a workspace the agent is editing.
func TestWriteFileAtomicLeavesNoTempFiles(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "f.txt")
		if err := WriteFileAtomic(path, []byte("x")); err != nil {
			t.Fatalf("WriteFileAtomic: %v", err)
		}
		assertNoTempFiles(t, dir)
	})

	t.Run("failure", func(t *testing.T) {
		dir := t.TempDir()
		// Make the destination itself a non-empty directory: the rename onto
		// it fails (a file cannot be renamed over a directory), which forces
		// WriteFileAtomic down its rename-failure cleanup path after the tmp
		// file already exists.
		target := filepath.Join(dir, "target")
		if err := os.MkdirAll(target, 0755); err != nil {
			t.Fatalf("mkdir target: %v", err)
		}
		if err := os.WriteFile(filepath.Join(target, "inner.txt"), []byte("x"), 0644); err != nil {
			t.Fatalf("seed inner file: %v", err)
		}

		if err := WriteFileAtomic(target, []byte("y")); err == nil {
			t.Fatal("WriteFileAtomic succeeded renaming a file onto an existing non-empty directory")
		}
		assertNoTempFiles(t, dir)
	})
}

func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".axon-write-") {
			t.Fatalf("leftover temp file: %s", e.Name())
		}
	}
}

// A destination whose parent directory does not exist must fail outright
// rather than attempt a partial write — there is nowhere for the tmp file to
// even land.
func TestWriteFileAtomicMissingParentDirErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "does", "not", "exist", "f.txt")

	if err := WriteFileAtomic(path, []byte("x")); err == nil {
		t.Fatal("WriteFileAtomic succeeded into a missing parent dir")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("partial write landed at %s", path)
	}
}

// binaryFileRefusal is the gate that stops a compiled binary or an image from
// being paged straight into the model's context at full token cost. A NUL
// byte is the strongest, cheapest signal, so it must trip the refusal on its
// own regardless of anything else in the sample.
func TestBinaryFileRefusalNUL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bin.dat")
	content := append([]byte("PK\x03\x04"), 0)
	content = append(content, []byte("trailer")...)
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	msg, refused := binaryFileRefusal(path)
	if !refused {
		t.Fatal("NUL byte did not trigger refusal")
	}
	if !strings.Contains(msg, path) {
		t.Fatalf("refusal message missing path: %q", msg)
	}
	if !strings.Contains(msg, strconv.Itoa(len(content))) {
		t.Fatalf("refusal message missing byte size %d: %q", len(content), msg)
	}
}

// Some binary formats (certain compressed streams) contain no NUL in their
// header, so a high ratio of control bytes has to be its own trigger — without
// it those formats would sail through as "text".
func TestBinaryFileRefusalControlByteRatio(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ctrl.dat")
	// 4 control bytes (0x01) in 20 total = 20%, comfortably over the 10%
	// threshold, no NUL anywhere, and still valid UTF-8 (0x01 is valid ASCII).
	content := []byte("\x01\x01\x01\x01aaaaaaaaaaaaaaaa")
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	if _, refused := binaryFileRefusal(path); !refused {
		t.Fatal(">10% control bytes with no NUL should be refused")
	}
}

// Invalid UTF-8 alone is not enough (Latin-1 text is invalid UTF-8 and is
// legitimate text); it only trips the refusal combined with at least one
// control byte, which is the signature of an actual binary stream.
func TestBinaryFileRefusalInvalidUTF8PlusControlBytes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mixed.dat")
	// 0x80 alone is not valid UTF-8 continuation-without-lead-byte; paired
	// with one control byte (0x01) at a ratio well under 10%, this only trips
	// the "invalid UTF-8 AND any control bytes" branch.
	content := append([]byte{0x01, 0x80}, []byte(strings.Repeat("a", 100))...)
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	if _, refused := binaryFileRefusal(path); !refused {
		t.Fatal("invalid UTF-8 plus a control byte should be refused")
	}
}

// The refusal must stay out of the way of ordinary text — including the
// control bytes (tab, CR, LF) that appear in totally normal files — an empty
// file, and a path that does not exist (read returns before any sniffing).
func TestBinaryFileRefusalPassesTextThrough(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]string{
		"plain.txt": "hello, world\nsecond line\n",
		"tabs.txt":  "col1\tcol2\r\nrow1\trow2\r\n",
		"empty.txt": "",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, name)
			if err := os.WriteFile(path, []byte(content), 0644); err != nil {
				t.Fatalf("seed file: %v", err)
			}
			if _, refused := binaryFileRefusal(path); refused {
				t.Fatalf("%s: plain text was refused as binary", name)
			}
		})
	}

	t.Run("nonexistent", func(t *testing.T) {
		if _, refused := binaryFileRefusal(filepath.Join(dir, "missing.txt")); refused {
			t.Fatal("a nonexistent path should not be refused (os.Open fails, sniff never runs)")
		}
	})
}

// limitBuf is what stands between a runaway command and an unbounded output
// string. It must still report the full byte count to the writer even past
// the cap: os/exec's Write contract requires n == len(p) on success, and
// returning a short count makes exec.Cmd.Run itself fail with a spurious
// "short write" error instead of the command's real exit status.
func TestLimitBufCapsAndReportsFullWriteCount(t *testing.T) {
	b := &limitBuf{limit: 5}

	n, err := b.Write([]byte("hello world"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != len("hello world") {
		t.Fatalf("n = %d, want %d (full len(p), or os/exec treats this as a short write)", n, len("hello world"))
	}

	got, truncated := b.snapshot()
	if got != "hello" {
		t.Fatalf("snapshot = %q, want %q", got, "hello")
	}
	if !truncated {
		t.Fatal("truncated should be true once the cap is exceeded")
	}

	// A further write past the cap keeps reporting truncated and the buffer
	// does not grow past the limit.
	n2, err := b.Write([]byte("more"))
	if err != nil || n2 != 4 {
		t.Fatalf("second write: n=%d err=%v, want n=4 err=nil", n2, err)
	}
	got2, truncated2 := b.snapshot()
	if got2 != "hello" || !truncated2 {
		t.Fatalf("snapshot after second write = (%q, %v), want (%q, true)", got2, truncated2, "hello")
	}
}

// os/exec writes into limitBuf from an internal copier goroutine that can
// still be running after a command is killed, concurrently with the caller
// reading the result. This is a live data race, not a theoretical one — the
// comment on limitBuf says so explicitly — so it must survive -race with
// concurrent writers and a concurrent reader.
func TestLimitBufConcurrentWritesAndSnapshotRace(t *testing.T) {
	b := &limitBuf{limit: 1024}
	var wg sync.WaitGroup

	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				b.Write([]byte("xyz"))
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 100; j++ {
			b.snapshot()
		}
	}()
	wg.Wait()

	got, _ := b.snapshot()
	if len(got) > 1024 {
		t.Fatalf("snapshot len = %d, want <= limit 1024", len(got))
	}
}

// tailN is what keeps a chatty command's output bounded to what the model
// asked for, while still telling it how much was hidden so it knows to ask
// for more if the hidden part matters.
func TestTailN(t *testing.T) {
	t.Run("keeps last N lines and reports hidden count", func(t *testing.T) {
		out, hidden := tailN("a\nb\nc\nd\n", 2)
		if out != "c\nd" {
			t.Fatalf("out = %q, want %q", out, "c\nd")
		}
		if hidden != 2 {
			t.Fatalf("hidden = %d, want 2", hidden)
		}
	})

	t.Run("input at or under N lines is returned unchanged with hidden 0", func(t *testing.T) {
		in := "a\nb"
		out, hidden := tailN(in, 5)
		if out != in {
			t.Fatalf("out = %q, want unchanged %q", out, in)
		}
		if hidden != 0 {
			t.Fatalf("hidden = %d, want 0", hidden)
		}
	})
}

// Catalog() is what the model reads to learn its tool surface; the actual
// tool constructors are what agent/setup.go binds. If the two ever drift —
// someone renames a tool's Name in its constructor but forgets Catalog(), or
// vice versa — the prompt would describe a tool the model can never
// successfully call. This compares Catalog() against the names the
// constructors in this package actually produce, not against a second
// hardcoded list that could drift right alongside it.
func TestCatalogMatchesActualToolNames(t *testing.T) {
	ws := newFakeWorkspace(t.TempDir())
	lim := testLimits()
	shells := NewBackgroundShells()
	defer shells.KillAll()
	plan := &fakePlan{}

	built := []Tool{
		ReadTool(ws, lim),
		WriteTool(ws),
		ExecTool(ws, shells, lim),
		BashOutputTool(shells, lim),
		KillShellTool(shells),
		SearchTool(ws, lim),
		TaskTool(plan),
	}

	catalog := Catalog()
	re := regexp.MustCompile(`"([a-z_]+)"`)
	found := re.FindAllStringSubmatch(catalog, -1)

	cataloged := make(map[string]int, len(found))
	for _, m := range found {
		cataloged[m[1]]++
	}

	seen := map[string]bool{}
	for _, tool := range built {
		if seen[tool.Name] {
			t.Fatalf("constructor list has duplicate name %q", tool.Name)
		}
		seen[tool.Name] = true
		if cataloged[tool.Name] != 1 {
			t.Fatalf("Catalog() names %q %d times, want exactly 1 (built tool names: %v)", tool.Name, cataloged[tool.Name], seen)
		}
	}
	if len(cataloged) != len(built) {
		t.Fatalf("Catalog() lists %d names, want %d matching the constructors: %v", len(cataloged), len(built), cataloged)
	}
}
