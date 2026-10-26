package axon

import "strings"

// ContextSnapshot is the next request as the model will receive it, block by
// block, with why each block looks the way it does, and what is left out. It
// exists so a person can audit what the agent sees and what its checkpoints
// chose. See WORKING_CONTEXT.md.
type ContextSnapshot struct {
	// Blocks are the messages of the next request, in order.
	Blocks []ContextBlock `json:"blocks"`
	// Outside are the results the request leaves out, listed by id in its
	// index: dropped by a checkpoint or the governor, or stale.
	Outside []ContextBlock `json:"outside"`
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

// ContextBlock is one message of the next request, or one result left out.
type ContextBlock struct {
	// ID is the block id ("m12"); empty for blocks the runtime composes.
	ID   string `json:"id,omitempty"`
	Role string `json:"role"`
	// Tool names the tool for a result, or the calls an assistant message makes.
	Tool string `json:"tool,omitempty"`
	// State says why the block reads as it does:
	//   full     - sent as recorded
	//   trimmed  - a checkpoint kept only some lines
	//   folded   - a finished request, or its final answer
	//   history  - the one note for finished requests, or for finished steps
	//   system   - the system prompt, or a runtime message in the log
	//   state    - the goal and the index of what is out
	// and, for blocks left out:
	//   dropped  - dropped by a checkpoint or the governor
	//   stale    - a newer copy or a later edit made it redundant
	State string `json:"state"`
	// Note is a left-out block's line in the index; Reason says who dropped it.
	Note   string `json:"note,omitempty"`
	Reason string `json:"reason,omitempty"`
	// Tokens estimates this block alone; for a left-out block, what it would
	// cost if it were sent.
	Tokens int `json:"tokens"`
	// Text is exactly what is sent; for a left-out block, the original.
	Text string `json:"text"`
}

// ContextSnapshot returns the next request as the model will see it. Like
// every other Agent method it must not be called while Step is running.
func (a *Agent) ContextSnapshot() ContextSnapshot {
	s := a.session
	view, info := s.view(a.settings.Pruner.ContextWindow())
	snap := ContextSnapshot{
		Blocks:      []ContextBlock{},
		Outside:     []ContextBlock{},
		Trigger:     a.checkpointTrigger(),
		Limit:       HardContextLimit,
		Checkpoints: append([]CheckpointRecord{}, s.Checkpoints...),
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
		switch kind := info.kinds[i]; {
		case kind == viewFolded:
			b.State = "folded"
		case kind == viewHistory:
			b.State = "history"
		case kind == viewState:
			b.State = "state"
		case v.Role == "system":
			b.State = "system"
		case v.ID != "" && s.Trims[v.ID] != nil:
			b.State = "trimmed"
		}
		b.Tokens = contextTokens(len(b.Text))
		sum += b.Tokens
		snap.Blocks = append(snap.Blocks, b)
	}
	for _, o := range info.outside {
		snap.Outside = append(snap.Outside, ContextBlock{
			ID: o.msg.ID, Role: o.msg.Role, Tool: o.msg.ToolName, State: o.why,
			Note: o.line, Reason: o.msg.ParkReason, Text: blockText(o.msg),
			Tokens: contextTokens(len(o.msg.Content)),
		})
	}
	snap.Tokens = a.viewTokens()
	if snap.Tools = snap.Tokens - sum; snap.Tools < 0 {
		snap.Tools = 0
	}
	return snap
}
