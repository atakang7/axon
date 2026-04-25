package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Edit struct {
	Path, Before, After string
}

// Task is the agent's registered objective for non-trivial work. It lives on
// the Session so it persists across turns and appears on the dashboard every
// turn until the work is done. One task at a time; setting a new one overwrites
// the old. Nil means no task is registered (one-shot or not yet scoped).
type Task struct {
	Objective        string `json:"objective"`
	DefinitionOfDone string `json:"definition_of_done"`
	CurrentFocus     string `json:"current_focus"`
}

type Session struct {
	ID           string                 `json:"id"`
	StartedAt    time.Time              `json:"started_at"`
	Cwd          string                 `json:"cwd,omitempty"`
	Messages     []Msg                  `json:"messages"`
	Edits        []Edit                 `json:"edits"`
	ParkedBlocks map[string]ParkedBlock `json:"parked_blocks,omitempty"`
	CurrentTask  *Task                  `json:"current_task,omitempty"`
	NextBlockID  int                    `json:"next_block_id,omitempty"`
	Turn         int                    `json:"turn,omitempty"`
	path         string
}

// TaskBlock returns the formatted task string injected transiently into
// ContextMessages just before the decay-status block. Returns empty string
// when no task is registered so ContextMessages can skip it cleanly.
func (s *Session) TaskBlock() string {
	if s.CurrentTask == nil {
		return ""
	}
	t := s.CurrentTask
	return fmt.Sprintf("[task]\n  objective:          %s\n  definition of done: %s\n  current focus:      %s",
		t.Objective, t.DefinitionOfDone, t.CurrentFocus)
}

func LoadOrCreateSession() *Session {
	p := sessionPath()
	if data, err := os.ReadFile(p); err == nil {
		var s Session
		if json.Unmarshal(data, &s) == nil {
			s.path = p
			s.ensure()
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

func (s *Session) Reset() {
	*s = Session{ID: fmt.Sprintf("%d", time.Now().UnixNano()), StartedAt: time.Now(), path: sessionPath()}
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
	if s.ParkedBlocks == nil {
		s.ParkedBlocks = map[string]ParkedBlock{}
	}
	if s.Cwd == "" {
		if wd, err := os.Getwd(); err == nil {
			s.Cwd = wd
		}
	}
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
	// Fresh non-system blocks start with a full TTL. Without this, every new
	// block would expire next decay tick — defeating the purpose of decay
	// being a per-turn signal rather than an immediate axe.
	if m.Role != "system" && !m.Parked && m.TTL == 0 {
		m.TTL = defaultActiveTTL
	}
	s.Messages = append(s.Messages, m)
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

func (s *Session) RecordEdit(path, before, after string) {
	s.Edits = append(s.Edits, Edit{path, before, after})
}

func (s *Session) Undo() (Edit, bool) {
	if len(s.Edits) == 0 {
		return Edit{}, false
	}
	e := s.Edits[len(s.Edits)-1]
	s.Edits = s.Edits[:len(s.Edits)-1]
	return e, true
}
