package axon

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Run notes: after a run finishes, one pruner model writes a short memory
// note for it in the background. A finished run never changes, so the note
// can never go stale and nothing waits for it; it is applied whenever it is
// ready and enriches that run's card. See WORKING_CONTEXT.md.

const (
	runNoteTimeout  = 90 * time.Second
	runNoteMinBytes = 3000
	runNoteMaxRunes = 1200
	// runNoteInputRunes caps the transcript a note is written from.
	runNoteInputRunes = 240000
)

const runNotePrompt = `You write the memory note for one finished run of a coding agent. From now on the agent sees this note instead of the run itself.
Write at most 6 short plain-text lines: what the user asked, what was done and decided, which files changed, how it was verified, what is still open.
State only facts present in the run. Use exact file paths and commands as they appear. No preamble, no markdown headings.`

type runNoteResult struct {
	session, run, summary string
}

// noteFinishedRun starts the background note for the run that just ended.
func (a *Agent) noteFinishedRun() {
	if a.pruner == nil || a.pruner.model == nil || !a.settings.Pruner.Semantic {
		return
	}
	runs := sessionRuns(a.session)
	if len(runs) == 0 {
		return
	}
	r := runs[len(runs)-1]
	id := a.session.Messages[r.start].ID
	if id == "" || runBytes(a.session, r) < runNoteMinBytes {
		return
	}
	if n := a.session.Runs[id]; n != nil && n.Summary != "" {
		return
	}
	transcript := runTranscript(a.session, r)
	model := a.pruner.summarizer()
	sessionID := a.session.ID

	a.notesMu.Lock()
	if a.notesAsked[sessionID+id] {
		a.notesMu.Unlock()
		return
	}
	if a.notesAsked == nil {
		a.notesAsked = map[string]bool{}
	}
	a.notesAsked[sessionID+id] = true
	if a.notesCancel == nil {
		ctx, cancel := context.WithCancel(context.Background())
		a.notesCancel = cancel
		a.notesCtx = ctx
	}
	parent := a.notesCtx
	a.notesMu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(parent, runNoteTimeout)
		defer cancel()
		reply, err := model.Complete(ctx, Request{Messages: []Msg{
			{Role: "system", Content: runNotePrompt},
			{Role: "user", Content: transcript},
		}})
		if err != nil || reply == nil {
			return
		}
		summary, ok := groundedNote(reply.Content, transcript)
		if !ok {
			return
		}
		a.notesMu.Lock()
		a.notes = append(a.notes, runNoteResult{session: sessionID, run: id, summary: summary})
		a.notesMu.Unlock()
	}()
}

// applyRunNotes stores finished notes on the session they were written for.
// Called on the loop, so the session is never touched concurrently.
func (a *Agent) applyRunNotes() {
	a.notesMu.Lock()
	ready := a.notes
	a.notes = nil
	a.notesMu.Unlock()
	changed := false
	for _, n := range ready {
		if n.session != a.session.ID {
			continue
		}
		if a.session.Runs == nil {
			a.session.Runs = map[string]*RunNote{}
		}
		if a.session.Runs[n.run] == nil {
			a.session.Runs[n.run] = &RunNote{}
		}
		a.session.Runs[n.run].Summary = n.summary
		changed = true
	}
	if changed {
		_ = savePrunedSession(a.session)
	}
}

func (a *Agent) stopRunNotes() {
	a.notesMu.Lock()
	defer a.notesMu.Unlock()
	if a.notesCancel != nil {
		a.notesCancel()
		a.notesCancel = nil
	}
	a.notes = nil
}

func runTranscript(s *Session, r run) string {
	var b strings.Builder
	for _, m := range s.Messages[r.start:r.end] {
		fmt.Fprintf(&b, "\n[%s %s %s]\n", m.ID, m.Role, m.ToolName)
		if m.Role == "user" {
			b.WriteString(m.Content)
		} else {
			b.WriteString(truncateRunes(m.Content, 2000))
		}
		for _, tc := range m.ToolCalls {
			fmt.Fprintf(&b, "\nCALL %s %s", tc.Function.Name, truncateRunes(stubWriteArgs(tc).Function.Arguments, 400))
		}
	}
	return truncateRunes(b.String(), runNoteInputRunes)
}

var notePathLike = regexp.MustCompile(`[\w./-]*[/\\][\w./\\-]+|\b[\w-]+\.(go|js|ts|tsx|py|md|json|ya?ml|kt|kts|java|gradle|rs|sh|ps1|toml|xml|html|css)\b`)

// groundedNote accepts a note only if every path-like token in it appears in
// the run. A note that names files the run never touched is discarded; the
// deterministic card is still there.
func groundedNote(note, transcript string) (string, bool) {
	note = strings.TrimSpace(note)
	if note == "" {
		return "", false
	}
	for _, p := range notePathLike.FindAllString(note, -1) {
		if !strings.Contains(transcript, strings.Trim(p, ".,;:()`'\"")) {
			return "", false
		}
	}
	return truncateRunes(note, runNoteMaxRunes), true
}

// summarizer is the single model that writes run notes: the first reviewer of
// a consensus pool, whose reply format is plain text rather than votes.
func (p *Pruner) summarizer() Model {
	if c, ok := p.model.(*ConsensusPruner); ok && len(c.models) > 0 {
		return c.models[0]
	}
	return p.model
}

func (p *Pruner) reviewTimeout() time.Duration {
	if p.timeout > 0 {
		return p.timeout
	}
	return 60 * time.Second
}
