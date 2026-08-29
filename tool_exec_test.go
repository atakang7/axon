package axon

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

func TestExecForegroundUsesPipefailWhenAvailable(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}

	ws := newFakeWorkspace(t.TempDir())
	tool := ExecTool(ws, NewBackgroundShells(), DefaultLimits())

	out, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{
		"command":    "false | true",
		"tail_lines": 5,
	}))
	if err != nil {
		t.Fatalf("Fn: %v", err)
	}
	if strings.Contains(out, "exit_code: 0") {
		t.Fatalf("pipeline failure was hidden by the last command: %s", out)
	}
	if !strings.Contains(out, "exit_code: 1") {
		t.Fatalf("out = %q, want exit_code: 1", out)
	}
}
