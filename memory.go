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

// contextView is ContextMessages with block IDs kept, for reviewers that must
// name blocks. windowBlocks < 0 (semantic curation) also applies the working
// context layers: folded runs become cards, stale results become stubs, and
// write bodies in closed runs are dropped. See WORKING_CONTEXT.md.
func (s *Session) contextView(windowBlocks int) []Msg {
	s.ensure()

	cut := windowCutoff(s, windowBlocks)

	// Folding is state the governor recorded, so it always renders; stubs are
	// part of semantic curation only.
	working := windowBlocks < 0
	var stale map[int]string
	folded := map[int]run{} // start index -> folded run
	closedEnd := 0
	var uses map[string]toolUse
	if working || len(s.Runs) > 0 {
		uses = toolUses(s)
		for _, r := range closedRuns(s) {
			closedEnd = r.end
			if s.runFolded(r) {
				folded[r.start] = r
			}
		}
	}
	if working {
		stale = staleResults(s)
	}
	foldedSteps := map[int]step{} // start index -> folded step
	if len(s.Steps) > 0 {
		if uses == nil {
			uses = toolUses(s)
		}
		for _, r := range sessionRuns(s) {
			if s.runFolded(r) {
				continue
			}
			for _, st := range runSteps(s, r, uses) {
				if s.stepFolded(st) {
					foldedSteps[st.start] = st
				}
			}
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

	out := make([]Msg, 0, len(s.Messages))
	for i := 0; i < len(s.Messages); i++ {
		m := s.Messages[i]
		if r, ok := folded[i]; ok {
			// The request stays verbatim; the work collapses to its card.
			out = append(out, Msg{Role: "user", Content: m.Content, ID: m.ID},
				Msg{Role: "assistant", Content: runCard(s, r, uses)})
			i = r.end - 1
			continue
		}
		if st, ok := foldedSteps[i]; ok {
			out = append(out, Msg{Role: "assistant", Content: stepCard(s, st, uses)})
			i = st.end - 1
			continue
		}
		if m.Role == "tool" && droppedToolCallIDs[m.ToolCallID] {
			continue
		}

		content := m.Content
		toolCalls := m.ToolCalls
		switch {
		case m.Parked:
			content = breadcrumb(m.ID, m.ParkSummary)
			toolCalls = nil
		case cut[m.ID]:
			content = breadcrumb(m.ID, gist(s, m.ID))
			toolCalls = nil
		case stale[i] != "":
			content = stale[i]
		case m.Role == "tool" && s.Trims[m.ID] != nil:
			content = renderTrim(m, s.Trims[m.ID])
		case working && i < closedEnd && len(toolCalls) > 0:
			stubbed := make([]ToolCall, len(toolCalls))
			for j, tc := range toolCalls {
				stubbed[j] = stubWriteArgs(tc)
			}
			toolCalls = stubbed
		}

		out = append(out, Msg{
			Role:       m.Role,
			Content:    content,
			ToolCalls:  toolCalls,
			ToolCallID: m.ToolCallID,
			ToolName:   m.ToolName,
			ID:         m.ID,
		})
	}

	if tb := s.GoalBlock(); tb != "" {
		out = append(out, Msg{Role: "system", Content: tb})
	}

	return out
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
