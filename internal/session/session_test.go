package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/atakang7/axon/internal/llm"
)

// newAt builds a session backed by a real file in t's temp dir. Tests run
// against the actual save/load path rather than a mock, so what they prove is
// what production does.
func newAt(t *testing.T, name string) *Session {
	t.Helper()
	s := &Session{path: filepath.Join(t.TempDir(), name)}
	s.ensure()
	return s
}

// Reset must not relocate a session an embedder placed deliberately. Before
// this was fixed, Reset re-resolved the default per-cwd path and every later
// Save silently wrote somewhere the embedder never asked for.
func TestResetKeepsCustomPath(t *testing.T) {
	s := newAt(t, "custom.json")
	want := s.Path()

	s.Append(llm.Msg{Role: "user", Content: "hello"})
	s.Reset()

	if got := s.Path(); got != want {
		t.Fatalf("Reset relocated storage: got %q, want %q", got, want)
	}
	if len(s.Messages) != 0 {
		t.Fatalf("Reset left %d messages behind", len(s.Messages))
	}
	if err := s.Save(); err != nil {
		t.Fatalf("Save after Reset: %v", err)
	}
}

// A parked assistant message that carried tool_calls must take its tool
// results with it. Emitting a `tool` message whose matching tool_call is gone
// is a protocol violation that providers reject outright, so this is the
// sharpest edge in the projection.
func TestContextMessagesDropsOrphanedToolResults(t *testing.T) {
	s := newAt(t, "s.json")

	s.Append(llm.Msg{Role: "user", Content: "read the file"})
	call := llm.ToolCall{ID: "call_1"}
	call.Function.Name = "read"
	call.Function.Arguments = `{"path":"x.go"}`
	s.Append(llm.Msg{Role: "assistant", Content: "reading", ToolCalls: []llm.ToolCall{call}})
	s.Append(llm.Msg{Role: "tool", ToolCallID: "call_1", ToolName: "read", Content: "file body"})

	assistantID := s.Messages[1].ID
	if err := s.Park(assistantID, "read x.go", "pruner: not needed"); err != nil {
		t.Fatalf("Park: %v", err)
	}

	for _, m := range s.ContextMessages() {
		if m.Role == "tool" && m.ToolCallID == "call_1" {
			t.Fatal("orphaned tool result survived into context after its tool_call was parked")
		}
		if len(m.ToolCalls) > 0 {
			t.Fatalf("parked assistant message still carries %d tool_calls", len(m.ToolCalls))
		}
	}

	// The log itself is append-only: nothing may be deleted by parking.
	if len(s.Messages) != 3 {
		t.Fatalf("park mutated the log: %d messages, want 3", len(s.Messages))
	}
	if s.Messages[1].Content != "reading" {
		t.Fatalf("park overwrote stored content: %q", s.Messages[1].Content)
	}
}

// Parking replaces a block with a breadcrumb rather than removing it, and
// never touches the append-only log.
func TestParkSubstitutesBreadcrumb(t *testing.T) {
	s := newAt(t, "s.json")
	s.Append(llm.Msg{Role: "user", Content: "keep me"})
	s.Append(llm.Msg{Role: "assistant", Content: "a very long answer"})

	id := s.Messages[1].ID
	if err := s.Park(id, "the answer", "pruner: stale"); err != nil {
		t.Fatalf("Park: %v", err)
	}
	out := s.ContextMessages()
	if len(out) != 2 {
		t.Fatalf("parked block should be replaced, not removed: got %d", len(out))
	}
	if !strings.Contains(out[1].Content, "parked") || !strings.Contains(out[1].Content, "the answer") {
		t.Fatalf("breadcrumb missing summary or marker: %q", out[1].Content)
	}

	// Parking twice is idempotent and re-parking refreshes the breadcrumb
	// rather than stacking state.
	if err := s.Park(id, "revised gist", "pruner: still stale"); err != nil {
		t.Fatalf("re-Park: %v", err)
	}
	if out := s.ContextMessages(); !strings.Contains(out[1].Content, "revised gist") {
		t.Fatalf("re-park did not refresh the breadcrumb: %q", out[1].Content)
	}
	if len(s.Messages) != 2 {
		t.Fatalf("park mutated the audit log: %d messages, want 2", len(s.Messages))
	}
	if s.Messages[1].Content != "a very long answer" {
		t.Fatalf("park overwrote stored content: %q", s.Messages[1].Content)
	}
}

// Appending one message must cost the same on a long log as on a short one.
// ensure() used to re-scan and fmt.Sscanf every existing ID on every Append,
// Save and ContextMessages, which made a single turn quadratic in its own
// length. Allocation is the tell: a scan of the log allocates per message,
// a constant-time append does not.
func TestAppendDoesNotScanTheLog(t *testing.T) {
	s := newAt(t, "s.json")
	for range 5000 {
		s.Append(llm.Msg{Role: "assistant", Content: "a previous block"})
	}

	perAppend := testing.AllocsPerRun(100, func() {
		s.Append(llm.Msg{Role: "assistant", Content: "one more"})
	})

	// Generous: the point is that this is a small constant rather than
	// something that grows with the 5000 blocks already recorded.
	if perAppend > 8 {
		t.Fatalf("Append made %.0f allocations on a 5000-block log; it is scanning the log", perAppend)
	}
}

// A log loaded from disk still gets IDs backfilled and its high-water mark
// re-derived — that work moved out of ensure(), it did not disappear.
func TestLoadedLogGetsIDsBackfilled(t *testing.T) {
	s := newAt(t, "s.json")
	s.Messages = []llm.Msg{
		{Role: "system", Content: "you are an agent"},
		{Role: "user", Content: "no id yet"},
		{Role: "assistant", Content: "already stamped", ID: "m7"},
	}
	s.assignIDs()

	if s.Messages[0].ID != "" {
		t.Fatalf("system message was given an ID: %q", s.Messages[0].ID)
	}
	if s.Messages[1].ID == "" {
		t.Fatal("message loaded without an ID was not backfilled")
	}
	if s.NextBlockID < 7 {
		t.Fatalf("high-water mark %d ignores the existing m7; the next append would collide", s.NextBlockID)
	}
	s.Append(llm.Msg{Role: "user", Content: "fresh"})
	if got := s.Messages[3].ID; got == "m7" {
		t.Fatalf("new block reused an existing ID: %q", got)
	}
}

// The undo ledger is part of the session, and the session is re-marshalled in
// full on every save — so an unbounded ledger is paid for on every later tool
// call. Twenty edits of a 550KB file used to produce a 23MB session file.
func TestEditLedgerIsBounded(t *testing.T) {
	s := newAt(t, "s.json")
	chunk := strings.Repeat("x", 1<<20) // 1MB per edit

	for i := range 40 {
		s.RecordEdit(fmt.Sprintf("/tmp/f%d.go", i), chunk)
	}

	total := 0
	for _, e := range s.Edits {
		total += len(e.Before)
	}
	if total > maxUndoBytes {
		t.Fatalf("ledger holds %d bytes, over the %d budget", total, maxUndoBytes)
	}
	if len(s.Edits) == 0 {
		t.Fatal("eviction emptied the ledger; the newest edit must survive")
	}

	// Eviction drops the oldest, so the most recent edit is the one Undo gets.
	e, ok := s.Undo()
	if !ok {
		t.Fatal("nothing to undo after 40 edits")
	}
	if e.Path != "/tmp/f39.go" {
		t.Fatalf("Undo returned %s, want the most recent edit", e.Path)
	}
}

// A single edit larger than the whole budget must still be undoable — it is the
// one a user is most likely to want back.
func TestOversizedEditSurvivesEviction(t *testing.T) {
	s := newAt(t, "s.json")
	s.RecordEdit("/tmp/small.go", "before")
	s.RecordEdit("/tmp/huge.go", strings.Repeat("y", maxUndoBytes+1))

	e, ok := s.Undo()
	if !ok || e.Path != "/tmp/huge.go" {
		t.Fatalf("Undo returned (%v, %v), want the oversized edit", e.Path, ok)
	}
}

// Park requires a reason, and the system prompt can never be parked — losing
// either would let the curator quietly destroy context nothing can restore.
func TestParkRefusesReasonlessAndSystem(t *testing.T) {
	s := newAt(t, "s.json")
	s.Messages = []llm.Msg{{Role: "system", Content: "you are an agent"}}
	s.Append(llm.Msg{Role: "user", Content: "hi"})
	s.assignIDs()

	if err := s.Park(s.Messages[1].ID, "gist", "  "); err == nil {
		t.Fatal("Park accepted a blank reason")
	}
	// The system message is skipped by assignIDs and never gets an ID, so it is
	// unreachable by ID; assert it survives a park attempt on every ID.
	for _, m := range s.Messages {
		_ = s.Park(m.ID, "gist", "test")
	}
	if s.ContextMessages()[0].Role != "system" {
		t.Fatal("system prompt was removed from context")
	}
	if s.ContextMessages()[0].Content != "you are an agent" {
		t.Fatal("system prompt was replaced by a breadcrumb")
	}
}

// With no session file on disk, LoadOrCreateSession must hand back a usable
// fresh session rather than an error — first run of the agent in a new
// directory is the common case, not an exceptional one.
func TestLoadOrCreateSessionCreatesFreshWhenMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist", "session.json")
	t.Setenv("AXON_SESSION_PATH", path)

	s := LoadOrCreateSession()
	if s.ID == "" {
		t.Fatal("fresh session has no ID")
	}
	if len(s.Messages) != 0 {
		t.Fatalf("fresh session has %d messages, want 0", len(s.Messages))
	}
	if s.Path() != path {
		t.Fatalf("Path() = %q, want %q", s.Path(), path)
	}
}

// A session saved to disk must come back with everything an embedder relies
// on across a restart: the message log, the undo ledger, the task, and the
// counters that keep new IDs from colliding with old ones.
func TestLoadOrCreateSessionRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.json")
	t.Setenv("AXON_SESSION_PATH", path)

	orig := LoadOrCreateSession()
	orig.Append(llm.Msg{Role: "user", Content: "hello"})
	orig.Append(llm.Msg{Role: "assistant", Content: "hi there"})
	orig.RecordEdit("/tmp/f.go", "old contents")
	if err := orig.RegisterTask("ship it", []TaskStep{{Description: "step one"}}); err != nil {
		t.Fatalf("RegisterTask: %v", err)
	}
	if err := orig.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded := LoadOrCreateSession()
	if len(loaded.Messages) != 2 {
		t.Fatalf("loaded %d messages, want 2", len(loaded.Messages))
	}
	if loaded.Messages[0].Content != "hello" || loaded.Messages[1].Content != "hi there" {
		t.Fatalf("message content not round-tripped: %+v", loaded.Messages)
	}
	if len(loaded.Edits) != 1 || loaded.Edits[0].Before != "old contents" {
		t.Fatalf("edits not round-tripped: %+v", loaded.Edits)
	}
	if loaded.CurrentTask == nil || loaded.CurrentTask.Goal != "ship it" {
		t.Fatalf("task not round-tripped: %+v", loaded.CurrentTask)
	}
	if loaded.NextBlockID != orig.NextBlockID {
		t.Fatalf("NextBlockID = %d, want %d", loaded.NextBlockID, orig.NextBlockID)
	}
	if loaded.Turn != orig.Turn {
		t.Fatalf("Turn = %d, want %d", loaded.Turn, orig.Turn)
	}
}

// A corrupt session file must never lose data or block startup: the bad
// bytes are preserved under a .corrupt.<unix> name for forensics, and the
// caller gets a working fresh session instead of an error or a crash.
func TestLoadOrCreateSessionRecoversFromCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.json")
	t.Setenv("AXON_SESSION_PATH", path)

	garbage := []byte("{not valid json at all")
	if err := os.WriteFile(path, garbage, 0600); err != nil {
		t.Fatal(err)
	}

	s := LoadOrCreateSession()
	if len(s.Messages) != 0 {
		t.Fatalf("recovered session is not fresh: %d messages", len(s.Messages))
	}

	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	var backup string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "session.json.corrupt.") {
			backup = filepath.Join(filepath.Dir(path), e.Name())
		}
	}
	if backup == "" {
		t.Fatalf("no *.corrupt.<unix> backup found among %v", entries)
	}
	got, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(garbage) {
		t.Fatalf("backup contents = %q, want the original garbage %q", got, garbage)
	}
}

// Save must create any missing parent directories (a fresh XDG data dir has
// none) and must write with 0600 — a session can carry file contents and
// tool output that should not be world- or group-readable.
func TestSaveCreatesDirsWithPrivateMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "deeper", "session.json")
	s := &Session{path: path}
	s.ensure()

	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("session file missing after Save: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Fatalf("mode = %o, want 0600", perm)
	}
}

// ensure() is the only place Cwd gets a default; it must fill an empty Cwd
// from the process's own working directory, but must never clobber a Cwd an
// embedder deliberately set (e.g. via Config.Cwd).
func TestEnsureFillsCwdWithoutOverwriting(t *testing.T) {
	s := newAt(t, "s.json")
	if s.Cwd == "" {
		t.Fatal("ensure() left Cwd empty")
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if s.Cwd != wd {
		t.Fatalf("Cwd = %q, want process cwd %q", s.Cwd, wd)
	}

	s.Cwd = "/deliberately/set"
	s.ensure()
	if s.Cwd != "/deliberately/set" {
		t.Fatalf("ensure() overwrote a set Cwd: %q", s.Cwd)
	}
}

// ResolvePath is the one place a relative tool argument becomes an absolute
// path; an absolute input must pass through untouched, and a relative one
// must resolve against the session's Cwd, not the process's.
func TestResolvePath(t *testing.T) {
	s := newAt(t, "s.json")
	s.Cwd = "/work/dir"

	if got := s.ResolvePath("/already/absolute"); got != "/already/absolute" {
		t.Fatalf("absolute path was rewritten: %q", got)
	}
	want := filepath.Join("/work/dir", "sub/file.go")
	if got := s.ResolvePath("sub/file.go"); got != want {
		t.Fatalf("relative path = %q, want %q", got, want)
	}
}

// SetCwd is the backing implementation for the cd tool: it must accept a
// relative path resolved against the current Cwd, and reject anything that
// is not a real, existing directory rather than silently pointing the
// session somewhere broken.
func TestSetCwd(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "afile")
	if err := os.WriteFile(file, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	t.Run("relative path resolves against Cwd", func(t *testing.T) {
		s := newAt(t, "s.json")
		s.Cwd = root
		if err := s.SetCwd("sub"); err != nil {
			t.Fatalf("SetCwd: %v", err)
		}
		if s.Cwd != sub {
			t.Fatalf("Cwd = %q, want %q", s.Cwd, sub)
		}
	})

	t.Run("nonexistent path is rejected", func(t *testing.T) {
		s := newAt(t, "s.json")
		s.Cwd = root
		before := s.Cwd
		if err := s.SetCwd("does-not-exist"); err == nil {
			t.Fatal("SetCwd accepted a nonexistent path")
		}
		if s.Cwd != before {
			t.Fatalf("Cwd changed despite the error: %q", s.Cwd)
		}
	})

	t.Run("a file is rejected as not a directory", func(t *testing.T) {
		s := newAt(t, "s.json")
		s.Cwd = root
		err := s.SetCwd("afile")
		if err == nil {
			t.Fatal("SetCwd accepted a file")
		}
		if !strings.Contains(err.Error(), "not a directory") {
			t.Fatalf("error = %q, want it to name \"not a directory\"", err)
		}
	})
}

// Append is the only supported way to grow the log, and its ID discipline
// matters: sequential mN IDs let the pruner and Undo address a block, system
// messages are never parkable so they get no ID, and empty-content messages
// (e.g. a tool-call-only assistant turn) get none either since there is
// nothing to park.
func TestAppendIDAssignment(t *testing.T) {
	s := newAt(t, "s.json")
	s.Append(llm.Msg{Role: "system", Content: "you are an agent"})
	s.Append(llm.Msg{Role: "user", Content: "first"})
	s.Append(llm.Msg{Role: "assistant", Content: ""}) // tool-call-only, no content
	s.Append(llm.Msg{Role: "user", Content: "second"})

	if got := s.Messages[0].ID; got != "" {
		t.Fatalf("system message got an ID: %q", got)
	}
	if got := s.Messages[1].ID; got != "m1" {
		t.Fatalf("first content message ID = %q, want m1", got)
	}
	if got := s.Messages[2].ID; got != "" {
		t.Fatalf("empty-content message got an ID: %q", got)
	}
	if got := s.Messages[3].ID; got != "m2" {
		t.Fatalf("second content message ID = %q, want m2 (sequential, skipping the ID-less ones)", got)
	}
}

// A message that already carries an ID (e.g. replayed from another session,
// or constructed by a caller directly) must keep it — Append only fills gaps,
// it never reassigns.
func TestAppendRespectsExistingID(t *testing.T) {
	s := newAt(t, "s.json")
	s.Append(llm.Msg{Role: "user", Content: "pinned", ID: "m99"})
	if got := s.Messages[0].ID; got != "m99" {
		t.Fatalf("Append overwrote a caller-supplied ID: %q", got)
	}
}

// Undo is a stack: it must return the most recently recorded edit first, and
// must report false rather than a zero-value Edit when the ledger is empty —
// callers use the bool to know whether there was anything to revert.
func TestUndoPopsLIFO(t *testing.T) {
	s := newAt(t, "s.json")
	if _, ok := s.Undo(); ok {
		t.Fatal("Undo on an empty ledger returned true")
	}

	s.RecordEdit("/a.go", "a-before")
	s.RecordEdit("/b.go", "b-before")

	e1, ok := s.Undo()
	if !ok || e1.Path != "/b.go" {
		t.Fatalf("first Undo = (%+v, %v), want the most recent edit (/b.go)", e1, ok)
	}
	e2, ok := s.Undo()
	if !ok || e2.Path != "/a.go" {
		t.Fatalf("second Undo = (%+v, %v), want the earlier edit (/a.go)", e2, ok)
	}
	if _, ok := s.Undo(); ok {
		t.Fatal("Undo returned true after the ledger was drained")
	}
}

// RecordEdit must be safe when tools running in parallel (e.g. two
// concurrent write calls) both hit the undo ledger — a data race here would
// corrupt the very history /undo relies on. Run with -race.
func TestRecordEditConcurrentCallers(t *testing.T) {
	s := newAt(t, "s.json")
	var wg sync.WaitGroup
	const n = 50
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			s.RecordEdit(fmt.Sprintf("/a/%d.go", i), "before-a")
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			s.RecordEdit(fmt.Sprintf("/b/%d.go", i), "before-b")
		}
	}()
	wg.Wait()

	if len(s.Edits) == 0 {
		t.Fatal("concurrent RecordEdit left an empty ledger")
	}
}

// RegisterTask is the write path the task tool's "register" action uses: the
// goal must be trimmed (a model-supplied goal often carries stray
// whitespace), the plan starts at step zero, and the task must be persisted
// immediately so a crash right after registering does not lose it.
func TestRegisterTaskStoresTrimmedGoalAndPersists(t *testing.T) {
	s := newAt(t, "s.json")
	if err := s.RegisterTask("  ship the thing  ", []TaskStep{{Description: "step one"}, {Description: "step two"}}); err != nil {
		t.Fatalf("RegisterTask: %v", err)
	}
	if s.CurrentTask.Goal != "ship the thing" {
		t.Fatalf("Goal = %q, want trimmed", s.CurrentTask.Goal)
	}
	if s.CurrentTask.CurrentStep != 0 {
		t.Fatalf("CurrentStep = %d, want 0", s.CurrentTask.CurrentStep)
	}
	if len(s.CurrentTask.Steps) != 2 {
		t.Fatalf("Steps = %v, want 2", s.CurrentTask.Steps)
	}

	raw, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatalf("RegisterTask did not persist: %v", err)
	}
	var onDisk Session
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatal(err)
	}
	if onDisk.CurrentTask == nil || onDisk.CurrentTask.Goal != "ship the thing" {
		t.Fatalf("persisted task = %+v, want the registered goal", onDisk.CurrentTask)
	}
}

// AdvanceTask is the write path for the task tool's "advance" action, and
// every one of its return states is meaningful to the caller: the next
// step's description while mid-plan, "done" on the last step, ("", nil) when
// called again after completion (idempotent, not an error), and an explicit
// error when there is nothing to advance.
func TestAdvanceTask(t *testing.T) {
	t.Run("errors with no task registered", func(t *testing.T) {
		s := newAt(t, "s.json")
		_, err := s.AdvanceTask()
		if err == nil || !strings.Contains(err.Error(), "no task registered") {
			t.Fatalf("err = %v, want \"no task registered\"", err)
		}
	})

	t.Run("errors when the task has no steps", func(t *testing.T) {
		s := newAt(t, "s.json")
		s.CurrentTask = &Task{Goal: "g"}
		_, err := s.AdvanceTask()
		if err == nil || !strings.Contains(err.Error(), "no task registered") {
			t.Fatalf("err = %v, want \"no task registered\"", err)
		}
	})

	t.Run("mid-plan returns the next step and marks the current one done", func(t *testing.T) {
		s := newAt(t, "s.json")
		if err := s.RegisterTask("g", []TaskStep{{Description: "one"}, {Description: "two"}, {Description: "three"}}); err != nil {
			t.Fatal(err)
		}
		next, err := s.AdvanceTask()
		if err != nil {
			t.Fatalf("AdvanceTask: %v", err)
		}
		if next != "two" {
			t.Fatalf("next = %q, want %q", next, "two")
		}
		if !s.CurrentTask.Steps[0].Done {
			t.Fatal("completed step not marked Done")
		}
		if s.CurrentTask.CurrentStep != 1 {
			t.Fatalf("CurrentStep = %d, want 1", s.CurrentTask.CurrentStep)
		}
	})

	t.Run("the last step returns done", func(t *testing.T) {
		s := newAt(t, "s.json")
		if err := s.RegisterTask("g", []TaskStep{{Description: "only"}}); err != nil {
			t.Fatal(err)
		}
		next, err := s.AdvanceTask()
		if err != nil {
			t.Fatalf("AdvanceTask: %v", err)
		}
		if next != "done" {
			t.Fatalf("next = %q, want done", next)
		}
	})

	t.Run("advancing past the end returns empty string and nil error", func(t *testing.T) {
		s := newAt(t, "s.json")
		if err := s.RegisterTask("g", []TaskStep{{Description: "only"}}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.AdvanceTask(); err != nil {
			t.Fatal(err)
		}
		next, err := s.AdvanceTask()
		if err != nil {
			t.Fatalf("AdvanceTask past the end returned an error: %v", err)
		}
		if next != "" {
			t.Fatalf("next = %q, want empty string", next)
		}
	})
}

// ReplanTask is the write path for the task tool's "replan" action: it must
// refuse to invent a task out of nothing, must keep the existing goal when
// the model sends a blank one (a common shorthand for "same goal, new
// steps"), and must restart progress at step zero since the old step
// indices no longer mean anything against a new plan.
func TestReplanTask(t *testing.T) {
	t.Run("errors with no task registered", func(t *testing.T) {
		s := newAt(t, "s.json")
		err := s.ReplanTask("new goal", []TaskStep{{Description: "a"}})
		if err == nil || !strings.Contains(err.Error(), "no task registered") {
			t.Fatalf("err = %v, want \"no task registered\"", err)
		}
	})

	t.Run("blank goal keeps the existing goal", func(t *testing.T) {
		s := newAt(t, "s.json")
		if err := s.RegisterTask("original goal", []TaskStep{{Description: "a"}}); err != nil {
			t.Fatal(err)
		}
		if err := s.ReplanTask("   ", []TaskStep{{Description: "b"}, {Description: "c"}}); err != nil {
			t.Fatalf("ReplanTask: %v", err)
		}
		if s.CurrentTask.Goal != "original goal" {
			t.Fatalf("Goal = %q, want it unchanged", s.CurrentTask.Goal)
		}
	})

	t.Run("resets CurrentStep to 0", func(t *testing.T) {
		s := newAt(t, "s.json")
		if err := s.RegisterTask("g", []TaskStep{{Description: "a"}, {Description: "b"}}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.AdvanceTask(); err != nil {
			t.Fatal(err)
		}
		if s.CurrentTask.CurrentStep != 1 {
			t.Fatal("test setup: expected CurrentStep to advance")
		}
		if err := s.ReplanTask("new goal", []TaskStep{{Description: "x"}}); err != nil {
			t.Fatalf("ReplanTask: %v", err)
		}
		if s.CurrentTask.CurrentStep != 0 {
			t.Fatalf("CurrentStep = %d, want reset to 0", s.CurrentTask.CurrentStep)
		}
		if s.CurrentTask.Goal != "new goal" {
			t.Fatalf("Goal = %q, want %q", s.CurrentTask.Goal, "new goal")
		}
	})
}

// TaskBlock is what actually reaches the model every turn — it must render
// nothing when there is no task, the mid-plan variant with the current step
// highlighted, the all-complete variant once CurrentStep reaches len(Steps),
// and the right marker ([x]/[>]/[ ]) for done, current and pending steps.
func TestTaskBlockRendering(t *testing.T) {
	s := newAt(t, "s.json")
	if got := s.TaskBlock(); got != "" {
		t.Fatalf("TaskBlock with no task = %q, want empty", got)
	}

	s.CurrentTask = &Task{
		Goal: "ship it",
		Steps: []TaskStep{
			{Description: "one", Done: true},
			{Description: "two"},
			{Description: "three"},
		},
		CurrentStep: 1,
	}
	mid := s.TaskBlock()
	if !strings.Contains(mid, ">>> CURRENT STEP (2/3): two") {
		t.Fatalf("mid-plan block missing current-step banner: %q", mid)
	}
	if !strings.Contains(mid, "[x] 1. one") {
		t.Fatalf("mid-plan block missing done marker: %q", mid)
	}
	if !strings.Contains(mid, "[>] 2. two") {
		t.Fatalf("mid-plan block missing current marker: %q", mid)
	}
	if !strings.Contains(mid, "[ ] 3. three") {
		t.Fatalf("mid-plan block missing pending marker: %q", mid)
	}

	s.CurrentTask.CurrentStep = len(s.CurrentTask.Steps)
	done := s.TaskBlock()
	if !strings.Contains(done, "ALL STEPS COMPLETE") {
		t.Fatalf("completed block missing the ALL STEPS COMPLETE banner: %q", done)
	}
	if strings.Contains(done, "CURRENT STEP") {
		t.Fatalf("completed block should not still show a current step: %q", done)
	}
}
