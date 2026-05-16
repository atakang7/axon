package agent

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// E1 — simple foreground run.
func TestExec_E1_Simple(t *testing.T) {
	s := newSession(t)
	out, err := callTool(t, context.Background(), s, "exec", map[string]any{
		"mode": "run", "command": "echo hi", "tail_lines": 5, "reason": "test",
	})
	mustNoErr(t, err)
	mustContain(t, out, "hi")
	mustContain(t, out, "exit_code: 0")
}

// E2 — non-zero exit reported, no Go-level error.
func TestExec_E2_NonZeroExit(t *testing.T) {
	s := newSession(t)
	out, err := callTool(t, context.Background(), s, "exec", map[string]any{
		"mode": "run", "command": "false", "tail_lines": 5, "reason": "test",
	})
	mustNoErr(t, err)
	mustContain(t, out, "exit_code: 1")
}

// E3 — timeout fires; default timeout from env applies.
func TestExec_E3_DefaultTimeout(t *testing.T) {
	s := newSession(t)
	t.Setenv("AXON_EXEC_TIMEOUT_SECONDS", "1")
	start := time.Now()
	out, err := callTool(t, context.Background(), s, "exec", map[string]any{
		"mode": "run", "command": "sleep 30", "tail_lines": 5, "reason": "test",
	})
	mustNoErr(t, err)
	elapsed := time.Since(start)
	if elapsed > 5*time.Second {
		t.Fatalf("expected ~1s, took %v", elapsed)
	}
	mustContain(t, out, "timed out")
	mustContain(t, out, "exit_code: -1")
}

// E4 — user-supplied timeout is capped by AXON_EXEC_MAX_TIMEOUT_SECONDS.
func TestExec_E4_TimeoutCap(t *testing.T) {
	s := newSession(t)
	t.Setenv("AXON_EXEC_MAX_TIMEOUT_SECONDS", "2")
	start := time.Now()
	out, err := callTool(t, context.Background(), s, "exec", map[string]any{
		"mode": "run", "command": "sleep 30", "tail_lines": 5,
		"timeout_seconds": 999999, "reason": "test",
	})
	mustNoErr(t, err)
	elapsed := time.Since(start)
	if elapsed > 6*time.Second {
		t.Fatalf("cap not enforced; took %v", elapsed)
	}
	mustContain(t, out, "timed out")
}

// E5 — process group kill: a grandchild holding stdout/stderr open does not
// keep cmd.Wait blocked past the timeout.
func TestExec_E5_ProcessGroupKill(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix process groups")
	}
	s := newSession(t)
	start := time.Now()
	out, err := callTool(t, context.Background(), s, "exec", map[string]any{
		"mode": "run",
		// Backgrounded grandchild inherits stdout/stderr → without Setpgid+
		// process-group SIGKILL, cmd.Wait blocks forever.
		"command":         "( sleep 30 & ) ; echo started",
		"tail_lines":      5,
		"timeout_seconds": 2,
		"reason":          "test",
	})
	mustNoErr(t, err)
	if elapsed := time.Since(start); elapsed > 6*time.Second {
		t.Fatalf("blocked on grandchild; took %v", elapsed)
	}
	// Output should include "started" and we should have timed out.
	mustContain(t, out, "started")
}

// E6 — cancelling the parent ctx returns within the grace window with
// "cancelled" note (Ctrl-C analogue).
func TestExec_E6_ParentCancel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix process groups")
	}
	s := newSession(t)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(500 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	out, err := callTool(t, ctx, s, "exec", map[string]any{
		"mode": "run", "command": "sleep 30", "tail_lines": 5,
		"timeout_seconds": 60, "reason": "test",
	})
	mustNoErr(t, err)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("parent-cancel did not propagate; took %v", elapsed)
	}
	mustContain(t, out, "cancelled")
}

// E7 — interactive prompt does not hang forever (stdin is /dev/null).
func TestExec_E7_StdinIsDevNull(t *testing.T) {
	s := newSession(t)
	start := time.Now()
	_, err := callTool(t, context.Background(), s, "exec", map[string]any{
		"mode": "run", "command": "head -n 1", "tail_lines": 5,
		"timeout_seconds": 5, "reason": "test",
	})
	mustNoErr(t, err)
	if elapsed := time.Since(start); elapsed > 4*time.Second {
		t.Fatalf("stdin not closed cleanly; took %v", elapsed)
	}
}

// E8 — tail_lines hides earlier output and reports the hidden count.
func TestExec_E8_TailLinesHidden(t *testing.T) {
	s := newSession(t)
	out, err := callTool(t, context.Background(), s, "exec", map[string]any{
		"mode": "run", "command": "seq 200", "tail_lines": 10, "reason": "test",
	})
	mustNoErr(t, err)
	mustContain(t, out, "190 earlier lines hidden")
	mustContain(t, out, "200")
}

// E9 — tail_lines is capped by AXON_EXEC_MAX_TAIL_LINES.
func TestExec_E9_TailLinesCapped(t *testing.T) {
	s := newSession(t)
	t.Setenv("AXON_EXEC_MAX_TAIL_LINES", "20")
	out, err := callTool(t, context.Background(), s, "exec", map[string]any{
		"mode": "run", "command": "seq 1000", "tail_lines": 999999, "reason": "test",
	})
	mustNoErr(t, err)
	// Count visible numeric lines after the header.
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	visibleNumbers := 0
	for _, l := range lines {
		if n, err := strconv.Atoi(strings.TrimSpace(l)); err == nil && n > 0 && n <= 1000 {
			visibleNumbers++
		}
	}
	if visibleNumbers > 20 {
		t.Fatalf("expected at most 20 lines, got %d. output:\n%s", visibleNumbers, out)
	}
}

// E10 — tail_lines required for run mode.
func TestExec_E10_TailLinesRequired(t *testing.T) {
	s := newSession(t)
	_, err := callTool(t, context.Background(), s, "exec", map[string]any{
		"mode": "run", "command": "echo x", "reason": "test",
	})
	mustErr(t, err, "tail_lines is required")
}

// E11 — output byte cap reported as truncated.
func TestExec_E11_OutputByteCap(t *testing.T) {
	s := newSession(t)
	t.Setenv("AXON_EXEC_OUTPUT_LIMIT", "200")
	out, err := callTool(t, context.Background(), s, "exec", map[string]any{
		"mode":       "run",
		"command":    "yes a | head -c 50000",
		"tail_lines": 5,
		"reason":     "test",
	})
	mustNoErr(t, err)
	mustContain(t, out, "[output truncated at byte limit]")
}

// E12 — verify on a Go project runs go build.
func TestExec_E12_VerifyGo(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not on PATH")
	}
	s := newSession(t)
	writeFile(t, s, "go.mod", "module testmod\ngo 1.21\n")
	writeFile(t, s, "main.go", "package main\nfunc main() {}\n")
	out, err := callTool(t, context.Background(), s, "exec", map[string]any{
		"mode": "verify", "reason": "test",
	})
	mustNoErr(t, err)
	mustContain(t, out, "go build")
	mustContain(t, out, "exit_code: 0")
}

// E13 — verify on a Python project uses compileall (not unsafe find $(...)).
func TestExec_E13_VerifyPythonCompileAll(t *testing.T) {
	if _, err := exec.LookPath("python"); err != nil {
		t.Skip("python not on PATH")
	}
	s := newSession(t)
	writeFile(t, s, "pyproject.toml", "[project]\nname=\"x\"\n")
	writeFile(t, s, "ok.py", "x = 1\n")
	out, err := callTool(t, context.Background(), s, "exec", map[string]any{
		"mode": "verify", "reason": "test",
	})
	mustNoErr(t, err)
	mustContain(t, out, "compileall")
	mustNotContain(t, out, "$(find")
}

// E14 — verify catches a syntax error in a path with spaces (the case the
// old find $(...) command broke on).
func TestExec_E14_VerifyPythonSpacedPath(t *testing.T) {
	if _, err := exec.LookPath("python"); err != nil {
		t.Skip("python not on PATH")
	}
	s := newSession(t)
	writeFile(t, s, "pyproject.toml", "[project]\nname=\"x\"\n")
	writeFile(t, s, "bad dir/x.py", "def (\n")
	out, err := callTool(t, context.Background(), s, "exec", map[string]any{
		"mode": "verify", "reason": "test",
	})
	mustNoErr(t, err)
	if strings.Contains(out, "exit_code: 0") {
		t.Fatalf("expected non-zero exit on bad python; output:\n%s", out)
	}
}

// E15 — verify with no markers errors clearly.
func TestExec_E15_VerifyNoMarkers(t *testing.T) {
	s := newSession(t)
	_, err := callTool(t, context.Background(), s, "exec", map[string]any{
		"mode": "verify", "reason": "test",
	})
	mustErr(t, err, "could not detect")
}

// E16 — run_in_background returns immediately with a shell_id.
func TestExec_E16_BackgroundReturnsImmediately(t *testing.T) {
	s := newSession(t)
	start := time.Now()
	out, err := callTool(t, context.Background(), s, "exec", map[string]any{
		"mode": "run", "command": "sleep 60", "run_in_background": true, "reason": "test",
	})
	mustNoErr(t, err)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("background spawn was not immediate: %v", elapsed)
	}
	mustContain(t, out, "shell_id: bash_")
	mustContain(t, out, "status: running")
	// Cleanup — kill via registry directly (we don't know the id without parsing).
	bgReg.killAll()
}

// E17 — run_in_background with verify is rejected.
func TestExec_E17_BackgroundVerifyRejected(t *testing.T) {
	s := newSession(t)
	writeFile(t, s, "go.mod", "module x\n")
	_, err := callTool(t, context.Background(), s, "exec", map[string]any{
		"mode": "verify", "run_in_background": true, "reason": "test",
	})
	mustErr(t, err, "not valid with mode=verify")
}

// E18 — exec dir override.
func TestExec_E18_DirOverride(t *testing.T) {
	s := newSession(t)
	otherDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(otherDir, "marker"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := callTool(t, context.Background(), s, "exec", map[string]any{
		"mode": "run", "command": "ls marker", "tail_lines": 5,
		"dir": otherDir, "reason": "test",
	})
	mustNoErr(t, err)
	mustContain(t, out, "marker")
}

// E19 — shell metacharacters work via sh -lc.
func TestExec_E19_ShellMeta(t *testing.T) {
	s := newSession(t)
	out, err := callTool(t, context.Background(), s, "exec", map[string]any{
		"mode": "run", "command": "echo a && echo b", "tail_lines": 5, "reason": "test",
	})
	mustNoErr(t, err)
	mustContain(t, out, "a")
	mustContain(t, out, "b")
}

// E20 — stderr is captured into the same buffer.
func TestExec_E20_StderrCaptured(t *testing.T) {
	s := newSession(t)
	out, err := callTool(t, context.Background(), s, "exec", map[string]any{
		"mode": "run", "command": "echo err 1>&2", "tail_lines": 5, "reason": "test",
	})
	mustNoErr(t, err)
	mustContain(t, out, "err")
}
