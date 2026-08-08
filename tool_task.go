package axon

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const taskDescription = `Track a multi-step plan. Skip for one-shot work.
  - register: set goal + steps (short imperatives, ~3-7 words each).
  - advance: mark current step done, move to next.
  - replan: replace steps when the current plan no longer fits.

Goal must be phrased as the question the final answer will answer (e.g. "is anything in the blog weak for my career?" — not "review the blog"). Aim for 2-4 steps; more than 4 means you haven't scoped tightly enough — narrow the goal or split into a follow-up.`

func TaskTool(plan Plan) Tool {
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
				if err := plan.RegisterTask(p.Goal, steps); err != nil {
					return "", err
				}
				// Echo the trimmed input rather than reading the plan back:
				// RegisterTask stores exactly this, and not reading keeps Plan
				// write-only, so the prompt stays the single renderer of task state.
				msg := fmt.Sprintf("task: %s (%d steps; current: %s)",
					strings.TrimSpace(p.Goal), len(steps), steps[0].Description)
				if len(steps) > 4 {
					msg += "\nwarning: >4 steps. Likely under-scoped — narrow the goal or split into a follow-up."
				}
				return msg, nil
			case "advance":
				nextStep, err := plan.AdvanceTask()
				if err != nil {
					return "", err
				}
				if nextStep == "" {
					return "all steps already complete", nil
				}
				if nextStep == "done" {
					return "done — answer the user", nil
				}
				return fmt.Sprintf("next → %s", nextStep), nil
			case "replan":
				steps := cleanSteps(p.Steps)
				if len(steps) == 0 {
					return "", fmt.Errorf("at least one step is required for replan")
				}
				if err := plan.ReplanTask(p.Goal, steps); err != nil {
					return "", err
				}
				msg := fmt.Sprintf("replanned: %d steps; current: %s", len(steps), steps[0].Description)
				if len(steps) > 4 {
					msg += "\nwarning: >4 steps. Likely under-scoped — narrow the goal or split into a follow-up."
				}
				return msg, nil
			default:
				return "", fmt.Errorf("action is required: register | advance | replan")
			}
		},
	}
}
