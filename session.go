package axon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Edit is one recorded file mutation: path and pre-edit content for undo.
type Edit struct {
	Path, Before string
}

const maxUndoBytes = 8 << 20

// Task is the agent's registered objective for non-trivial work.
type Task struct {
	Goal        string     `json:"goal"`
	Steps       []TaskStep `json:"steps,omitempty"`
	CurrentStep int        `json:"current_step,omitempty"`
}

// TaskStep is one committed step in the task plan.
type TaskStep struct {
	Description string `json:"description"`
	Done        bool   `json:"done,omitempty"`
}

type Session struct {
	ID          string    `json:"id"`
	StartedAt   time.Time `json:"started_at"`
	Cwd         string    `json:"cwd,omitempty"`
	Messages    []Msg     `json:"messages"`
	Edits       []Edit    `json:"edits"`
	CurrentTask *Task     `json:"current_task,omitempty"`
	NextBlockID int       `json:"next_block_id,omitempty"`
	Turn        int       `json:"turn,omitempty"`
	path        string
	editsMu     sync.Mutex
}

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
		fmt.Fprintf(&b, "\n  >>> ALL STEPS COMPLETE: %s", t.Goal)
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

// LoadOrCreateSession loads the session for the current working directory
// from the default location. Agents constructed through New use
// LoadOrCreateSessionAt with the configured location instead.
func LoadOrCreateSession() *Session {
	return LoadOrCreateSessionAt(SessionPath())
}

// LoadOrCreateSessionAt loads the session stored at path, or starts a fresh
// one. A file that exists but does not parse is moved aside rather than
// deleted: a corrupt session is still the only record of what happened.
func LoadOrCreateSessionAt(p string) *Session {
	if p == "" {
		p = SessionPath()
	}

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

func (s *Session) Path() string {
	return s.path
}

func (s *Session) Reset() {
	path := s.path
	if path == "" {
		path = SessionPath()
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

func (s *Session) Append(m Msg) {
	s.ensure()

	if m.Role != "system" && m.Content != "" && m.ID == "" {
		s.NextBlockID++
		m.ID = fmt.Sprintf("m%d", s.NextBlockID)
	}

	s.Messages = append(s.Messages, m)
}

func (s *Session) Dir() string {
	return s.Cwd
}

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

// RecordEdit stores the pre-edit contents of path so the write can be reverted.
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
