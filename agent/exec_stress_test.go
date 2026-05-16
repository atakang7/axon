package agent

import (
	"context"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// ES1 — shell injection attempt: user passes command with semicolons/backquotes.
// These MUST be interpreted by the shell (that's the feature), but we verify
// the output is sane and the tool does not panic.
func TestExecStress_ES1_ShellInjectionPassThrough(t *testing.T) {
	s := newSession(t)
	out, err := callTool(t, context.Background(), s, "exec", map[string]any{
		"mode": "run", "command": "echo A; echo B", "tail_lines": 10, "reason": "t",
	})
	mustNoErr(t, err)
	mustContain(t, out, "A")
	mustContain(t, out, "B")
}

// ES2 — command writes to stderr only; combined buffer must contain it.
func TestExecStress_ES2_StderrOnly(t *testing.T) {
	s := newSession(t)
	out, err := callTool(t, context.Background(), s, "exec", map[string]any{
		"mode": "run", "command": "echo STDERR_ONLY 1>&2", "tail_lines": 10, "reason": "t",
	})
	mustNoErr(t, err)
	mustContain(t, out, "STDERR_ONLY")
}

// ES3 — command produces interleaved stdout/stderr rapidly; no deadlock.
func TestExecStress_ES3_InterleavedOutputNoDeadlock(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix")
	}
	s := newSession(t)
	// Alternate stdout/stderr 500 times rapidly.
	cmd := `for i in $(seq 1 500); do echo "OUT$i"; echo "ERR$i" 1>&2; done`
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = callTool(t, context.Background(), s, "exec", map[string]any{
			"mode": "run", "command": cmd, "tail_lines": 20,
			"timeout_seconds": 30, "reason": "t",
		})
	}()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("interleaved I/O caused deadlock")
	}
}

// ES4 — zero timeout_seconds (0) is treated as "use default", not "instant kill".
func TestExecStress_ES4_ZeroTimeoutUsesDefault(t *testing.T) {
	s := newSession(t)
	t.Setenv("AXON_EXEC_TIMEOUT_SECONDS", "5")
	out, err := callTool(t, context.Background(), s, "exec", map[string]any{
		"mode": "run", "command": "echo ok", "tail_lines": 5,
		"timeout_seconds": 0, "reason": "t",
	})
	mustNoErr(t, err)
	mustContain(t, out, "ok")
}

// ES5 — tail_lines=1: only the last line is visible; prior lines are hidden.
func TestExecStress_ES5_TailLines1(t *testing.T) {
	s := newSession(t)
	out, err := callTool(t, context.Background(), s, "exec", map[string]any{
		"mode": "run", "command": "seq 100", "tail_lines": 1, "reason": "t",
	})
	mustNoErr(t, err)
	mustContain(t, out, "100")
	mustNotContain(t, out, "\n1\n")
}

// ES6 — command that does NOT write a newline at EOF: tail_lines must still
// capture the last partial line.
func TestExecStress_ES6_NoTrailingNewline(t *testing.T) {
	s := newSession(t)
	out, err := callTool(t, context.Background(), s, "exec", map[string]any{
		"mode": "run", "command": "printf 'no_newline'", "tail_lines": 5, "reason": "t",
	})
	mustNoErr(t, err)
	mustContain(t, out, "no_newline")
}

// ES7 — output cap fires mid-stream: truncation header present, no panic.
func TestExecStress_ES7_OutputCapMidStream(t *testing.T) {
	s := newSession(t)
	t.Setenv("AXON_EXEC_OUTPUT_LIMIT", "1024")
	out, err := callTool(t, context.Background(), s, "exec", map[string]any{
		"mode": "run", "command": "yes X | head -c 1000000", "tail_lines": 5, "reason": "t",
	})
	mustNoErr(t, err)
	mustContain(t, out, "[output truncated at byte limit]")
}

// ES8 — concurrent exec calls share no state: each command's output is
// independent.
func TestExecStress_ES8_ConcurrentExecIsolation(t *testing.T) {
	s := newSession(t)
	type result struct {
		id  int
		out string
		err error
	}
	n := 10
	results := make(chan result, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tag := "TAG" + string(rune('A'+i))
			out, err := callTool(t, context.Background(), s, "exec", map[string]any{
				"mode": "run", "command": "echo " + tag, "tail_lines": 5, "reason": "t",
			})
			results <- result{i, out, err}
		}(i)
	}
	wg.Wait()
	close(results)
	for r := range results {
		if r.err != nil {
			t.Errorf("goroutine %d: %v", r.id, r.err)
			continue
		}
		tag := "TAG" + string(rune('A'+r.id))
		if !strings.Contains(r.out, tag) {
			t.Errorf("goroutine %d: output missing tag %q: %q", r.id, tag, r.out)
		}
	}
}

// ES9 — parent context cancelled while output is still streaming: tool returns
// promptly, not after command finishes.
func TestExecStress_ES9_CancelMidOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix")
	}
	s := newSession(t)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(300 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	_, err := callTool(t, ctx, s, "exec", map[string]any{
		"mode": "run", "command": "yes X", "tail_lines": 5,
		"timeout_seconds": 60, "reason": "t",
	})
	elapsed := time.Since(start)
	_ = err
	if elapsed > 5*time.Second {
		t.Fatalf("cancel mid-stream took too long: %v", elapsed)
	}
}

// ES10 — very long single command line (8KB): no arg truncation or panic.
func TestExecStress_ES10_LongCommandLine(t *testing.T) {
	s := newSession(t)
	// Echo a deterministic marker buried in a very long echo command.
	marker := "LONGCMD_MARKER"
	noise := strings.Repeat("x", 8000)
	cmd := "echo " + noise + " && echo " + marker
	out, err := callTool(t, context.Background(), s, "exec", map[string]any{
		"mode": "run", "command": cmd, "tail_lines": 5, "reason": "t",
	})
	mustNoErr(t, err)
	mustContain(t, out, marker)
}

// ES11 — exec with dir pointing to a non-existent directory returns an error.
func TestExecStress_ES11_BadDir(t *testing.T) {
	s := newSession(t)
	_, err := callTool(t, context.Background(), s, "exec", map[string]any{
		"mode": "run", "command": "echo x", "tail_lines": 5,
		"dir": "/no/such/__axon_test_dir__", "reason": "t",
	})
	if err == nil {
		t.Fatal("expected error for non-existent dir")
	}
}

// ES12 — run_in_background: shell_id in output is parseable and unique across
// rapid sequential spawns.
func TestExecStress_ES12_BackgroundUniqueIDs(t *testing.T) {
	s := newSession(t)
	defer bgReg.killAll()
	seen := map[string]bool{}
	for i := 0; i < 10; i++ {
		out, err := callTool(t, context.Background(), s, "exec", map[string]any{
			"mode": "run", "command": "sleep 60", "run_in_background": true, "reason": "t",
		})
		mustNoErr(t, err)
		// Extract shell_id from "shell_id: bash_N"
		for _, line := range strings.Split(out, "\n") {
			if strings.HasPrefix(line, "shell_id:") {
				id := strings.TrimSpace(strings.TrimPrefix(line, "shell_id:"))
				if seen[id] {
					t.Fatalf("duplicate shell_id: %s", id)
				}
				seen[id] = true
			}
		}
	}
	if len(seen) != 10 {
		t.Fatalf("expected 10 unique shell IDs, got %d", len(seen))
	}
}

// ES13 — command exits immediately with code 127 (not found): reported cleanly.
func TestExecStress_ES13_CommandNotFound(t *testing.T) {
	s := newSession(t)
	out, err := callTool(t, context.Background(), s, "exec", map[string]any{
		"mode": "run", "command": "__axon_no_such_command_xyz__", "tail_lines": 5, "reason": "t",
	})
	mustNoErr(t, err)
	// sh reports 127 for command not found
	mustContain(t, out, "exit_code:")
	mustNotContain(t, out, "exit_code: 0")
}

// ES14 — tail_lines=1 on a command that writes millions of bytes: does not OOM.
func TestExecStress_ES14_TailOnMassiveOutput(t *testing.T) {
	s := newSession(t)
	t.Setenv("AXON_EXEC_OUTPUT_LIMIT", "10485760") // 10MB cap
	out, err := callTool(t, context.Background(), s, "exec", map[string]any{
		"mode": "run", "command": "yes AXON | head -c 5000000", "tail_lines": 1,
		"timeout_seconds": 30, "reason": "t",
	})
	mustNoErr(t, err)
	mustContain(t, out, "AXON")
}

// ES15 — timeout_seconds is a float (1.5): no JSON unmarshal panic.
func TestExecStress_ES15_FloatTimeout(t *testing.T) {
	s := newSession(t)
	// This will likely be parsed as an integer truncation or error — must not panic.
	_, _ = callTool(t, context.Background(), s, "exec", map[string]any{
		"mode": "run", "command": "echo x", "tail_lines": 5,
		"timeout_seconds": 1.5, "reason": "t",
	})
}
