package axon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Two registries sharing a log directory was the actual bug that motivated
// per-registry directories: bash_1 in one agent's registry would silently
// collide with bash_1 in another's, and each would serve the other's output.
// Every registry must allocate its own directory.
func TestNewBackgroundShellsAllocatesOwnLogDir(t *testing.T) {
	t.Setenv("AXON_DATA_DIR", t.TempDir())

	a := NewBackgroundShells()
	defer a.KillAll()
	b := NewBackgroundShells()
	defer b.KillAll()

	if a.LogDir() == b.LogDir() {
		t.Fatalf("two registries share a log dir: %s", a.LogDir())
	}
	if a.LogDir() == "" || b.LogDir() == "" {
		t.Fatal("LogDir() must not be empty")
	}
}

// Shell IDs are per-registry and restart at 1 in a fresh registry — that is
// what makes the collision bug above possible if the log directory were ever
// shared again, so both halves of the contract are pinned here.
func TestBackgroundShellIDsPerRegistry(t *testing.T) {
	t.Setenv("AXON_DATA_DIR", t.TempDir())
	shells := NewBackgroundShells()
	defer shells.KillAll()

	sh1, err := shells.start("true", "")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	sh2, err := shells.start("true", "")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if sh1.ID != "bash_1" || sh2.ID != "bash_2" {
		t.Fatalf("IDs = %s, %s, want bash_1, bash_2", sh1.ID, sh2.ID)
	}

	fresh := NewBackgroundShells()
	defer fresh.KillAll()
	sh3, err := fresh.start("true", "")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if sh3.ID != "bash_1" {
		t.Fatalf("a fresh registry's first shell ID = %s, want bash_1", sh3.ID)
	}
}

// The immediate response after spawning a background command is the model's
// only synchronous feedback that the spawn worked; it must carry everything
// needed to follow up (which shell to read/kill, where it's running).
func TestFormatBgStart(t *testing.T) {
	t.Setenv("AXON_DATA_DIR", t.TempDir())
	shells := NewBackgroundShells()
	defer shells.KillAll()

	sh, err := shells.start("true", "/tmp")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	out := formatBgStart(sh)
	for _, want := range []string{"true", "dir: /tmp", "shell_id: bash_1", "status: running"} {
		if !strings.Contains(out, want) {
			t.Fatalf("formatBgStart missing %q: %q", want, out)
		}
	}
}

// bash_output against an unknown shell_id must error — there is nothing to
// read and pretending otherwise would hide a model bug (typo'd ID, reused ID
// after a KillAll) behind a silent empty result.
func TestBashOutputUnknownShellIDErrors(t *testing.T) {
	t.Setenv("AXON_DATA_DIR", t.TempDir())
	shells := NewBackgroundShells()
	defer shells.KillAll()
	tool := BashOutputTool(shells, testLimits())

	_, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{"shell_id": "bash_99"}))
	if err == nil {
		t.Fatal("expected error for an unknown shell_id")
	}
}

// The delta contract is the entire point of bash_output: two consecutive
// reads must never return the same bytes, and an idle shell's second read
// must say so plainly rather than returning an empty string that looks like
// an error.
func TestBashOutputDeltaNeverRepeats(t *testing.T) {
	t.Setenv("AXON_DATA_DIR", t.TempDir())
	shells := NewBackgroundShells()
	defer shells.KillAll()
	tool := BashOutputTool(shells, testLimits())

	// Use an explicit release file instead of sleep-based timing. The shell
	// cannot emit "second" until the test releases it, so a slow CI machine
	// cannot make this test race against the clock.
	workdir := t.TempDir()
	sh, err := shells.start(`echo first; while [ ! -f release ]; do sleep 0.01; done; echo second`, workdir)
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	waitForLog(t, sh, "first")
	out1, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{"shell_id": sh.ID}))
	if err != nil {
		t.Fatalf("Fn: %v", err)
	}
	if !strings.Contains(out1, "first") {
		t.Fatalf("first read missing 'first': %q", out1)
	}

	// The shell is blocked on the release file, so there is deterministically
	// no new output here.
	out2, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{"shell_id": sh.ID}))
	if err != nil {
		t.Fatalf("Fn: %v", err)
	}
	if strings.Contains(out2, "first") {
		t.Fatal("second read repeated bytes already delivered by the first read")
	}
	if !strings.Contains(out2, "(no new output)") {
		t.Fatalf("idle re-read missing the no-new-output marker: %q", out2)
	}

	if err := os.WriteFile(filepath.Join(workdir, "release"), []byte("go"), 0644); err != nil {
		t.Fatalf("release shell: %v", err)
	}
	waitForLog(t, sh, "second")

	out3, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{"shell_id": sh.ID}))
	if err != nil {
		t.Fatalf("Fn: %v", err)
	}
	if strings.Contains(out3, "first") {
		t.Fatal("third read repeated 'first' again")
	}
	if !strings.Contains(out3, "second") {
		t.Fatalf("third read missing the newly-appeared 'second': %q", out3)
	}
	waitForExit(t, sh)
}

// status must read "running" while the process is alive and switch to an
// exit summary once it finishes — this is how the model knows a long job is
// actually done without having to guess from output content.
func TestBashOutputStatusTransitionsOnExit(t *testing.T) {
	t.Setenv("AXON_DATA_DIR", t.TempDir())
	shells := NewBackgroundShells()
	defer shells.KillAll()
	tool := BashOutputTool(shells, testLimits())

	workdir := t.TempDir()
	sh, err := shells.start(`echo ready; while [ ! -f release ]; do sleep 0.01; done`, workdir)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitForLog(t, sh, "ready")

	running, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{"shell_id": sh.ID}))
	if err != nil {
		t.Fatalf("Fn while running: %v", err)
	}
	if !strings.Contains(running, "status: running") {
		t.Fatalf("out = %q, want status: running", running)
	}

	if err := os.WriteFile(filepath.Join(workdir, "release"), []byte("go"), 0644); err != nil {
		t.Fatalf("release shell: %v", err)
	}
	waitForExit(t, sh)

	exited, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{"shell_id": sh.ID}))
	if err != nil {
		t.Fatalf("Fn after exit: %v", err)
	}
	if !strings.Contains(exited, "status: exited (exit 0)") {
		t.Fatalf("out = %q, want status: exited (exit 0)", exited)
	}
}

// A shell killed by a signal (as opposed to exiting on its own) must report
// which signal, distinct from a plain exit — the model needs to tell "the
// command finished" from "something killed it".
func TestBashOutputStatusSignaled(t *testing.T) {
	t.Setenv("AXON_DATA_DIR", t.TempDir())
	shells := NewBackgroundShells()
	defer shells.KillAll()

	sh, err := shells.start("echo ready; sleep 30", "")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitForLog(t, sh, "ready")
	if err := sh.kill(2 * time.Second); err != nil {
		t.Fatalf("kill: %v", err)
	}

	if !strings.Contains(sh.status(), "signaled:") {
		t.Fatalf("status = %q, want a signaled: summary after SIGTERM", sh.status())
	}
}

// max_bytes must cap the delta, keep the tail (most recent bytes, not the
// oldest), and still advance the offset past whatever was dropped — the
// comment on readNew is explicit that this is what stops a poll loop from
// re-reading a stale backlog forever.
func TestBashOutputMaxBytesKeepsTailAndAdvancesOffset(t *testing.T) {
	t.Setenv("AXON_DATA_DIR", t.TempDir())
	shells := NewBackgroundShells()
	defer shells.KillAll()
	tool := BashOutputTool(shells, testLimits())

	sh, err := shells.start("printf 'AAAAA'; printf 'BBBBB'", "")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitForExit(t, sh)

	out, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{"shell_id": sh.ID, "max_bytes": 5}))
	if err != nil {
		t.Fatalf("Fn: %v", err)
	}
	if !strings.Contains(out, "BBBBB") {
		t.Fatalf("out = %q, want the tail (BBBBB) kept", out)
	}
	if strings.Contains(out, "AAAAA") {
		t.Fatalf("out = %q, want the dropped head (AAAAA) absent", out)
	}
	if !strings.Contains(out, "[earlier delta bytes dropped") {
		t.Fatalf("out = %q, missing the bytes-dropped marker", out)
	}

	// The offset must have advanced past the dropped bytes: a follow-up read
	// must not re-deliver anything, not even the dropped head.
	out2, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{"shell_id": sh.ID}))
	if err != nil {
		t.Fatalf("Fn: %v", err)
	}
	if !strings.Contains(out2, "(no new output)") {
		t.Fatalf("out2 = %q, want no new output — offset should have advanced past the drop, not resumed from the backlog", out2)
	}
}

// Omitting max_bytes must fall back to Limits.BashOutputMaxBytes rather than
// returning everything unbounded — a chatty server with no cap on its poll
// loop would otherwise dump megabytes into context on one call.
func TestBashOutputMaxBytesDefaultsToLimit(t *testing.T) {
	t.Setenv("AXON_DATA_DIR", t.TempDir())
	shells := NewBackgroundShells()
	defer shells.KillAll()

	lim := testLimits()
	lim.BashOutputMaxBytes = 4
	tool := BashOutputTool(shells, lim)

	sh, err := shells.start("printf '0123456789'", "")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitForExit(t, sh)

	out, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{"shell_id": sh.ID}))
	if err != nil {
		t.Fatalf("Fn: %v", err)
	}
	if !strings.Contains(out, "6789") {
		t.Fatalf("out = %q, want the last 4 bytes (Limits.BashOutputMaxBytes) kept", out)
	}
}

// tail_lines trims the delta to the last N lines and reports how many were
// dropped, independent of the byte cap — the two knobs solve different
// problems (byte budget vs. "just show me the recent lines of a log").
func TestBashOutputTailLinesTrimsDelta(t *testing.T) {
	t.Setenv("AXON_DATA_DIR", t.TempDir())
	shells := NewBackgroundShells()
	defer shells.KillAll()
	tool := BashOutputTool(shells, testLimits())

	sh, err := shells.start(`printf 'l1\nl2\nl3\nl4\n'`, "")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitForExit(t, sh)

	out, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{"shell_id": sh.ID, "tail_lines": 2}))
	if err != nil {
		t.Fatalf("Fn: %v", err)
	}
	if !strings.Contains(out, "l3") || !strings.Contains(out, "l4") {
		t.Fatalf("out = %q, want the last 2 lines", out)
	}
	if strings.Contains(out, "l1") || strings.Contains(out, "l2") {
		t.Fatalf("out = %q, want earlier lines trimmed", out)
	}
	if !strings.Contains(out, "2 earlier delta lines dropped") {
		t.Fatalf("out = %q, missing the dropped-lines marker", out)
	}
}

// A log file that is truncated or replaced out from under readNew (offset
// now beyond the file's actual size) must restart cleanly from the new
// content instead of erroring or seeking past EOF.
func TestBashOutputHandlesTruncatedLog(t *testing.T) {
	t.Setenv("AXON_DATA_DIR", t.TempDir())
	shells := NewBackgroundShells()
	defer shells.KillAll()

	sh, err := shells.start("printf '0123456789'", "")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitForExit(t, sh)

	// Advance the offset past a chunk of the log by reading once.
	if _, _, err := sh.readNew(0); err != nil {
		t.Fatalf("readNew: %v", err)
	}
	// Now truncate the underlying log file to simulate rotation.
	if err := os.WriteFile(sh.LogPath, []byte("new"), 0644); err != nil {
		t.Fatalf("truncate log: %v", err)
	}

	out, _, err := sh.readNew(0)
	if err != nil {
		t.Fatalf("readNew after truncation errored: %v", err)
	}
	if out != "new" {
		t.Fatalf("readNew after truncation = %q, want the fresh content 'new'", out)
	}
}

// readNew is called concurrently by real bash_output invocations from the
// model; the lock documented on the type must hold under real concurrent
// callers so no byte is ever delivered twice.
func TestBashOutputReadNewConcurrentNoDoubleDelivery(t *testing.T) {
	t.Setenv("AXON_DATA_DIR", t.TempDir())
	shells := NewBackgroundShells()
	defer shells.KillAll()

	sh, err := shells.start("for i in $(seq 1 200); do printf 'x'; done", "")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitForExit(t, sh)

	var mu sync.Mutex
	total := 0
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				out, _, err := sh.readNew(0)
				if err != nil {
					t.Errorf("readNew: %v", err)
					return
				}
				if out == "" {
					return
				}
				mu.Lock()
				total += len(out)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if total != 200 {
		t.Fatalf("total bytes delivered across concurrent readers = %d, want exactly 200 (no double delivery, no loss)", total)
	}
}

// kill_shell on an unknown id must error, and on a live shell it must SIGTERM
// the process group and return the final status the model can trust.
func TestKillShellUnknownAndLive(t *testing.T) {
	t.Setenv("AXON_DATA_DIR", t.TempDir())
	shells := NewBackgroundShells()
	defer shells.KillAll()
	tool := KillShellTool(shells)

	_, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{"shell_id": "bash_99"}))
	if err == nil {
		t.Fatal("expected error for an unknown shell_id")
	}

	sh, err := shells.start("sleep 30", "")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	out, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{"shell_id": sh.ID}))
	if err != nil {
		t.Fatalf("Fn: %v", err)
	}
	if !strings.Contains(out, sh.ID) || !strings.Contains(out, "status:") {
		t.Fatalf("out = %q, want shell_id and status", out)
	}
	if !sh.finished {
		t.Fatal("shell not marked finished after kill_shell")
	}
}

// Killing a shell that has already finished on its own must be a no-op that
// returns nil, not an error about a dead process — the model routinely calls
// kill_shell defensively on things it is not sure are still running.
func TestKillAlreadyFinishedShellIsNoop(t *testing.T) {
	t.Setenv("AXON_DATA_DIR", t.TempDir())
	shells := NewBackgroundShells()
	defer shells.KillAll()

	sh, err := shells.start("true", "")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitForExit(t, sh)

	if err := sh.kill(2 * time.Second); err != nil {
		t.Fatalf("kill on an already-finished shell returned an error: %v", err)
	}
}

// A process that ignores SIGTERM must be SIGKILLed once the grace period
// elapses — otherwise kill_shell could hang forever on an uncooperative
// child, and the model would have no way to actually free the resource.
func TestKillSIGKILLsAfterGracePeriod(t *testing.T) {
	t.Setenv("AXON_DATA_DIR", t.TempDir())
	shells := NewBackgroundShells()
	defer shells.KillAll()

	// Do not signal until the shell has actually installed the TERM trap.
	sh, err := shells.start(`trap '' TERM; echo ready; sleep 30`, "")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitForLog(t, sh, "ready")

	start := time.Now()
	if err := sh.kill(300 * time.Millisecond); err != nil {
		t.Fatalf("kill: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed < 300*time.Millisecond {
		t.Fatalf("kill returned in %s, before the grace period elapsed — SIGKILL fallback did not wait", elapsed)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("kill took %s, want it bounded near the grace period", elapsed)
	}
	if !sh.finished {
		t.Fatal("shell not finished after SIGKILL fallback")
	}
}

// KillAll must terminate every live shell concurrently (not serially — a
// registry with several long-running servers must not take N * grace-period
// to shut down) and must be safe to call with nothing registered.
func TestKillAllTerminatesConcurrentlyAndIsSafeWhenEmpty(t *testing.T) {
	t.Setenv("AXON_DATA_DIR", t.TempDir())

	empty := NewBackgroundShells()
	empty.KillAll() // must not panic or block

	shells := NewBackgroundShells()
	for i := 0; i < 3; i++ {
		sh, err := shells.start(`trap '' TERM; echo ready; sleep 30`, "")
		if err != nil {
			t.Fatalf("start: %v", err)
		}
		waitForLog(t, sh, "ready")
	}

	start := time.Now()
	shells.KillAll()
	elapsed := time.Since(start)
	// Serial SIGTERM-then-grace-then-SIGKILL for 3 uncooperative shells at
	// the hardcoded 2s grace in KillAll would take >=6s; concurrent kill
	// should land close to the single grace period.
	if elapsed > 4*time.Second {
		t.Fatalf("KillAll took %s for 3 shells, want them killed concurrently (well under 3x the grace period)", elapsed)
	}
	for _, sh := range shells.list() {
		if !sh.finished {
			t.Fatalf("shell %s still not finished after KillAll", sh.ID)
		}
	}
	// Idempotent after all processes are already gone.
	shells.KillAll()
}

// Killing the process group must reap children the sh -lc wrapper spawns,
// not just the wrapper shell itself — otherwise a background command that
// backgrounds its own child (`sleep 100 &`) would survive kill_shell and leak.
func TestKillReapsGrandchildren(t *testing.T) {
	t.Setenv("AXON_DATA_DIR", t.TempDir())
	shells := NewBackgroundShells()
	defer shells.KillAll()

	sh, err := shells.start(`sleep 100 & echo "child_pid=$!"; wait`, "")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitForLog(t, sh, "child_pid=")

	data, err := os.ReadFile(sh.LogPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	line := strings.TrimSpace(strings.SplitN(string(data), "child_pid=", 2)[1])
	childPID := line

	if err := sh.kill(2 * time.Second); err != nil {
		t.Fatalf("kill: %v", err)
	}

	// Reaping can lag process-group termination very slightly, so poll for
	// disappearance instead of assuming /proc is gone in the same instant.
	waitForProcGone(t, childPID)
}

// Same shell IDs in separate registries must not imply shared storage. This
// directly pins the original regression: bash_1 in registry A can never read
// bash_1's output from registry B.
func TestBackgroundShellRegistriesIsolateLogContents(t *testing.T) {
	t.Setenv("AXON_DATA_DIR", t.TempDir())

	a := NewBackgroundShells()
	defer a.KillAll()
	b := NewBackgroundShells()
	defer b.KillAll()

	sha, err := a.start("printf 'registry-A'", "")
	if err != nil {
		t.Fatalf("start A: %v", err)
	}
	shb, err := b.start("printf 'registry-B'", "")
	if err != nil {
		t.Fatalf("start B: %v", err)
	}
	if sha.ID != "bash_1" || shb.ID != "bash_1" {
		t.Fatalf("first IDs = %s, %s, want bash_1 in both registries", sha.ID, shb.ID)
	}
	waitForExit(t, sha)
	waitForExit(t, shb)

	outA, _, err := sha.readNew(0)
	if err != nil {
		t.Fatalf("read A: %v", err)
	}
	outB, _, err := shb.readNew(0)
	if err != nil {
		t.Fatalf("read B: %v", err)
	}
	if outA != "registry-A" {
		t.Fatalf("registry A read %q, want only its own output", outA)
	}
	if outB != "registry-B" {
		t.Fatalf("registry B read %q, want only its own output", outB)
	}
	if sha.LogPath == shb.LogPath {
		t.Fatalf("registries share a log path: %s", sha.LogPath)
	}
}

// start must actually honor the execution contract it advertises: cwd is
// applied, stdout and stderr are merged into the log, and stdin is /dev/null
// so a background process cannot block waiting for agent input forever.
func TestBackgroundShellStartExecutionContract(t *testing.T) {
	t.Setenv("AXON_DATA_DIR", t.TempDir())
	shells := NewBackgroundShells()
	defer shells.KillAll()

	workdir := t.TempDir()
	sh, err := shells.start(`pwd; echo stdout-line; echo stderr-line >&2; if read x; then echo unexpected-stdin; else echo stdin-eof; fi`, workdir)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitForExit(t, sh)

	out, _, err := sh.readNew(0)
	if err != nil {
		t.Fatalf("readNew: %v", err)
	}
	for _, want := range []string{workdir, "stdout-line", "stderr-line", "stdin-eof"} {
		if !strings.Contains(out, want) {
			t.Fatalf("combined log missing %q: %q", want, out)
		}
	}
	if strings.Contains(out, "unexpected-stdin") {
		t.Fatalf("background command unexpectedly read stdin: %q", out)
	}
}

// ID generation and registry insertion are both protected by the registry
// mutex. Concurrent starts must therefore produce one unique stable ID per
// successful process and must not lose any registry entries.
func TestBackgroundShellConcurrentStartsHaveUniqueIDs(t *testing.T) {
	t.Setenv("AXON_DATA_DIR", t.TempDir())
	shells := NewBackgroundShells()
	defer shells.KillAll()

	const n = 50
	var wg sync.WaitGroup
	ids := make(chan string, n)
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sh, err := shells.start("true", "")
			if err != nil {
				errs <- err
				return
			}
			ids <- sh.ID
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)

	for err := range errs {
		t.Fatalf("concurrent start: %v", err)
	}
	seen := make(map[string]struct{}, n)
	for id := range ids {
		if _, exists := seen[id]; exists {
			t.Fatalf("duplicate shell ID allocated concurrently: %s", id)
		}
		seen[id] = struct{}{}
	}
	if len(seen) != n {
		t.Fatalf("unique IDs = %d, want %d", len(seen), n)
	}
	if got := len(shells.list()); got != n {
		t.Fatalf("registry entries = %d, want %d", got, n)
	}
}

// A command that exits normally but unsuccessfully must retain its exact exit
// code; otherwise the agent cannot distinguish a successful build from one
// that simply stopped running.
func TestBackgroundShellReportsNonZeroExitCode(t *testing.T) {
	t.Setenv("AXON_DATA_DIR", t.TempDir())
	shells := NewBackgroundShells()
	defer shells.KillAll()

	sh, err := shells.start("exit 7", "")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitForExit(t, sh)

	if got := sh.status(); got != "exited (exit 7)" {
		t.Fatalf("status = %q, want %q", got, "exited (exit 7)")
	}
}

// A failed exec.Start must be surfaced as an error and must never leave a
// phantom shell in the registry that bash_output/kill_shell can later find.
func TestBackgroundShellStartFailureDoesNotRegisterShell(t *testing.T) {
	t.Setenv("AXON_DATA_DIR", t.TempDir())
	shells := NewBackgroundShells()
	defer shells.KillAll()

	missingDir := filepath.Join(t.TempDir(), "does-not-exist")
	if _, err := shells.start("echo should-not-run", missingDir); err == nil {
		t.Fatal("start with a nonexistent working directory unexpectedly succeeded")
	}
	if got := len(shells.list()); got != 0 {
		t.Fatalf("registry contains %d shells after failed start, want 0", got)
	}
}

// readNew's documented rotation behavior must work even when the replacement
// file is larger than the previous read offset. This currently exposes a bug:
// size alone cannot distinguish "old file appended" from "new file replaced".
func TestBashOutputHandlesReplacedLargerLog(t *testing.T) {
	t.Setenv("AXON_DATA_DIR", t.TempDir())
	shells := NewBackgroundShells()
	defer shells.KillAll()

	sh, err := shells.start("printf '0123456789'", "")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitForExit(t, sh)

	first, _, err := sh.readNew(0)
	if err != nil {
		t.Fatalf("initial readNew: %v", err)
	}
	if first != "0123456789" {
		t.Fatalf("initial read = %q, want original log", first)
	}

	// Replace, rather than append to, the log. Make the new file larger than
	// the old offset so min(readOffset, size) cannot accidentally reset to 0.
	replacement := "NEW-FILE-CONTENT-IS-LONGER"
	tmp := sh.LogPath + ".replacement"
	if err := os.WriteFile(tmp, []byte(replacement), 0644); err != nil {
		t.Fatalf("write replacement: %v", err)
	}
	if err := os.Rename(tmp, sh.LogPath); err != nil {
		t.Fatalf("replace log: %v", err)
	}

	out, _, err := sh.readNew(0)
	if err != nil {
		t.Fatalf("readNew after replacement: %v", err)
	}
	if out != replacement {
		t.Fatalf("readNew after larger-file replacement = %q, want all fresh content %q", out, replacement)
	}
}

// waitForLog polls a shell's log file until it contains want, or fails the
// test after a bounded timeout — used instead of a fixed sleep so the test is
// both fast on a quiet machine and not flaky on a loaded one.
func waitForLog(t *testing.T, sh *bgShell, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(sh.LogPath)
		if err == nil && strings.Contains(string(data), want) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for log to contain %q", want)
}

func waitForProcGone(t *testing.T, pid string) {
	t.Helper()
	path := filepath.Join("/proc", pid)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("process %s still present after bounded reap wait", pid)
}

func waitForExit(t *testing.T, sh *bgShell) {
	t.Helper()
	select {
	case <-sh.doneCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for shell to exit")
	}
}
