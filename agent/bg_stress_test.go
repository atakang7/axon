package agent

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// BS1 — rapid-fire bash_output polls on a running shell: no data race, no
// panic, each call returns a valid response.
func TestBgStress_BS1_RapidFirePolls(t *testing.T) {
	s := newSession(t)
	sh := startBg(t, "for i in $(seq 1 100); do echo line$i; sleep 0.01; done")
	var wg sync.WaitGroup
	errs := make(chan error, 50)
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := callTool(t, context.Background(), s, "bash_output", map[string]any{
				"shell_id": sh.ID, "reason": "t",
			})
			if err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// BS2 — bash_output with max_bytes=1: every call returns at most 1 byte of
// content (plus metadata), offset advances so each call doesn't loop.
func TestBgStress_BS2_MaxBytesOne(t *testing.T) {
	s := newSession(t)
	sh := startBg(t, "printf 'ABCDE'")
	<-sh.doneCh
	// 5 calls each with max_bytes=1 should collectively drain the log.
	// We just verify no call blocks or panics.
	for i := 0; i < 5; i++ {
		_, err := callTool(t, context.Background(), s, "bash_output", map[string]any{
			"shell_id": sh.ID, "max_bytes": 1, "reason": "t",
		})
		mustNoErr(t, err)
	}
}

// BS3 — bash_output tail_lines=0: should not panic; returns "(no new output)"
// or the last 0 lines (empty tail).
func TestBgStress_BS3_TailLinesZero(t *testing.T) {
	s := newSession(t)
	sh := startBg(t, "seq 10")
	<-sh.doneCh
	// prime offset
	_, _ = callTool(t, context.Background(), s, "bash_output", map[string]any{
		"shell_id": sh.ID, "reason": "t",
	})
	// second call with tail_lines=0
	out, err := callTool(t, context.Background(), s, "bash_output", map[string]any{
		"shell_id": sh.ID, "tail_lines": 0, "reason": "t",
	})
	mustNoErr(t, err)
	_ = out // no specific assertion — just no panic
}

// BS4 — concurrent bash_output calls on the SAME shell: offset accounting is
// monotonic (no call should return already-consumed bytes).
func TestBgStress_BS4_ConcurrentOffsetMonotonic(t *testing.T) {
	s := newSession(t)
	// Produce 10000 lines then exit.
	sh := startBg(t, "seq 10000")
	<-sh.doneCh

	// 20 goroutines each call bash_output; together they drain the log.
	// Each byte should appear at most once across all outputs (delta semantics).
	var mu sync.Mutex
	allOut := ""
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := callTool(t, context.Background(), s, "bash_output", map[string]any{
				"shell_id": sh.ID, "reason": "t",
			})
			if err != nil {
				return
			}
			mu.Lock()
			allOut += out
			mu.Unlock()
		}()
	}
	wg.Wait()

	// "10000" should appear exactly once across all reads.
	count := strings.Count(allOut, "10000\n")
	if count > 1 {
		t.Fatalf("'10000' appeared %d times across concurrent reads (delta semantics violated)", count)
	}
}

// BS5 — kill_shell on a shell that loops forever: returns within 5s and shell
// is marked finished afterward.
func TestBgStress_BS5_KillLoopingShell(t *testing.T) {
	s := newSession(t)
	sh := startBg(t, "while true; do :; done")
	start := time.Now()
	_, err := callTool(t, context.Background(), s, "kill_shell", map[string]any{
		"shell_id": sh.ID, "reason": "t",
	})
	mustNoErr(t, err)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("kill took too long: %v", elapsed)
	}
	// Shell should be done now.
	select {
	case <-sh.doneCh:
	case <-time.After(2 * time.Second):
		t.Fatal("shell not done after kill")
	}
}

// BS6 — bash_output called before shell exits: partial output is returned.
func TestBgStress_BS6_PartialOutputBeforeExit(t *testing.T) {
	s := newSession(t)
	sh := startBg(t, "echo BEFORE; sleep 0.5; echo AFTER")
	time.Sleep(100 * time.Millisecond) // let BEFORE land
	out, err := callTool(t, context.Background(), s, "bash_output", map[string]any{
		"shell_id": sh.ID, "reason": "t",
	})
	mustNoErr(t, err)
	mustContain(t, out, "BEFORE")
	// Wait for shell to finish.
	select {
	case <-sh.doneCh:
	case <-time.After(5 * time.Second):
		t.Fatal("shell did not finish")
	}
}

// BS7 — bash_output on a shell that exited with code 2: exit status reported.
func TestBgStress_BS7_NonZeroExitCode(t *testing.T) {
	s := newSession(t)
	sh := startBg(t, "exit 2")
	<-sh.doneCh
	out, err := callTool(t, context.Background(), s, "bash_output", map[string]any{
		"shell_id": sh.ID, "reason": "t",
	})
	mustNoErr(t, err)
	mustContain(t, out, "exit 2")
}

// BS8 — max_bytes larger than the entire log: no truncation flag.
func TestBgStress_BS8_MaxBytesLargerThanLog(t *testing.T) {
	s := newSession(t)
	sh := startBg(t, "echo small")
	<-sh.doneCh
	out, err := callTool(t, context.Background(), s, "bash_output", map[string]any{
		"shell_id": sh.ID, "max_bytes": 100000000, "reason": "t",
	})
	mustNoErr(t, err)
	mustContain(t, out, "small")
	mustNotContain(t, out, "earlier delta bytes dropped")
}

// BS9 — spawn 100 background shells rapidly; all IDs are unique; registry does
// not deadlock.
func TestBgStress_BS9_MassSpawn(t *testing.T) {
	defer bgReg.killAll()
	seen := map[string]bool{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sh, err := bgReg.start("true", "")
			if err != nil {
				return
			}
			mu.Lock()
			seen[sh.ID] = true
			mu.Unlock()
		}()
	}
	wg.Wait()
	if len(seen) != 100 {
		t.Fatalf("expected 100 unique IDs, got %d", len(seen))
	}
}

// BS10 — bash_output: max_bytes=0 should not hang (treated as "no limit" or
// as "0 bytes" returning empty but valid response).
func TestBgStress_BS10_MaxBytesZero(t *testing.T) {
	s := newSession(t)
	sh := startBg(t, "echo test")
	<-sh.doneCh
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = callTool(t, context.Background(), s, "bash_output", map[string]any{
			"shell_id": sh.ID, "max_bytes": 0, "reason": "t",
		})
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("max_bytes=0 caused hang")
	}
}
