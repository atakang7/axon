package tools

// tools_test.go — shared test fakes for the internal/tools test suite.
//
// Per the package's own design contract (tools.go), a tool depends only on
// the narrow Workspace/Plan interfaces it declares, never on *session.Session
// directly. That is what makes these fakes possible in a handful of lines
// each, and it is itself the thing worth protecting: if a tool ever grew a
// dependency that these fakes could not satisfy, that would be a regression
// in the boundary, not just a missing test helper.

import (
	"path/filepath"
	"sync"
	"time"

	"github.com/atakang7/axon/internal/config"
	"github.com/atakang7/axon/internal/session"
)

// fakeWorkspace is the four-line Workspace fake the design invites. It backs
// onto a real directory (always a t.TempDir() in callers) so path resolution
// and file operations are exercised for real; only the edit ledger is faked,
// because asserting what a tool recorded is easier against a plain slice than
// against a live *session.Session.
type fakeWorkspace struct {
	dir string

	mu    sync.Mutex
	edits []recordedEdit
}

type recordedEdit struct {
	path, before string
}

func newFakeWorkspace(dir string) *fakeWorkspace { return &fakeWorkspace{dir: dir} }

func (w *fakeWorkspace) Dir() string { return w.dir }

func (w *fakeWorkspace) ResolvePath(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(w.dir, p)
}

func (w *fakeWorkspace) RecordEdit(path, before string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.edits = append(w.edits, recordedEdit{path, before})
}

func (w *fakeWorkspace) editsSnapshot() []recordedEdit {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]recordedEdit, len(w.edits))
	copy(out, w.edits)
	return out
}

// fakePlan is the four-line Plan fake. It exists so the task tool's write-only
// contract (J9) can be asserted directly: this fake never returns anything a
// caller could query — it only records what was written and returns
// caller-configured advance results.
type fakePlan struct {
	mu sync.Mutex

	registerCalls int
	lastGoal      string
	lastSteps     []session.TaskStep
	registerErr   error

	advanceResult string
	advanceErr    error
	advanceCalls  int

	replanCalls int
	replanErr   error
}

func (p *fakePlan) RegisterTask(goal string, steps []session.TaskStep) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.registerCalls++
	p.lastGoal = goal
	p.lastSteps = steps
	return p.registerErr
}

func (p *fakePlan) AdvanceTask() (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.advanceCalls++
	return p.advanceResult, p.advanceErr
}

func (p *fakePlan) ReplanTask(goal string, steps []session.TaskStep) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.replanCalls++
	p.lastGoal = goal
	p.lastSteps = steps
	return p.replanErr
}

// testLimits returns a config.Limits with every timeout cut to sub-second so
// the suite never waits on the production defaults (30s exec, 30s search).
// Individual tests override only the fields their behaviour depends on.
func testLimits() config.Limits {
	return config.Limits{
		ReadLines:    200,
		ReadMaxBytes: 2 * 1024 * 1024,

		ExecTimeout:      2 * time.Second,
		ExecMaxTimeout:   5 * time.Second,
		ExecOutputBytes:  12000,
		ExecTailLines:    50,
		ExecMaxTailLines: 500,

		BashOutputMaxBytes: 32 * 1024,

		SearchTimeout:     2 * time.Second,
		SearchMaxMatches:  100,
		SearchOutputBytes: 12000,
	}
}
