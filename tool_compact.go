package axon

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// CompactTool records a checkpoint; the normal loop performs review only after
// every tool result has been appended. No transcript mutation occurs in a tool.
func CompactTool(s *Session) Tool {
	return Tool{Name: "compact", Description: `Checkpoint a completed phase and request context pruning. Use when a subtask is closed, its state is summarizable, and new progress has occurred. Do not use mid-debugging, while stuck, or repeatedly without progress. Preserve decisions, modified paths, verification results, unresolved work and next action. Include source block IDs. Carry forward still-relevant facts from prior checkpoints. This records working state; independent reviewers decide what can be parked, and recall retrieves originals.`, Schema: obj("object", props{
		"closed_unit":  map[string]any{"type": "boolean"},
		"summarizable": map[string]any{"type": "boolean"},
		"new_progress": map[string]any{"type": "boolean"},
		"not_stuck":    map[string]any{"type": "boolean"},
		"state":        strSchema("Concise factual checkpoint: decisions, changed paths, verification, unresolved work, next action. Maximum 6000 bytes. Distinguish verified facts from hypotheses."),
		"sources":      arr(strSchema("Existing evidence block ID, e.g. m12")),
	}, []string{"closed_unit", "summarizable", "new_progress", "not_stuck", "state", "sources"}), Fn: func(ctx context.Context, raw json.RawMessage) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		var in struct {
			Closed       bool     `json:"closed_unit"`
			Summarizable bool     `json:"summarizable"`
			Progress     bool     `json:"new_progress"`
			NotStuck     bool     `json:"not_stuck"`
			State        string   `json:"state"`
			Sources      []string `json:"sources"`
		}
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", err
		}
		if !in.Closed || !in.Summarizable || !in.Progress || !in.NotStuck {
			return "", fmt.Errorf("checkpoint requires a closed, summarizable phase with new progress and no stuck investigation")
		}
		in.State = strings.TrimSpace(in.State)
		if in.State == "" || len(in.State) > 6000 || len(in.Sources) == 0 || len(in.Sources) > 24 {
			return "", fmt.Errorf("provide 1-6000 bytes of working state and 1-24 source block IDs")
		}
		known := map[string]bool{}
		for _, m := range s.Messages {
			if m.Role != "system" && m.ID != "" {
				known[m.ID] = true
			}
		}
		for _, id := range in.Sources {
			if !known[id] {
				return "", fmt.Errorf("unknown evidence block %q", id)
			}
		}
		return "[working checkpoint; agent-authored, not independently verified]\n" + in.State + "\nEvidence: " + strings.Join(in.Sources, ", ") + "\nPruning requested; original evidence remains available through recall.", nil
	}}
}

func latestCheckpoint(s *Session) *Msg {
	for i := len(s.Messages) - 1; i >= 0; i-- {
		m := &s.Messages[i]
		if m.Role == "tool" && m.ToolName == "compact" && !m.ToolFailed && !m.Parked {
			return m
		}
	}
	return nil
}
