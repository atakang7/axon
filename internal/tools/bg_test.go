package tools

import (
	"strings"
	"testing"
	"time"
)

// Two registries must be completely independent. While the registry was a
// package-level global, two Agents in one process shared it, so either one's
// Close or Reset terminated the other's background servers. This is the
// regression that change was made to prevent.
func TestRegistriesAreIndependent(t *testing.T) {
	mine := NewBackgroundShells()
	theirs := NewBackgroundShells()
	t.Cleanup(func() { mine.KillAll(); theirs.KillAll() })

	longRunning, err := mine.start("sleep 30", t.TempDir())
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := theirs.start("sleep 30", t.TempDir()); err != nil {
		t.Fatalf("start: %v", err)
	}

	// Shell IDs restart at bash_1 in every registry, so the log directories
	// must not be shared or the two bash_1 logs land in the same file and each
	// agent is served the other's output.
	if mine.LogDir() == theirs.LogDir() {
		t.Fatalf("registries share a log directory: %s", mine.LogDir())
	}

	// Killing everything on one registry must leave the other running.
	theirs.KillAll()
	if got := longRunning.status(); got != "running" {
		t.Fatalf("another registry's KillAll terminated our shell: status %q", got)
	}
}

// bash_output returns only bytes appended since the previous read. Without
// this the agent re-reads a growing log every poll and pays tokens linear in
// the process's runtime.
func TestReadNewReturnsOnlyTheDelta(t *testing.T) {
	reg := NewBackgroundShells()
	t.Cleanup(reg.KillAll)

	sh, err := reg.start(`echo first; sleep 0.4; echo second`, t.TempDir())
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	waitFor(t, func() bool {
		out, _, _ := sh.readNew(4096)
		return out != ""
	})

	// Everything already delivered; a second read sees nothing new yet.
	if out, _, err := sh.readNew(4096); err != nil {
		t.Fatalf("readNew: %v", err)
	} else if out != "" {
		t.Fatalf("re-read redelivered bytes already returned: %q", out)
	}

	<-sh.doneCh
	out, _, err := sh.readNew(4096)
	if err != nil {
		t.Fatalf("readNew: %v", err)
	}
	if want := "second"; !strings.Contains(out, want) {
		t.Fatalf("delta missing later output: got %q, want it to contain %q", out, want)
	}
	if strings.Contains(out, "first") {
		t.Fatalf("delta redelivered earlier output: %q", out)
	}
}

// Killing a shell must take its children with it. Servers routinely spawn
// child processes, and terminating only the wrapper leaves them orphaned and
// still holding the port.
func TestKillTerminatesTheProcessGroup(t *testing.T) {
	reg := NewBackgroundShells()
	t.Cleanup(reg.KillAll)

	sh, err := reg.start("sleep 30 & sleep 30", t.TempDir())
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := sh.kill(2 * time.Second); err != nil {
		t.Fatalf("kill: %v", err)
	}
	if got := sh.status(); got == "running" {
		t.Fatal("kill returned while the shell was still running")
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition not met within 3s")
}
