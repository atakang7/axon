package main

// Test suite layout
//
// These tests stay in the same package directory as the production code on
// purpose: they exercise package-private helpers and runtime wiring such as
// Agent.Run, BuildTools, ContextMessages, and the SSE client stream parser.
// A separate external test package or sibling test folder would lose access
// to those unexported integration points.

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return false }

func tmpSession(t *testing.T) *Session {
	t.Helper()
	dir := t.TempDir()
	s := &Session{ID: "test", path: filepath.Join(dir, "session.json"), Cwd: dir}
	s.ensure()
	return s
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func requireCommand(t *testing.T, name string) {
	t.Helper()
	if _, err := osexec.LookPath(name); err != nil {
		t.Skipf("%s not available in PATH: %v", name, err)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		data, _ := io.ReadAll(r)
		done <- string(data)
	}()
	defer func() {
		os.Stdout = old
	}()

	fn()

	_ = w.Close()
	out := <-done
	_ = r.Close()
	return out
}

func writeProvidersFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "providers.json")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func newSSEServer(t *testing.T, handler func(call int, w http.ResponseWriter, r *http.Request)) (*httptest.Server, *int32) {
	t.Helper()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := int(atomic.AddInt32(&calls, 1))
		handler(call, w, r)
	}))
	return srv, &calls
}

func sanitizeTestName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	repl := strings.NewReplacer("/", "_", " ", "_", ":", "_", "\t", "_", "\n", "_")
	return repl.Replace(s)
}
