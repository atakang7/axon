package main

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

const taskDescription = `Register or advance the task plan.
  - action=register: commit objective, definition_of_done, hypothesis, and a step list (>=2 steps).
  - action=advance: mark the current step done and move to the next.
  - action=replan: replace the hypothesis and step list (use when a step's result contradicts the hypothesis the plan was built on).
Plan is conditional on the hypothesis. If the hypothesis turns out wrong, replan — do not keep executing a plan whose foundation has collapsed.`

func TaskTool(s *Session) Tool {
	type input struct {
		Action           string   `json:"action"`
		Objective        string   `json:"objective"`
		DefinitionOfDone string   `json:"definition_of_done"`
		Hypothesis       string   `json:"hypothesis"`
		Steps            []string `json:"steps"`
		Reason           string   `json:"reason"`
	}
	return Tool{
		Name:        toolTask,
		Description: taskDescription,
		Schema: obj("object", props{
			"action":             enumSchema("register | advance | replan. Required.", "register", "advance", "replan"),
			"objective":          strSchema("Precise statement of what is being accomplished. One sentence. Required for register and replan."),
			"definition_of_done": strSchema("The observable condition under which the task is complete. Required for register."),
			"hypothesis":         strSchema("Your best current explanation of the cause / shape of the problem, in one sentence. The plan is built on this. Required for register and replan. If you don't have one, the task is exploratory — say so explicitly."),
			"steps":              arr(strSchema("One concrete step. Each step is a single sentence: an action you will execute, not a description of an area to think about. Required for register and replan; min 2 steps.")),
			"reason":             reasonField(),
		}, []string{"action", "reason"}),
		Fn: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var p input
			if err := json.Unmarshal(raw, &p); err != nil {
				return "", err
			}
			if err := requireReason(p.Reason); err != nil {
				return "", err
			}
			switch p.Action {
			case "register":
				if strings.TrimSpace(p.Objective) == "" || strings.TrimSpace(p.DefinitionOfDone) == "" {
					return "", fmt.Errorf("objective and definition_of_done are required for action=register")
				}
				if strings.TrimSpace(p.Hypothesis) == "" {
					return "", fmt.Errorf("hypothesis is required for action=register; state your best current explanation of the cause/shape of the problem, or say 'exploratory: no hypothesis yet' if the task is genuinely open-ended")
				}
				if len(p.Steps) < 2 {
					return "", fmt.Errorf("at least 2 steps are required for action=register; if the task is one tool call, do not register a task")
				}
				steps := make([]TaskStep, 0, len(p.Steps))
				for _, d := range p.Steps {
					d = strings.TrimSpace(d)
					if d == "" {
						continue
					}
					steps = append(steps, TaskStep{Description: d})
				}
				s.CurrentTask = &Task{
					Objective:        strings.TrimSpace(p.Objective),
					DefinitionOfDone: strings.TrimSpace(p.DefinitionOfDone),
					Hypothesis:       strings.TrimSpace(p.Hypothesis),
					Steps:            steps,
					CurrentStep:      0,
				}
				return fmt.Sprintf("task registered: %s (%d steps; current: %s)",
					s.CurrentTask.Objective, len(steps), steps[0].Description), s.Save()
			case "advance":
				if s.CurrentTask == nil || len(s.CurrentTask.Steps) == 0 {
					return "", fmt.Errorf("no task with steps registered")
				}
				if s.CurrentTask.CurrentStep >= len(s.CurrentTask.Steps) {
					return "all steps already complete", nil
				}
				s.CurrentTask.Steps[s.CurrentTask.CurrentStep].Done = true
				s.CurrentTask.CurrentStep++
				if s.CurrentTask.CurrentStep >= len(s.CurrentTask.Steps) {
					return "step complete; plan finished — produce the final answer to the user", s.Save()
				}
				next := s.CurrentTask.Steps[s.CurrentTask.CurrentStep].Description
				return fmt.Sprintf("step complete; current step → %s", next), s.Save()
			case "replan":
				if s.CurrentTask == nil {
					return "", fmt.Errorf("no task registered to replan; use action=register")
				}
				if strings.TrimSpace(p.Hypothesis) == "" {
					return "", fmt.Errorf("hypothesis is required for action=replan; replan exists because the previous hypothesis failed — state the new one")
				}
				if len(p.Steps) < 2 {
					return "", fmt.Errorf("at least 2 steps are required for action=replan")
				}
				steps := make([]TaskStep, 0, len(p.Steps))
				for _, d := range p.Steps {
					d = strings.TrimSpace(d)
					if d == "" {
						continue
					}
					steps = append(steps, TaskStep{Description: d})
				}
				if strings.TrimSpace(p.Objective) != "" {
					s.CurrentTask.Objective = strings.TrimSpace(p.Objective)
				}
				s.CurrentTask.Hypothesis = strings.TrimSpace(p.Hypothesis)
				s.CurrentTask.Steps = steps
				s.CurrentTask.CurrentStep = 0
				return fmt.Sprintf("replanned: %d steps; current: %s", len(steps), steps[0].Description), s.Save()
			default:
				return "", fmt.Errorf("action is required: register | advance | replan")
			}
		},
	}
}
