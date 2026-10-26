package axon

import (
	"fmt"
	"strings"
)

// -----------------------------------------------------------------------------
// Active message stream
// -----------------------------------------------------------------------------

// ContextMessages builds the slice of Msg sent to the model on the next call.
//
// GOLDEN RULE: Session.Messages is an immutable historical log. We never
// mutate stored Msgs to reflect park/forget state. Instead, we DERIVE the
// LLM-visible context here at emission time:
//
//   - active block, inside the window  → emit Content as-is.
//   - parked block                     → emit a breadcrumb from park fields.
//   - active block, outside the window → emit a breadcrumb too, but this is
//     a live view, not a decision: nothing is mutated, so a block that ages
//     out this call is back to full content the moment it re-enters the
//     window (e.g. the curator parks enough that the window recedes).
//
// windowBlocks is the free, no-model-call first line of defense: only the
// windowBlocks most recent active blocks are ever shown in full. Everything
// older is collapsed by default. The curator (Pruner.Prune) is the second,
// paid line of defense — it only ever needs to reason about what's already
// outside the window, since the window has already handled recency for
// free. windowBlocks <= 0 disables windowing (every active block shown).
//
// Internal bookkeeping (ID, park metadata) is stripped — the provider sees
// only role + content + tool-call fields.
func (s *Session) ContextMessages(windowBlocks int) []Msg {
	view := s.contextView(windowBlocks)
	for i := range view {
		view[i].ID = ""
	}
	return view
}

// contextView is ContextMessages with block IDs kept, for callers that must
// name blocks. windowBlocks < 0 also applies the working context layers. See
// WORKING_CONTEXT.md.
func (s *Session) contextView(windowBlocks int) []Msg {
	out, _ := s.view(windowBlocks)
	return out
}

// View kinds: what the runtime composed a message as, for ContextSnapshot.
const (
	viewRecorded = ""        // a logged message, as recorded or trimmed
	viewFolded   = "folded"  // a folded request or its final answer
	viewHistory  = "history" // the note for folded requests or steps
	viewState    = "state"   // the goal block and the index of what is out
)

// outsideBlock is a result the working view leaves out, with its index line.
type outsideBlock struct {
	msg  Msg
	why  string // "dropped" or "stale"
	line string
}

// view builds the next request and says what each message is.
//
// In the working view nothing repeats a shape:
//
//   - A folded request keeps the user's words and the final answer; what the
//     work did goes in one history note after the last folded request.
//   - The folded steps of the open request share one note where they began.
//   - Results dropped by a checkpoint or the governor, or made stale by a newer
//     copy or a later edit, leave with their calls; one index at the end, in
//     the state block, lists each by id. It is at the end because it changes
//     most often, so the cached prefix before it survives.
//   - Replies that only copied a runtime note are left out.
func (s *Session) view(windowBlocks int) ([]Msg, viewInfo) {
	s.ensure()
	var info viewInfo

	cut := windowCutoff(s, windowBlocks)
	working := windowBlocks < 0
	n := len(s.Messages)

	var uses map[string]toolUse
	folded := map[int]run{} // start index -> folded run
	var foldedRuns []run
	closedEnd := 0
	if working || len(s.Runs) > 0 {
		uses = toolUses(s)
		for _, r := range closedRuns(s) {
			closedEnd = r.end
			if s.runFolded(r) {
				folded[r.start] = r
				foldedRuns = append(foldedRuns, r)
			}
		}
	}
	lastFolded := -1
	if len(foldedRuns) > 0 {
		lastFolded = foldedRuns[len(foldedRuns)-1].start
	}

	hidden := make([]bool, n) // inside a folded run or step
	for _, r := range foldedRuns {
		for i := r.start + 1; i < r.end; i++ {
			hidden[i] = true
		}
	}
	stepSkip := map[int]int{}     // folded step start -> end
	stepNotes := map[int]string{} // where a request's folded steps begin -> their note
	if len(s.Steps) > 0 {
		if uses == nil {
			uses = toolUses(s)
		}
		for _, r := range sessionRuns(s) {
			if s.runFolded(r) {
				continue
			}
			var group []step
			for _, st := range runSteps(s, r, uses) {
				if s.stepFolded(st) {
					group = append(group, st)
					stepSkip[st.start] = st.end
					for i := st.start; i < st.end; i++ {
						hidden[i] = true
					}
				}
			}
			if len(group) > 0 {
				stepNotes[group[0].start] = stepsNote(s, group, uses)
			}
		}
	}

	elided := map[string]bool{} // tool call ids whose result is out
	if working {
		stale := staleResults(s)
		for i, m := range s.Messages {
			if m.Role != "tool" || hidden[i] || m.ToolCallID == "" || !(m.Parked || stale[i] != "") {
				continue
			}
			elided[m.ToolCallID] = true
			why := "dropped"
			if !m.Parked {
				why = "stale"
			}
			info.outside = append(info.outside, outsideBlock{msg: m, why: why, line: indexLine(s, m, uses, stale[i])})
		}
	}

	droppedToolCallIDs := map[string]bool{}
	for _, m := range s.Messages {
		if (m.Parked || cut[m.ID]) && m.Role == "assistant" && len(m.ToolCalls) > 0 {
			for _, tc := range m.ToolCalls {
				if tc.ID != "" {
					droppedToolCallIDs[tc.ID] = true
				}
			}
		}
	}

	out := make([]Msg, 0, n)
	add := func(m Msg, kind string) {
		out = append(out, m)
		info.kinds = append(info.kinds, kind)
	}
	for i := 0; i < n; i++ {
		m := s.Messages[i]
		if r, ok := folded[i]; ok {
			add(Msg{Role: "user", Content: m.Content, ID: m.ID}, viewFolded)
			if w := summarizeWork(s, r.start+1, r.end, uses); w.answer != "" {
				add(Msg{Role: "assistant", Content: truncateRunes(w.answer, 700)}, viewFolded)
			}
			if i == lastFolded {
				add(Msg{Role: "user", Content: historyNote(s, foldedRuns, uses)}, viewHistory)
			}
			i = r.end - 1
			continue
		}
		if end, ok := stepSkip[i]; ok {
			if note, ok := stepNotes[i]; ok {
				add(Msg{Role: "user", Content: note}, viewHistory)
			}
			i = end - 1
			continue
		}
		if m.Role == "tool" && (droppedToolCallIDs[m.ToolCallID] || elided[m.ToolCallID]) {
			continue
		}
		// A recorded reply that only copied a runtime note did nothing and
		// teaches the model to do it again. It stays in the log.
		if m.Role == "assistant" && len(m.ToolCalls) == 0 && looksLikeCardEcho(m.Content) {
			continue
		}

		content := m.Content
		toolCalls := m.ToolCalls
		if len(elided) > 0 && len(toolCalls) > 0 {
			kept := make([]ToolCall, 0, len(toolCalls))
			for _, tc := range toolCalls {
				if !elided[tc.ID] {
					kept = append(kept, tc)
				}
			}
			if len(kept) == 0 && strings.TrimSpace(content) == "" {
				continue
			}
			toolCalls = kept
		}
		switch {
		case m.Parked:
			content = breadcrumb(m.ID, m.ParkSummary)
			toolCalls = nil
		case cut[m.ID]:
			content = breadcrumb(m.ID, gist(s, m.ID))
			toolCalls = nil
		case m.Role == "tool" && s.Trims[m.ID] != nil:
			content = renderTrim(m, s.Trims[m.ID])
		case working && i < closedEnd && len(toolCalls) > 0:
			stubbed := make([]ToolCall, len(toolCalls))
			for j, tc := range toolCalls {
				stubbed[j] = stubWriteArgs(tc)
			}
			toolCalls = stubbed
		}

		add(Msg{
			Role:       m.Role,
			Content:    content,
			ToolCalls:  toolCalls,
			ToolCallID: m.ToolCallID,
			ToolName:   m.ToolName,
			ID:         m.ID,
		}, viewRecorded)
	}

	var state []string
	if tb := s.GoalBlock(); tb != "" {
		state = append(state, tb)
	}
	if len(info.outside) > 0 {
		lines := []string{"[out of context; recall an id to read it again]"}
		for _, o := range info.outside {
			lines = append(lines, o.line)
		}
		state = append(state, strings.Join(lines, "\n"))
	}
	if len(state) > 0 {
		add(Msg{Role: "system", Content: strings.Join(state, "\n\n")}, viewState)
	}
	return out, info
}

// viewInfo says what view composed: one kind per message, and the results
// it left out.
type viewInfo struct {
	kinds   []string
	outside []outsideBlock
}

// indexLine is one result's line in the index: its id, what the call was,
// and why it is out.
func indexLine(s *Session, m Msg, uses map[string]toolUse, stale string) string {
	what := ""
	if u, ok := uses[m.ToolCallID]; ok {
		what = callGist(u, m)
	}
	if what == "" {
		what = m.ToolName
	}
	line := "#" + m.ID + " " + what
	switch {
	case m.Parked && m.ParkSummary != "" && m.ParkSummary != what:
		line += " — " + oneLine(m.ParkSummary, 300)
	case !m.Parked && strings.Contains(stale, "superseded by #"):
		newer := stale[strings.Index(stale, "superseded by #")+len("superseded by "):]
		if j := strings.IndexAny(newer, ", ]"); j > 0 {
			newer = newer[:j]
		}
		line += " — older copy of " + newer
	case !m.Parked:
		line += " — the file was edited after this read"
	}
	return line
}

// windowCutoff returns the IDs of active blocks that fall outside the most
// recent windowBlocks — candidates for the free recency collapse above.
// Already-parked blocks don't consume window budget; they're already
// collapsed. protectedIDs (latest user, latest assistant) are never cut,
// same guarantee the curator gives, so the window can never hide the turn
// the model is actively answering.
func windowCutoff(s *Session, windowBlocks int) map[string]bool {
	cut := map[string]bool{}
	if windowBlocks <= 0 {
		return cut
	}

	protected := protectedIDs(s)

	kept := 0
	for i := len(s.Messages) - 1; i >= 0; i-- {
		m := s.Messages[i]
		if m.ID == "" || m.Parked {
			continue
		}
		if kept < windowBlocks || protected[m.ID] {
			kept++
			continue
		}
		cut[m.ID] = true
	}

	return cut
}

// breadcrumb is the one-line in-context replacement for a parked block. Park
// reasons remain in session metadata for auditability; the provider only needs
// the block id and gist.
func breadcrumb(id, summary string) string {
	return fmt.Sprintf("[#%s parked | gist: %s]", id, summary)
}

// -----------------------------------------------------------------------------
// Park — move from active to parked (called by the pruner)
// -----------------------------------------------------------------------------

func (s *Session) Park(id, summary, reason string) error {
	s.ensure()

	summary = strings.TrimSpace(summary)
	reason = strings.TrimSpace(reason)

	if reason == "" {
		return fmt.Errorf("reason is required to park a block")
	}

	for i := range s.Messages {
		m := &s.Messages[i]
		if m.ID != id {
			continue
		}

		if m.Role == "system" {
			return fmt.Errorf("cannot park system message %s", id)
		}

		if !m.Parked && m.Content == "" {
			return fmt.Errorf("block %s has no content to park", id)
		}

		m.Parked = true
		m.ParkSummary = summary
		m.ParkReason = reason

		return nil
	}

	return fmt.Errorf("block %s not found", id)
}
