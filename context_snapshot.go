package axon

import "strings"

// ContextSnapshot is the next request as the model will receive it, block by
// block, with why each block looks the way it does. It exists so a person can
// audit what the agent sees and what its checkpoints chose. See
// WORKING_CONTEXT.md.
type ContextSnapshot struct {
	// Blocks are the messages of the next request, in order.
	Blocks []ContextBlock `json:"blocks"`
	// Tokens estimates the whole request, tool definitions included; Tools
	// is the part the tool definitions take.
	Tokens int `json:"tokens"`
	Tools  int `json:"tools"`
	// Trigger is the size that forces the next checkpoint, Limit the hard
	// request limit.
	Trigger int `json:"trigger"`
	Limit   int `json:"limit"`
	// Checkpoints are every checkpoint applied in this session, oldest first.
	Checkpoints []CheckpointRecord `json:"checkpoints"`
}

// ContextBlock is one message of the next request.
type ContextBlock struct {
	// ID is the block id ("m12"); empty for blocks the runtime composes, such
	// as the system prompt, cards for folded work and the goal block.
	ID   string `json:"id,omitempty"`
	Role string `json:"role"`
	// Tool names the tool for a result, or the calls an assistant message makes.
	Tool string `json:"tool,omitempty"`
	// State says why the block reads as it does:
	//   full     - sent as recorded
	//   trimmed  - a checkpoint kept only some lines
	//   dropped  - replaced by a one-line stub (Note); recall brings it back
	//   stale    - a newer copy or a later edit made it redundant
	//   folded   - a card standing for finished work
	//   system   - the system prompt
	//   goal     - the goal block
	State string `json:"state"`
	// Note is the stub's text for dropped blocks: the checkpoint's note or
	// the call's gist. Reason says who dropped it.
	Note   string `json:"note,omitempty"`
	Reason string `json:"reason,omitempty"`
	// Tokens estimates this block alone.
	Tokens int `json:"tokens"`
	// Text is exactly what is sent, tool-call arguments included.
	Text string `json:"text"`
}

// ContextSnapshot returns the next request as the model will see it. Like
// every other Agent method it must not be called while Step is running.
func (a *Agent) ContextSnapshot() ContextSnapshot {
	s := a.session
	view := s.contextView(a.settings.Pruner.ContextWindow())
	byID := map[string]Msg{}
	for _, m := range s.Messages {
		if m.ID != "" {
			byID[m.ID] = m
		}
	}
	snap := ContextSnapshot{
		Trigger:     a.checkpointTrigger(),
		Limit:       HardContextLimit,
		Checkpoints: append([]CheckpointRecord(nil), s.Checkpoints...),
	}
	if snap.Checkpoints == nil {
		snap.Checkpoints = []CheckpointRecord{}
	}
	sum := 0
	for i, v := range view {
		b := ContextBlock{ID: v.ID, Role: v.Role, Tool: v.ToolName, State: "full", Text: v.Content}
		var calls []string
		for _, tc := range v.ToolCalls {
			calls = append(calls, tc.Function.Name)
			b.Text += "\n→ " + tc.Function.Name + " " + tc.Function.Arguments
		}
		if b.Tool == "" && len(calls) > 0 {
			b.Tool = strings.Join(calls, ", ")
		}
		orig, recorded := byID[v.ID]
		switch {
		case v.ID == "" && v.Role == "system" && i == len(view)-1 && i > 0 && v.Content == s.GoalBlock():
			b.State = "goal"
		case v.ID == "" && v.Role == "system":
			b.State = "system"
		case v.ID == "" && v.Role == "assistant" && len(v.ToolCalls) == 0:
			// Cards are the only assistant messages the view composes; a call
			// without text has no id either, but is sent as recorded.
			b.State = "folded"
		case recorded && orig.Parked:
			b.State, b.Note, b.Reason = "dropped", orig.ParkSummary, orig.ParkReason
		case recorded && v.Content != orig.Content && s.Trims[v.ID] != nil:
			b.State = "trimmed"
		case recorded && v.Content != orig.Content && v.Role == "tool":
			b.State = "stale"
		}
		b.Tokens = contextTokens(len(b.Text))
		sum += b.Tokens
		snap.Blocks = append(snap.Blocks, b)
	}
	snap.Tokens = a.viewTokens()
	if snap.Tools = snap.Tokens - sum; snap.Tools < 0 {
		snap.Tools = 0
	}
	return snap
}
