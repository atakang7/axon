package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Background processes are how the agent operates servers, watchers, and
// anything else that should outlive a single foreground exec. Modeled after
// Claude Code's Bash run_in_background + BashOutput + KillShell trio: each
// process gets a stable shell ID, output streams to a per-shell log file,
// and reads return only the bytes added since the last read so the agent
// does not keep redownloading growing logs at full token cost.
//
// State lives in-process (the registry map) and on disk (log files under
// dataDir/bg/<sessionkey>/). The registry is rebuilt fresh per axon run —
// shells do not survive across `axon` restarts. On clean exit and on /new,
// every live shell is killed; we do not leak dev servers across sessions.

type bgShell struct {
	ID         string
	Command    string
	Dir        string
	PID        int
	StartedAt  time.Time
	LogPath    string
	cmd        *exec.Cmd
	logFile    *os.File
	doneCh     chan struct{}
	mu         sync.Mutex
	exitCode   int    // valid only after doneCh closed
	exitNote   string // "exited", "killed", "signaled: ..."
	finished   bool
	readOffset int64 // bytes already returned by bash_output
}

type bgRegistry struct {
	mu     sync.Mutex
	shells map[string]*bgShell
	next   int
	dir    string // log dir for this process
}

var bgReg = newBgRegistry()

func newBgRegistry() *bgRegistry {
	dir := filepath.Join(dataDir(), "bg", fmt.Sprintf("%d", os.Getpid()))
	_ = os.MkdirAll(dir, 0755)
	return &bgRegistry{shells: map[string]*bgShell{}, dir: dir}
}

func (r *bgRegistry) start(command, workdir string) (*bgShell, error) {
	r.mu.Lock()
	r.next++
	id := fmt.Sprintf("bash_%d", r.next)
	r.mu.Unlock()

	logPath := filepath.Join(r.dir, id+".log")
	logFile, err := os.Create(logPath)
	if err != nil {
		return nil, fmt.Errorf("create log: %w", err)
	}

	cmd := exec.Command("sh", "-lc", command)
	if workdir != "" {
		cmd.Dir = workdir
	}
	dn, dnErr := os.Open(os.DevNull)
	if dnErr == nil {
		cmd.Stdin = dn
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	// New process group so we can SIGTERM the whole tree (servers often
	// spawn children — killing only the shell leaves them orphaned).
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		if dn != nil {
			dn.Close()
		}
		logFile.Close()
		return nil, fmt.Errorf("start: %w", err)
	}
	// Once the child has started, the kernel has its own fd for /dev/null;
	// the parent handle is no longer needed and would leak a descriptor per
	// background spawn.
	if dn != nil {
		dn.Close()
	}

	sh := &bgShell{
		ID:        id,
		Command:   command,
		Dir:       workdir,
		PID:       cmd.Process.Pid,
		StartedAt: time.Now(),
		LogPath:   logPath,
		cmd:       cmd,
		logFile:   logFile,
		doneCh:    make(chan struct{}),
	}

	go sh.wait()

	r.mu.Lock()
	r.shells[id] = sh
	r.mu.Unlock()
	return sh, nil
}

func (s *bgShell) wait() {
	err := s.cmd.Wait()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.finished = true
	if err == nil {
		s.exitCode = 0
		s.exitNote = "exited"
	} else if exitErr, ok := err.(*exec.ExitError); ok {
		s.exitCode = exitErr.ExitCode()
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			s.exitNote = "signaled: " + status.Signal().String()
		} else {
			s.exitNote = "exited"
		}
	} else {
		s.exitCode = -1
		s.exitNote = "wait error: " + err.Error()
	}
	s.logFile.Close()
	close(s.doneCh)
}

// readNew returns bytes appended to the log since the last call. The offset
// is per-shell, persistent across calls within this axon run. Returning only
// the delta is the key behavior — without it, the agent rereads the full log
// every poll and spends linear-in-runtime tokens on a watcher.
//
// maxBytes caps the returned chunk; on overflow the tail is kept (most recent
// bytes) and the offset advances past the dropped bytes so the next call
// continues from "now," not from the middle of a backlog. truncated reports
// whether bytes were dropped, so the caller can label the result.
func (s *bgShell) readNew(maxBytes int) (string, bool, error) {
	// Hold the lock for the entire read+advance so concurrent callers cannot
	// both read at the same offset and double-deliver the same bytes.
	s.mu.Lock()
	defer s.mu.Unlock()

	off := s.readOffset
	f, err := os.Open(s.LogPath)
	if err != nil {
		return "", false, err
	}
	defer f.Close()
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return "", false, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return "", false, err
	}

	advance := int64(len(data))
	truncated := false
	if maxBytes > 0 && len(data) > maxBytes {
		data = data[len(data)-maxBytes:]
		truncated = true
	}

	s.readOffset += advance
	return string(data), truncated, nil
}

func (s *bgShell) status() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.finished {
		return "running"
	}
	return fmt.Sprintf("%s (exit %d)", s.exitNote, s.exitCode)
}

// kill sends SIGTERM to the process group, waits up to grace, then SIGKILL.
// Killing the whole group catches children spawned by sh -lc wrappers.
func (s *bgShell) kill(grace time.Duration) error {
	s.mu.Lock()
	if s.finished {
		s.mu.Unlock()
		return nil
	}
	pgid, err := syscall.Getpgid(s.cmd.Process.Pid)
	s.mu.Unlock()
	if err != nil {
		pgid = s.cmd.Process.Pid
	}
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	select {
	case <-s.doneCh:
		return nil
	case <-time.After(grace):
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		<-s.doneCh
		return nil
	}
}

func (r *bgRegistry) get(id string) (*bgShell, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	sh, ok := r.shells[id]
	return sh, ok
}

func (r *bgRegistry) list() []*bgShell {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*bgShell, 0, len(r.shells))
	for _, sh := range r.shells {
		out = append(out, sh)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// killAll terminates every live shell. Called on /new and on process exit so
// background servers do not outlive the session that started them.
func (r *bgRegistry) killAll() {
	for _, sh := range r.list() {
		_ = sh.kill(2 * time.Second)
	}
}

// formatBgStart renders the immediate response after spawning a background
// process. Mirrors the foreground exec format so output blocks look uniform.
func formatBgStart(sh *bgShell) string {
	var b strings.Builder
	fmt.Fprintf(&b, "$ %s &\n", sh.Command)
	if sh.Dir != "" {
		b.WriteString("dir: " + sh.Dir + "\n")
	}
	fmt.Fprintf(&b, "shell_id: %s\n", sh.ID)
	fmt.Fprintf(&b, "pid: %d\n", sh.PID)
	b.WriteString("status: running\n")
	b.WriteString("(use bash_output to read logs, kill_shell to stop)\n")
	return b.String()
}
