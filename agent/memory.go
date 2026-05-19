package agent

// memory.go — context-cost management.
//
// The main agent has no memory tools and no awareness of memory state. A
// separate pruner (see pruner.go) is the only operator of Park / Forget;
// the agent's prompt does not even mention them.
//
// State model (still three-state, but driven by the pruner now):
//
//   active    — full content lives in the message stream sent to the model.
//   parked    — replaced in stream by a one-line breadcrumb. The original
//               lives in Session.ParkedBlocks for human audit.
//   forgotten — dropped from the model's view entirely (no breadcrumb). The
//               original Msg stays in Session.Messages for human audit only.
//
// Park / Forget / Refresh / Recall remain as Session methods because the
// pruner runtime calls them. They are no longer tools.
//
// Session.Messages is still the immutable log; ContextMessages is still the
// projection function that builds what the model sees.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ParkedBlock is the side-table record for a parked Msg. Self-describing:
// holds the metadata (summary, reason) AND the exact breadcrumb string that
// will be substituted into the active context for this block.
type ParkedBlock struct {
	ID              string    `json:"id"`
	Role            string    `json:"role"`
	OriginalContent string    `json:"original_content"`
	Summary         string    `json:"summary"`
	Reason          string    `json:"reason"`
	Breadcrumb      string    `json:"breadcrumb"`
	ParkedAt        time.Time `json:"parked_at"`
	ParkedAtTurn    int       `json:"parked_at_turn"`
}

// -----------------------------------------------------------------------------
// Active message stream
// -----------------------------------------------------------------------------

// ContextMessages builds the slice of Msg sent to the model on the next call.
//
// GOLDEN RULE: Session.Messages is an immutable historical log. We never
// mutate stored Msgs to reflect park/forget state. Instead, we DERIVE the
// LLM-visible context here at emission time:
//
//   - active block      → emit Content as-is.
//   - parked block      → emit the stored breadcrumb from ParkedBlocks[ID].
//   - forgotten block   → drop entirely, no breadcrumb.
//
// Internal bookkeeping (ID, park metadata) is stripped — the provider sees
// only role + content + tool-call fields.
func (s *Session) ContextMessages() []Msg {
	s.ensure()

	// First pass: any parked or forgotten assistant message that originally
	// carried tool_calls becomes content-only. The tool_calls field can hold
	// massive arguments (e.g. a 200-line file written via `write`), and
	// dropping content alone leaves those bytes in the prompt. We also note
	// which tool_call_ids vanish so their matching `tool` result messages
	// can be skipped — orphan tool messages with no preceding tool_call
	// break the API contract.
	droppedToolCallIDs := map[string]bool{}
	for _, m := range s.Messages {
		if (m.Parked || m.Forgotten) && m.Role == "assistant" && len(m.ToolCalls) > 0 {
			for _, tc := range m.ToolCalls {
				if tc.ID != "" {
					droppedToolCallIDs[tc.ID] = true
				}
			}
		}
	}

	out := make([]Msg, 0, len(s.Messages))
	for _, m := range s.Messages {
		if m.Forgotten {
			continue
		}
		if m.Role == "tool" && droppedToolCallIDs[m.ToolCallID] {
			continue
		}
		content := m.Content
		toolCalls := m.ToolCalls
		if m.Parked {
			if rec, ok := s.ParkedBlocks[m.ID]; ok && rec.Breadcrumb != "" {
				content = rec.Breadcrumb
			} else {
				content = breadcrumb(m.ID, m.ParkReason, m.ParkSummary)
			}
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
	// Task block is transient: derived at emission time, never stored. Append
	// at the TAIL so the prefix stays cache-stable across turns.
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
		if m.Parked {
			rec := s.ParkedBlocks[id]
			rec.Summary = summary
			rec.Reason = reason
			rec.Breadcrumb = breadcrumb(id, reason, summary)
			s.ParkedBlocks[id] = rec
		} else {
			if m.Content == "" {
				return fmt.Errorf("block %s has no content to park", id)
			}
			s.ParkedBlocks[id] = ParkedBlock{
				ID:              id,
				Role:            m.Role,
				OriginalContent: m.Content,
				Summary:         summary,
				Reason:          reason,
				Breadcrumb:      breadcrumb(id, reason, summary),
				ParkedAt:        time.Now(),
				ParkedAtTurn:    s.Turn,
			}
		}
		m.Parked = true
		m.ParkSummary = summary
		m.ParkReason = reason
		m.Forgotten = false
		m.ForgetReason = ""
		return nil
	}
	return fmt.Errorf("block %s not found", id)
}

// -----------------------------------------------------------------------------
// Recall — read parked content (utility for human / future use)
// -----------------------------------------------------------------------------

func (s *Session) Recall(ids []string, query string) []ParkedBlock {
	s.ensure()
	seen := map[string]bool{}
	var out []ParkedBlock

	for _, id := range ids {
		r, ok := s.ParkedBlocks[id]
		if !ok || seen[id] {
			continue
		}
		out = append(out, r)
		seen[id] = true
	}

	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return out
	}
	for _, r := range s.ParkedBlocks {
		if seen[r.ID] {
			continue
		}
		hay := strings.ToLower(r.ID + "\n" + r.Summary + "\n" + r.Reason + "\n" + r.OriginalContent)
		if strings.Contains(hay, q) {
			out = append(out, r)
		}
	}
	return out
}

// -----------------------------------------------------------------------------
// Forget — irreversible removal (pruner-driven)
// -----------------------------------------------------------------------------

func (s *Session) Forget(id, reason string) error {
	s.ensure()
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return fmt.Errorf("reason is required to forget a block")
	}
	for i := range s.Messages {
		m := &s.Messages[i]
		if m.ID != id {
			continue
		}
		if m.Role == "system" {
			return fmt.Errorf("cannot forget system message %s", id)
		}
		delete(s.ParkedBlocks, id)
		m.Parked = false
		m.ParkSummary = ""
		m.ParkReason = ""
		m.Forgotten = true
		m.ForgetReason = reason
		return nil
	}
	if _, ok := s.ParkedBlocks[id]; ok {
		delete(s.ParkedBlocks, id)
		return nil
	}
	return fmt.Errorf("block %s not found", id)
}

// -----------------------------------------------------------------------------
// TASK tool — register or update the current objective
// -----------------------------------------------------------------------------

const taskDescription = `Track a multi-step plan. Skip for one-shot work.
  - register: set goal + steps (short imperatives, ~3-7 words each).
  - advance: mark current step done, move to next.
  - replan: replace steps when the current plan no longer fits.

Goal must be phrased as the question the final answer will answer (e.g. "is anything in the blog weak for my career?" — not "review the blog"). Aim for 2-4 steps; more than 4 means you haven't scoped tightly enough — narrow the goal or split into a follow-up.`

func TaskTool(s *Session) Tool {
	type input struct {
		Action string   `json:"action"`
		Goal   string   `json:"goal"`
		Steps  []string `json:"steps"`
	}
	return Tool{
		Name:        toolTask,
		Description: taskDescription,
		Schema: obj("object", props{
			"action": enumSchema("register | advance | replan.", "register", "advance", "replan"),
			"goal":   strSchema("The question the final answer will answer. One short line. Required for register."),
			"steps":  arr(strSchema("Short imperative (~3-7 words). Aim for 2-4 total. Required for register and replan.")),
		}, []string{"action"}),
		Fn: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var p input
			if err := json.Unmarshal(raw, &p); err != nil {
				return "", err
			}
			cleanSteps := func(in []string) []TaskStep {
				out := make([]TaskStep, 0, len(in))
				for _, d := range in {
					d = strings.TrimSpace(d)
					if d == "" {
						continue
					}
					out = append(out, TaskStep{Description: d})
				}
				return out
			}
			switch p.Action {
			case "register":
				if strings.TrimSpace(p.Goal) == "" {
					return "", fmt.Errorf("goal is required for register")
				}
				steps := cleanSteps(p.Steps)
				if len(steps) == 0 {
					return "", fmt.Errorf("at least one step is required for register")
				}
				s.CurrentTask = &Task{
					Goal:        strings.TrimSpace(p.Goal),
					Steps:       steps,
					CurrentStep: 0,
				}
				msg := fmt.Sprintf("task: %s (%d steps; current: %s)",
					s.CurrentTask.Goal, len(steps), steps[0].Description)
				if len(steps) > 4 {
					msg += "\nwarning: >4 steps. Likely under-scoped — narrow the goal or split into a follow-up."
				}
				return msg, s.Save()
			case "advance":
				if s.CurrentTask == nil || len(s.CurrentTask.Steps) == 0 {
					return "", fmt.Errorf("no task registered")
				}
				if s.CurrentTask.CurrentStep >= len(s.CurrentTask.Steps) {
					return "all steps already complete", nil
				}
				s.CurrentTask.Steps[s.CurrentTask.CurrentStep].Done = true
				s.CurrentTask.CurrentStep++
				if s.CurrentTask.CurrentStep >= len(s.CurrentTask.Steps) {
					return "done — answer the user", s.Save()
				}
				return fmt.Sprintf("next → %s",
					s.CurrentTask.Steps[s.CurrentTask.CurrentStep].Description), s.Save()
			case "replan":
				if s.CurrentTask == nil {
					return "", fmt.Errorf("no task registered; use register")
				}
				steps := cleanSteps(p.Steps)
				if len(steps) == 0 {
					return "", fmt.Errorf("at least one step is required for replan")
				}
				if g := strings.TrimSpace(p.Goal); g != "" {
					s.CurrentTask.Goal = g
				}
				s.CurrentTask.Steps = steps
				s.CurrentTask.CurrentStep = 0
				msg := fmt.Sprintf("replanned: %d steps; current: %s", len(steps), steps[0].Description)
				if len(steps) > 4 {
					msg += "\nwarning: >4 steps. Likely under-scoped — narrow the goal or split into a follow-up."
				}
				return msg, s.Save()
			default:
				return "", fmt.Errorf("action is required: register | advance | replan")
			}
		},
	}
}
