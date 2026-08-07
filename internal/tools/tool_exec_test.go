package tools

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/atakang7/axon/internal/config"
)

// A command whose grandchild escapes the process group must not hang the turn
// forever.
//
// Because Stdout is an io.Writer rather than a file, os/exec pipes output
// through a copier goroutine and cmd.Wait blocks until that copier sees EOF.
// A child that called setsid survives the process-group SIGKILL and keeps the
// write end open, so EOF never arrives. Waiting on Wait unconditionally — as
// this did — hung the agent permanently with no recovery but killing it.
func TestExecDoesNotHangOnEscapedChild(t *testing.T) {
	if _, err := exec.LookPath("setsid"); err != nil {
		t.Skip("setsid not available")
	}
	ws := &dirWorkspace{dir: t.TempDir()}
	tool := ExecTool(ws, NewBackgroundShells(), config.LoadLimits())

	args, _ := json.Marshal(map[string]any{
		"command":         "setsid sleep 60 & echo started",
		"tail_lines":      10,
		"timeout_seconds": 1,
	})

	done := make(chan string, 1)
	go func() {
		out, err := tool.Fn(context.Background(), args)
		if err != nil {
			out = "error: " + err.Error()
		}
		done <- out
	}()

	select {
	case out := <-done:
		if !strings.Contains(out, "timed out") {
			t.Fatalf("expected the call to report a timeout, got:\n%s", out)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("exec never returned: an escaped child held the output pipe and Wait blocked forever")
	}
}
