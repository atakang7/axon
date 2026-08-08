package axon

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
// dataDir/bg//). Shells do not survive a restart. On clean exit and on
// reset, every live shell is killed; we do not leak dev servers.
//
// OWNERSHIP: a BackgroundShells registry belongs to one Agent, which creates
// it at construction and kills it on Close. It used to be a package-level
// global, which meant two Agents in the same process shared one registry and
// either one's Close or Reset terminated the other's servers. Nothing here is
// ambient any more: if you can reach a shell, someone handed you the registry.
type bgShell struct {
	ID        string
	Command   string
	Dir       string
	PID       int
	StartedAt time.Time
	LogPath   string

	cmd     *exec.Cmd
	logFile *os.File
	doneCh  chan struct{}

	mu       sync.Mutex
	exitCode int    // valid only after doneCh closed
	exitNote string // "exited", "killed", "signaled: ..."
	finished bool

	readOffset int64 // bytes already returned by bash_output

	// Identity of the log file seen by the previous readNew call.
	// Offset alone cannot detect replacement when the new file is larger
	// than the old one, so track device + inode as well.
	logDev uint64
	logIno uint64
}

type BackgroundShells struct {
	mu     sync.Mutex
	shells map[string]*bgShell
	next   int
	dir    string // log directory owned exclusively by this registry
}

// NewBackgroundShells creates an empty registry with its own log directory.
//
// The directory is allocated uniquely rather than derived from the PID: shell
// IDs restart at bash_1 in every registry, so two registries in one process
// sharing a directory would write both their bash_1 logs to the same file and
// each would serve the other's output to its agent.
func NewBackgroundShells() *BackgroundShells {
	root := BackgroundLogRoot(os.Getpid())
	if err := os.MkdirAll(root, 0755); err != nil {
		// Fall back to the system temp dir rather than failing construction:
		// losing background-shell logging is survivable, refusing to build an
		// agent is not.
		root = os.TempDir()
	}

	dir, err := os.MkdirTemp(root, "shells-")
	if err != nil {
		dir = root
	}

	return &BackgroundShells{
		shells: map[string]*bgShell{},
		dir:    dir,
	}
}

// LogDir is the directory this registry writes shell logs to. Exposed for
// tests asserting that two registries never share storage.
func (r *BackgroundShells) LogDir() string {
	return r.dir
}

func (r *BackgroundShells) start(command, workdir string) (*bgShell, error) {
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
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	if err := cmd.Start(); err != nil {
		if dn != nil {
			_ = dn.Close()
		}
		_ = logFile.Close()
		return nil, fmt.Errorf("start: %w", err)
	}

	// Once the child has started, the kernel has its own fd for /dev/null;
	// the parent handle is no longer needed and would leak a descriptor per
	// background spawn.
	if dn != nil {
		_ = dn.Close()
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

	_ = s.logFile.Close()
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
//
// If the underlying log is truncated or replaced, reading restarts from the
// beginning of the new file. File identity is tracked using device + inode
// because comparing size against readOffset alone cannot detect a replacement
// whose new contents happen to be larger than the old file.
func (s *bgShell) readNew(maxBytes int) (string, bool, error) {
	// Hold the lock for the entire read+advance so concurrent callers cannot
	// both read at the same offset and double-deliver the same bytes.
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.Open(s.LogPath)
	if err != nil {
		return "", false, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return "", false, err
	}

	size := fi.Size()
	off := s.readOffset

	// Detect replacement of the log file.
	//
	// Size alone is insufficient. For example:
	//
	//     old file size = 10, readOffset = 10
	//     new file size = 30
	//
	// min(readOffset, size) would incorrectly start at byte 10 of the
	// completely new file. Device + inode identify whether this is still
	// the same filesystem object.
	if stat, ok := fi.Sys().(*syscall.Stat_t); ok {
		dev := uint64(stat.Dev)
		ino := uint64(stat.Ino)

		if s.logIno != 0 && (s.logDev != dev || s.logIno != ino) {
			off = 0
		}

		s.logDev = dev
		s.logIno = ino
	}

	// Same file, but shortened in place.
	//
	// The previous offset no longer refers to valid unread data, so restart
	// from the beginning of the truncated file.
	if size < off {
		off = 0
	}

	truncated := false

	// Seek past the backlog rather than reading it and throwing it away.
	// A watcher left running between two polls can append hundreds of
	// megabytes; reading that in full to return the last 32KB of it would
	// defeat the cap and could exhaust memory on its own.
	if maxBytes > 0 && size-off > int64(maxBytes) {
		off = size - int64(maxBytes)
		truncated = true
	}

	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return "", false, err
	}

	data := make([]byte, size-off)

	n, err := io.ReadFull(f, data)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return "", false, err
	}

	// Advance by what was actually read, not by size: the writer may have
	// appended since Stat, and those bytes belong to the next poll.
	s.readOffset = off + int64(n)

	return string(data[:n]), truncated, nil
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

func (r *BackgroundShells) get(id string) (*bgShell, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	sh, ok := r.shells[id]
	return sh, ok
}

func (r *BackgroundShells) list() []*bgShell {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]*bgShell, 0, len(r.shells))

	for _, sh := range r.shells {
		out = append(out, sh)
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})

	return out
}

// KillAll terminates every live shell. Called on /new and on process exit so
// background servers do not outlive the session that started them.
func (r *BackgroundShells) KillAll() {
	var wg sync.WaitGroup

	for _, sh := range r.list() {
		wg.Add(1)

		go func(s *bgShell) {
			defer wg.Done()
			_ = s.kill(2 * time.Second)
		}(sh)
	}

	wg.Wait()
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
