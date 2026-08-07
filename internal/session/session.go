// Package session holds everything that persists across a turn: the
// append-only message log, the working directory, the edit history behind
// undo, and the current task plan.
//
// BOUNDARY RULE: session may import llm and config. It must never import
// tools or agent, and it deliberately declares no interfaces describing its
// consumers — a tool that needs only a working directory declares that narrow
// interface on its own side, and Session satisfies it implicitly. Dependencies
// point one way: session does not know who reads it.
//
// The central invariant is that Messages is an append-only log. Park and
// forget never mutate stored entries; they set projection metadata, and
// ContextMessages (in memory.go) derives what the model sees at emission
// time. That is what keeps the audit history intact after pruning.
package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/atakang7/axon/internal/config"
	"github.com/atakang7/axon/internal/llm"
)

// Edit is one recorded file mutation: where it happened and what was there
// before. That is the whole undo contract — restoring means writing Before back
// to Path.
//
// It used to carry the post-edit contents too. Nothing ever read them: Undo
// restores Before and the file on disk already holds After. They were pure
// weight, and expensive weight, because the ledger is part of the session and
// the session is re-marshalled in full on every save.
type Edit struct {
	Path, Before string
}

// maxUndoBytes bounds the total pre-edit content the ledger retains. Undo is an
// escape hatch for the last few edits, not a version-control system, and the
// ledger is serialised into the session file on every save — so an unbounded
// one is paid for on every subsequent tool call, not just once.
//
// Measured before this cap existed: twenty edits of a 550KB file produced a
// 23MB session file and a 269ms Save, with a Save after every tool result.
const maxUndoBytes = 8 << 20

// Task is the agent's registered objective for non-trivial work. It lives on
// the Session so it persists across turns and appears on the dashboard every
// turn until the work is done. One task at a time; setting a new one overwrites
// the old. Nil means no task is registered (one-shot or not yet scoped).
type Task struct {
	Goal        string     `json:"goal"`
	Steps       []TaskStep `json:"steps,omitempty"`
	CurrentStep int        `json:"current_step,omitempty"`
}

// TaskStep is one committed step in the task plan. Done flips to true when
// the LLM declares the step complete via the task tool's advance_step action.
type TaskStep struct {
	Description string `json:"description"`
	Done        bool   `json:"done,omitempty"`
}

type Session struct {
	ID          string    `json:"id"`
	StartedAt   time.Time `json:"started_at"`
	Cwd         string    `json:"cwd,omitempty"`
	Messages    []llm.Msg `json:"messages"`
	Edits       []Edit    `json:"edits"`
	CurrentTask *Task     `json:"current_task,omitempty"`
	NextBlockID int       `json:"next_block_id,omitempty"`
	Turn        int       `json:"turn,omitempty"`
	path        string
	editsMu     sync.Mutex
}

// TaskBlock returns the formatted task string injected transiently into
// ContextMessages just before the decay-status block. Returns empty string
// when no task is registered so ContextMessages can skip it cleanly.
func (s *Session) TaskBlock() string {
	if s.CurrentTask == nil {
		return ""
	}
	t := s.CurrentTask
	var b strings.Builder
	b.WriteString("[task]")
	if len(t.Steps) > 0 && t.CurrentStep < len(t.Steps) {
		current := t.Steps[t.CurrentStep].Description
		fmt.Fprintf(&b, "\n  >>> CURRENT STEP (%d/%d): %s", t.CurrentStep+1, len(t.Steps), current)
		fmt.Fprintf(&b, "\n  >>> Do this step now; call task advance after. Keep findings scoped to: %s", t.Goal)
		b.WriteString("\n  >>> If the plan no longer fits, replan.")
	} else if len(t.Steps) > 0 {
		fmt.Fprintf(&b, "\n  >>> ALL STEPS COMPLETE — answer ONLY the goal: %s", t.Goal)
		b.WriteString("\n  >>> Omit findings not in scope. No more tools.")
	}
	fmt.Fprintf(&b, "\n  goal: %s", t.Goal)
	if len(t.Steps) > 0 {
		b.WriteString("\n  plan:")
		for i, step := range t.Steps {
			marker := "[ ]"
			if step.Done {
				marker = "[x]"
			} else if i == t.CurrentStep {
				marker = "[>]"
			}
			fmt.Fprintf(&b, "\n    %s %d. %s", marker, i+1, step.Description)
		}
	}
	return b.String()
}

func LoadOrCreateSession() *Session {
	p := config.SessionPath()
	if data, err := os.ReadFile(p); err == nil {
		var s Session
		if json.Unmarshal(data, &s) == nil {
			s.path = p
			s.ensure()
			s.assignIDs()
			return &s
		}
		backup := fmt.Sprintf("%s.corrupt.%d", p, time.Now().Unix())
		if renameErr := os.Rename(p, backup); renameErr == nil {
			fmt.Fprintf(os.Stderr, "session file corrupt; moved to %s\n", backup)
		} else {
			fmt.Fprintf(os.Stderr, "session file corrupt; could not back up: %v\n", renameErr)
		}
	}
	s := &Session{ID: fmt.Sprintf("%d", time.Now().UnixNano()), StartedAt: time.Now(), path: p}
	s.ensure()
	return s
}

// Path returns the file this session persists to. The field stays unexported
// so nothing outside this package can retarget a live session's storage by
// assignment; changing where a session lives means constructing a new one.
func (s *Session) Path() string { return s.path }

// Reset wipes the conversation and starts a fresh one in the same file.
//
// The path is carried over rather than re-resolved: an embedder that supplied
// its own session (Config.Session, or AXON_SESSION_PATH set after start) would
// otherwise find its storage silently relocated to the default per-cwd path on
// the first reset, and every later Save would write somewhere it never asked
// for. Where a session lives is decided once, at construction.
func (s *Session) Reset() {
	path := s.path
	if path == "" {
		path = config.SessionPath()
	}
	*s = Session{ID: fmt.Sprintf("%d", time.Now().UnixNano()), StartedAt: time.Now(), path: path}
	s.ensure()
}

func (s *Session) Save() error {
	s.ensure()
	os.MkdirAll(filepath.Dir(s.path), 0755)
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0600)
}

func (s *Session) ensure() {
	if s.Cwd == "" {
		if wd, err := os.Getwd(); err == nil {
			s.Cwd = wd
		}
	}
}

// assignIDs stamps a block ID on every message that lacks one and re-derives
// the high-water mark. It runs exactly once per session, at load, because that
// is the only moment a log can arrive with IDs this process did not issue.
// Append maintains NextBlockID from there, and it is persisted.
//
// This used to live in ensure(), which Append, Save, Park and ContextMessages
// all call — so recording one message re-scanned and re-parsed the entire log,
// and a single turn cost time quadratic in its own length. IDs are issued at
// append time; nothing else needs to look for them.
//
// System messages are deliberately skipped: they are never parkable, so they
// need no handle.
func (s *Session) assignIDs() {
	max := s.NextBlockID
	for i := range s.Messages {
		m := &s.Messages[i]
		if m.Role == "system" || m.Content == "" {
			continue
		}
		if m.ID == "" {
			max++
			m.ID = fmt.Sprintf("m%d", max)
			continue
		}
		var n int
		if _, err := fmt.Sscanf(m.ID, "m%d", &n); err == nil && n > max {
			max = n
		}
	}
	s.NextBlockID = max
}

// Append records one message and issues its block ID. This is the only
// supported way to grow the log: a Session whose Messages slice is assigned to
// directly gets no IDs, and its blocks are therefore invisible to the pruner.
func (s *Session) Append(m llm.Msg) {
	s.ensure()
	if m.Role != "system" && m.Content != "" && m.ID == "" {
		s.NextBlockID++
		m.ID = fmt.Sprintf("m%d", s.NextBlockID)
	}
	s.Messages = append(s.Messages, m)
}

// Dir returns the absolute working directory. It exists as a method, and not
// only as the Cwd field, so consumers can depend on a narrow interface
// (tools.Workspace) rather than on this struct.
func (s *Session) Dir() string { return s.Cwd }

func (s *Session) ResolvePath(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	base := s.Cwd
	if base == "" {
		base, _ = os.Getwd()
	}
	return filepath.Join(base, p)
}

func (s *Session) SetCwd(p string) error {
	abs := p
	if !filepath.IsAbs(abs) {
		abs = s.ResolvePath(p)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("not a directory: %s", abs)
	}
	s.Cwd = abs
	return nil
}

// RecordEdit stores the pre-edit contents of path so the write can be reverted,
// evicting the oldest entries once the ledger exceeds maxUndoBytes.
//
// The most recent edit is never evicted: a single write larger than the whole
// budget must still be undoable, since it is the one the user is most likely to
// want back.
func (s *Session) RecordEdit(path, before string) {
	s.editsMu.Lock()
	defer s.editsMu.Unlock()
	s.Edits = append(s.Edits, Edit{Path: path, Before: before})

	total := 0
	for _, e := range s.Edits {
		total += len(e.Before)
	}
	drop := 0
	for drop < len(s.Edits)-1 && total > maxUndoBytes {
		total -= len(s.Edits[drop].Before)
		drop++
	}
	if drop > 0 {
		// Re-slicing alone would keep the evicted contents alive through the
		// backing array, which is the entire thing being reclaimed here.
		s.Edits = append([]Edit(nil), s.Edits[drop:]...)
	}
}

func (s *Session) Undo() (Edit, bool) {
	s.editsMu.Lock()
	defer s.editsMu.Unlock()
	if len(s.Edits) == 0 {
		return Edit{}, false
	}
	e := s.Edits[len(s.Edits)-1]
	s.Edits = s.Edits[:len(s.Edits)-1]
	return e, true
}

// -----------------------------------------------------------------------------
// Task Management
// -----------------------------------------------------------------------------

func (s *Session) RegisterTask(goal string, steps []TaskStep) error {
	s.CurrentTask = &Task{
		Goal:        strings.TrimSpace(goal),
		Steps:       steps,
		CurrentStep: 0,
	}
	return s.Save()
}

func (s *Session) AdvanceTask() (string, error) {
	if s.CurrentTask == nil || len(s.CurrentTask.Steps) == 0 {
		return "", fmt.Errorf("no task registered")
	}
	if s.CurrentTask.CurrentStep >= len(s.CurrentTask.Steps) {
		return "", nil // all steps already complete
	}
	s.CurrentTask.Steps[s.CurrentTask.CurrentStep].Done = true
	s.CurrentTask.CurrentStep++
	if s.CurrentTask.CurrentStep >= len(s.CurrentTask.Steps) {
		return "done", s.Save()
	}
	return s.CurrentTask.Steps[s.CurrentTask.CurrentStep].Description, s.Save()
}

func (s *Session) ReplanTask(goal string, steps []TaskStep) error {
	if s.CurrentTask == nil {
		return fmt.Errorf("no task registered; use register")
	}
	if goal := strings.TrimSpace(goal); goal != "" {
		s.CurrentTask.Goal = goal
	}
	s.CurrentTask.Steps = steps
	s.CurrentTask.CurrentStep = 0
	return s.Save()
}
