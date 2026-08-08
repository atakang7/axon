package axon

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// sessions_list.go — enumerating saved sessions so an embedder can offer a
// switcher. Listing does not load full sessions: it reads only the header
// fields needed to present a choice, so a directory with hundreds of large
// sessions stays cheap to scan.

// SessionMeta is the lightweight description of one saved session, enough to
// list and pick without paying to unmarshal the whole transcript.
type SessionMeta struct {
	ID        string    `json:"id"`
	Cwd       string    `json:"cwd,omitempty"`
	StartedAt time.Time `json:"started_at"`
	Title     string    `json:"title,omitempty"`
	Turn      int       `json:"turn,omitempty"`
	Path      string    `json:"-"`
	Current   bool      `json:"-"`
}
// sessionHeader is the subset of Session we unmarshal while scanning. Keeping
// it small means listing a directory of large transcripts reads only the first
// page of each file before json.Unmarshal stops needing more.
type sessionHeader struct {
	ID        string    `json:"id"`
	Cwd       string    `json:"cwd,omitempty"`
	StartedAt time.Time `json:"started_at"`
	Title     string    `json:"title,omitempty"`
	Turn      int       `json:"turn,omitempty"`
}

// ListSessions scans dir for session files and returns their metadata, newest
// first. dir should be the sessions directory (the parent of the *.json
// files); an empty dir resolves to the default location.
//
// A file that cannot be read or parsed is skipped rather than failing the
// whole call: one corrupt session should not hide the rest. The current
// session, when currentPath is non-empty, is still listed but flagged Current
// so a switcher can render it distinctly.
func ListSessions(dir, currentPath string) []SessionMeta {
	if dir == "" {
		dir = filepath.Join(SessionConfig{}.dataDir(), "sessions")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var metas []SessionMeta
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if filepath.Ext(name) != ".json" {
			continue
		}

		full := filepath.Join(dir, name)
		data, err := os.ReadFile(full)
		if err != nil {
			continue
		}

		var h sessionHeader
		if json.Unmarshal(data, &h) != nil {
			continue
		}

		m := SessionMeta{
			ID:        h.ID,
			Cwd:       h.Cwd,
			StartedAt: h.StartedAt,
			Title:     h.Title,
			Turn:      h.Turn,
			Path:      full,
			Current:   currentPath != "" && full == currentPath,
		}
		metas = append(metas, m)
	}

	// Newest first, so the most recently started sessions sit at the top of a
	// picker. Ties (same timestamp, unlikely) fall back to path for a stable
	// order.
	sort.Slice(metas, func(i, j int) bool {
		if !metas[i].StartedAt.Equal(metas[j].StartedAt) {
			return metas[i].StartedAt.After(metas[j].StartedAt)
		}
		return metas[i].Path < metas[j].Path
	})

	return metas
}

// errSessionLoad is returned by SwitchSession when a target session file could
// not be loaded. It is wrapped so callers can distinguish it with errors.Is.
var errSessionLoad = errors.New("agent: could not load session")

// SwitchSession replaces the agent's current session with the one stored at
// path, rebinding the built-in tools to the new session and rebuilding the
// system prompt from the agent's role text. Background shells are killed, since
// they belong to the session being left.
//
// A path that does not exist or cannot be parsed starts a fresh session at that
// path instead; this matches LoadOrCreateSessionAt's contract, so a switch to
// a not-yet-existing file is treated as "begin a new session there" rather than
// an error.
func (a *Agent) SwitchSession(path string) error {
	if path == "" {
		return errors.New("agent: SwitchSession requires a path")
	}

	sess := LoadOrCreateSessionAt(path)
	if sess == nil {
		return errSessionLoad
	}

	a.shells.KillAll()

	a.session = sess
	a.initSessionMessages()

	a.tools = append(builtinTools(a.session, a.shells, a.limits, a.excludeBuiltins), a.customTools...)

	return nil
}
