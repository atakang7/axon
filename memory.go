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
//   - active block  → emit Content as-is.
//   - parked block  → emit a breadcrumb derived from the Msg's park fields.
//
// Internal bookkeeping (ID, park metadata) is stripped — the provider sees
// only role + content + tool-call fields.
func (s *Session) ContextMessages() []Msg {
	s.ensure()

	droppedToolCallIDs := map[string]bool{}
	for _, m := range s.Messages {
		if m.Parked && m.Role == "assistant" && len(m.ToolCalls) > 0 {
			for _, tc := range m.ToolCalls {
				if tc.ID != "" {
					droppedToolCallIDs[tc.ID] = true
				}
			}
		}
	}

	out := make([]Msg, 0, len(s.Messages))
	for _, m := range s.Messages {
		if m.Role == "tool" && droppedToolCallIDs[m.ToolCallID] {
			continue
		}

		content := m.Content
		toolCalls := m.ToolCalls
		if m.Parked {
			content = breadcrumb(m.ID, m.ParkReason, m.ParkSummary)
			toolCalls = nil
		}

		out = append(out, Msg{
			Role:       m.Role,
			Content:    content,
			ToolCalls:  toolCalls,
			ToolCallID: m.ToolCallID,
			ToolName:   m.ToolName,
		})
	}

	if tb := s.TaskBlock(); tb != "" {
		out = append(out, Msg{Role: "system", Content: tb})
	}

	return out
}

// breadcrumb is the one-line in-context replacement for a parked block.
func breadcrumb(id, reason, summary string) string {
	return fmt.Sprintf("[#%s parked | reason: %s | gist: %s]", id, reason, summary)
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
