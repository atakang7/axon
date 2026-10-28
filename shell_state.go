package axon

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// shellState carries the working directory and the exported variables from
// one foreground command to the next, so `cd` and `export` behave as they do
// in a terminal. Without it every command started from scratch: a traced run
// on 2026-10-24 began 70 of its 228 commands with the same `export ...`
// preamble, and `cd x && ...` had to be repeated on every call.
//
// Each command still runs in its own process; the state lives in two files the
// command reads at start and writes on exit. Only bash supports this (it is
// the foreground shell wherever it is installed, Git Bash on Windows).
type shellState struct {
	mu  sync.Mutex
	dir string // where the state files live; "" disables persistence
	cwd string // where the last command ended, inside the workspace
}

func (s *shellState) envFile() string { return filepath.Join(s.dir, "env.sh") }
func (s *shellState) cwdFile() string { return filepath.Join(s.dir, "cwd") }

// enabled reports whether commands carry state between them.
func (s *shellState) enabled() bool { return s != nil && s.dir != "" && persistentShell() }

// startDir is where the next command starts: where the last one ended, when
// that is still inside the workspace, else the workspace itself.
func (s *shellState) startDir(workspace string) string {
	if !s.enabled() {
		return workspace
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cwd == "" || !insideDir(s.cwd, workspace) {
		return workspace
	}
	if info, err := os.Stat(s.cwd); err != nil || !info.IsDir() {
		return workspace
	}
	return s.cwd
}

// wrap returns command with the state prelude and the exit hook that saves it.
func (s *shellState) wrap(command string) string {
	if !s.enabled() {
		return command
	}
	env, cwd := shellQuote(filepath.ToSlash(s.envFile())), shellQuote(filepath.ToSlash(s.cwdFile()))
	return "[ -f " + env + " ] && . " + env + " >/dev/null 2>&1\n" +
		"__axon_save() { __axon_rc=$?; export -p > " + env + ".tmp 2>/dev/null && mv -f " + env + ".tmp " + env + "; " +
		"{ pwd -W 2>/dev/null || pwd; } > " + cwd + " 2>/dev/null; return $__axon_rc; }\n" +
		"trap __axon_save EXIT\n" + command
}

// settle records where the command ended. It returns that directory when it
// differs from where the command started, for the result to mention.
func (s *shellState) settle(started, workspace string) string {
	if !s.enabled() {
		return ""
	}
	data, err := os.ReadFile(s.cwdFile())
	if err != nil {
		return ""
	}
	ended := filepath.Clean(strings.TrimSpace(string(data)))
	if ended == "." || ended == "" {
		return ""
	}
	if !insideDir(ended, workspace) {
		ended = workspace
	}
	s.mu.Lock()
	s.cwd = ended
	s.mu.Unlock()
	if sameDir(ended, started) {
		return ""
	}
	return ended
}

func insideDir(path, root string) bool {
	if root == "" {
		return true
	}
	rel, err := filepath.Rel(canonicalDir(root), canonicalDir(path))
	if runtime.GOOS == "windows" && err == nil {
		// Rel is case-sensitive; Windows paths are not.
		if r2, err2 := filepath.Rel(strings.ToLower(canonicalDir(root)), strings.ToLower(canonicalDir(path))); err2 == nil {
			rel = r2
		}
	}
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// sameDir compares directories as the platform does: case-insensitively on
// Windows, exactly elsewhere.
func sameDir(a, b string) bool {
	a, b = canonicalDir(a), canonicalDir(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// canonicalDir resolves links and, on Windows, 8.3 short names, so that the
// workspace as the process sees it (C:\Users\RUNNER~1\...) and the directory
// bash reports (C:/Users/runneradmin/...) compare equal.
func canonicalDir(p string) string {
	p = filepath.Clean(p)
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	if a, err := filepath.Abs(p); err == nil {
		p = a
	}
	return p
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
