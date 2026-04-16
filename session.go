package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Edit struct {
	Path   string `json:"path"`
	Before string `json:"before"`
	After  string `json:"after"`
}

type Session struct {
	ID        string        `json:"id"`
	StartedAt time.Time     `json:"started_at"`
	Messages  []ChatMessage `json:"messages"`
	Edits     []Edit        `json:"edits"`
	path      string
}

func sessionPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "agent", "session.json")
}

func LoadOrCreateSession() *Session {
	p := sessionPath()
	if data, err := os.ReadFile(p); err == nil {
		var s Session
		if json.Unmarshal(data, &s) == nil {
			s.path = p
			return &s
		}
	}
	return &Session{ID: fmt.Sprintf("%d", time.Now().UnixNano()), StartedAt: time.Now(), path: p}
}

func NewSession() *Session {
	return &Session{ID: fmt.Sprintf("%d", time.Now().UnixNano()), StartedAt: time.Now(), path: sessionPath()}
}

func (s *Session) Save() error {
	os.MkdirAll(filepath.Dir(s.path), 0755)
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0600)
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
