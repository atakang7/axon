package axon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecCarriesDirectoryAndExportsToTheNextCommand(t *testing.T) {
	if !persistentShell() {
		t.Skip("bash not available")
	}
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	tool := ExecTool(newFakeWorkspace(root), NewBackgroundShellsAt(t.TempDir()), DefaultLimits())
	run := func(cmd string) string {
		t.Helper()
		out, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{"command": cmd}))
		if err != nil {
			t.Fatalf("%s: %v", cmd, err)
		}
		return out
	}

	first := run("cd sub && export AXON_PROBE=kept")
	if !strings.Contains(first, "now in") || !strings.HasPrefix(first, "$ cd sub && export AXON_PROBE=kept\n") {
		t.Fatalf("first result should show the command as given and the move:\n%s", first)
	}
	second := run("echo \"probe=$AXON_PROBE\"; ls ..")
	if !strings.Contains(second, "probe=kept") {
		t.Fatalf("export was not carried over:\n%s", second)
	}
	if !strings.Contains(second, "sub") {
		t.Fatalf("directory was not carried over:\n%s", second)
	}

	// Leaving the workspace does not stick: the next command starts at its root.
	run("cd ..")
	if third := run("ls"); !strings.Contains(third, "sub") {
		t.Fatalf("a directory outside the workspace should reset to its root:\n%s", third)
	}
}

func TestExecKeepsErrorLinesTheTailCutOff(t *testing.T) {
	out := "compiling\nmain.go:3: undefined: Foo\n" + strings.Repeat("noise\n", 40) + "FAIL build\n"
	got := errorLines(out, 30)
	if !strings.Contains(got, "2: main.go:3: undefined: Foo") {
		t.Fatalf("errorLines = %q", got)
	}
	if errorLines("all good\nok\n", 1) != "" {
		t.Fatal("no error lines should give nothing")
	}
}
