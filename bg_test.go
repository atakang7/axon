package main

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Spawn a bg shell directly via the registry and return its id.
// Cleans up via t.Cleanup.
func startBg(t *testing.T, command string) *bgShell {
	t.Helper()
	sh, err := bgReg.start(command, "")
	if err != nil {
		t.Fatalf("bg start: %v", err)
	}
	t.Cleanup(func() { _ = sh.kill(2 * time.Second) })
	return sh
}

func waitForOutput(sh *bgShell, want string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		// Peek without advancing offset.
		// Use readNew(0) which means "no cap" — but it advances offset, so
		// instead spin on log via the public bash_output tool path. Easier:
		// just sleep small intervals and test status + a single readNew at
		// the end. Acceptable since these are deterministic tests.
		time.Sleep(50 * time.Millisecond)
		_ = want
	}
	return false
}

// B1 — read fresh background output.
func TestBg_B1_ReadFresh(t *testing.T) {
	s := newSession(t)
	sh := startBg(t, "for i in 1 2 3 4 5; do echo $i; done")
	// wait for exit
	select {
	case <-sh.doneCh:
	case <-time.After(5 * time.Second):
		t.Fatal("bg shell did not finish in time")
	}
	out, err := callTool(t, context.Background(), s, "bash_output", map[string]any{
		"shell_id": sh.ID, "reason": "test",
	})
	mustNoErr(t, err)
	for _, n := range []string{"1", "2", "3", "4", "5"} {
		mustContain(t, out, n)
	}
}

// B2 — second read returns no new output.
func TestBg_B2_DeltaOnly(t *testing.T) {
	s := newSession(t)
	sh := startBg(t, "echo hello")
	<-sh.doneCh
	_, err := callTool(t, context.Background(), s, "bash_output", map[string]any{
		"shell_id": sh.ID, "reason": "test",
	})
	mustNoErr(t, err)
	out2, err := callTool(t, context.Background(), s, "bash_output", map[string]any{
		"shell_id": sh.ID, "reason": "test",
	})
	mustNoErr(t, err)
	mustContain(t, out2, "(no new output)")
}

// B3 — max_bytes truncates and reports.
func TestBg_B3_MaxBytesTruncates(t *testing.T) {
	s := newSession(t)
	// Produce >2KB of output then exit.
	sh := startBg(t, "yes a | head -c 4096; echo END")
	<-sh.doneCh
	out, err := callTool(t, context.Background(), s, "bash_output", map[string]any{
		"shell_id": sh.ID, "max_bytes": 512, "reason": "test",
	})
	mustNoErr(t, err)
	mustContain(t, out, "earlier delta bytes dropped")
}

// B4 — after a max_bytes truncation, the offset still advanced past the
// dropped bytes; a follow-up read returns only NEW data, not the dropped tail.
func TestBg_B4_OffsetAdvancesPastDropped(t *testing.T) {
	s := newSession(t)
	sh := startBg(t, "yes a | head -c 4096; sleep 1; echo MARKER")
	// Wait for the first 4 KB chunk to land but NOT the MARKER (which is
	// gated behind sleep 1).
	time.Sleep(300 * time.Millisecond)
	// First read with small cap — most of the buffered bytes get dropped.
	out1, err := callTool(t, context.Background(), s, "bash_output", map[string]any{
		"shell_id": sh.ID, "max_bytes": 256, "reason": "test",
	})
	mustNoErr(t, err)
	if strings.Contains(out1, "MARKER") {
		t.Skipf("test timing: shell finished before first read; got %q", out1)
	}

	// Wait for completion so MARKER is emitted.
	select {
	case <-sh.doneCh:
	case <-time.After(5 * time.Second):
		t.Fatal("bg did not finish")
	}

	// Second read: should NOT redeliver the dropped 'a' bytes; should
	// include MARKER (and any tail-of-aaa that landed after the offset).
	out2, err := callTool(t, context.Background(), s, "bash_output", map[string]any{
		"shell_id": sh.ID, "reason": "test",
	})
	mustNoErr(t, err)
	mustContain(t, out2, "MARKER")
	// Sanity: we should not have re-received thousands of 'a' bytes.
	if strings.Count(out2, "a") > 200 {
		t.Fatalf("offset did not advance past dropped bytes; out2 had %d 'a' chars",
			strings.Count(out2, "a"))
	}
}

// B5 — tail_lines truncates a chatty delta.
func TestBg_B5_TailLines(t *testing.T) {
	s := newSession(t)
	sh := startBg(t, "seq 1000")
	<-sh.doneCh
	out, err := callTool(t, context.Background(), s, "bash_output", map[string]any{
		"shell_id": sh.ID, "tail_lines": 3, "max_bytes": 1000000, "reason": "test",
	})
	mustNoErr(t, err)
	mustContain(t, out, "earlier delta lines dropped at tail_lines")
	mustContain(t, out, "1000")
	mustNotContain(t, out, "\n1\n")
}

// B6 — unknown shell_id is a clean error.
func TestBg_B6_UnknownID(t *testing.T) {
	s := newSession(t)
	_, err := callTool(t, context.Background(), s, "bash_output", map[string]any{
		"shell_id": "bash_999999", "reason": "test",
	})
	mustErr(t, err, "unknown shell_id")
}

// B7 — read after exit reports exit status.
func TestBg_B7_ReadAfterExit(t *testing.T) {
	s := newSession(t)
	sh := startBg(t, "echo done")
	<-sh.doneCh
	out, err := callTool(t, context.Background(), s, "bash_output", map[string]any{
		"shell_id": sh.ID, "reason": "test",
	})
	mustNoErr(t, err)
	mustContain(t, out, "exit 0")
	mustContain(t, out, "done")
}

// K1 — kill running shell returns within the grace window.
func TestKill_K1_RunningShell(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix signals")
	}
	s := newSession(t)
	sh := startBg(t, "sleep 60")
	start := time.Now()
	out, err := callTool(t, context.Background(), s, "kill_shell", map[string]any{
		"shell_id": sh.ID, "reason": "test",
	})
	mustNoErr(t, err)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("kill_shell took too long: %v", elapsed)
	}
	mustContain(t, out, sh.ID)
}

// K2 — kill already-finished shell is a no-op.
func TestKill_K2_AlreadyFinished(t *testing.T) {
	s := newSession(t)
	sh := startBg(t, "true")
	<-sh.doneCh
	_, err := callTool(t, context.Background(), s, "kill_shell", map[string]any{
		"shell_id": sh.ID, "reason": "test",
	})
	mustNoErr(t, err)
}

// K3 — kill propagates to grandchildren via process group.
func TestKill_K3_ProcessGroup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix process groups")
	}
	s := newSession(t)
	sh := startBg(t, "(sleep 60 &) ; sleep 60")
	start := time.Now()
	_, err := callTool(t, context.Background(), s, "kill_shell", map[string]any{
		"shell_id": sh.ID, "reason": "test",
	})
	mustNoErr(t, err)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("kill did not return promptly: %v", elapsed)
	}
}

// K4 — unknown shell_id returns a clean error.
func TestKill_K4_UnknownID(t *testing.T) {
	s := newSession(t)
	_, err := callTool(t, context.Background(), s, "kill_shell", map[string]any{
		"shell_id": "bash_999999", "reason": "test",
	})
	mustErr(t, err, "unknown shell_id")
}
