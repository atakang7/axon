package axon

import (
	"context"
	"fmt"
	"sync"
)

// readOnlyTools have no ordering dependency on each other: a run of them in
// one reply executes concurrently. Any other call is a barrier — everything
// before it finishes first, and nothing after it starts until it is done — so
// "write X, then read X" in one reply still reads the new contents.
var readOnlyTools = map[string]bool{toolRead: true, toolSearch: true}

// toolGroups splits one reply's calls into execution groups, in order.
func toolGroups(calls []ToolCall) [][]ToolCall {
	var groups [][]ToolCall
	for i := 0; i < len(calls); {
		j := i + 1
		if readOnlyTools[calls[i].Function.Name] {
			for j < len(calls) && readOnlyTools[calls[j].Function.Name] {
				j++
			}
		}
		groups = append(groups, calls[i:j])
		i = j
	}
	return groups
}

// runToolGroup executes a group and returns results in call order. Events are
// emitted by the caller, serially: the event sink is not required to be
// goroutine-safe.
func (a *Agent) runToolGroup(ctx context.Context, group []ToolCall) []Msg {
	out := make([]Msg, len(group))
	errs := make([]error, len(group))
	if len(group) == 1 {
		out[0], errs[0] = a.execTool(ctx, group[0])
	} else {
		var wg sync.WaitGroup
		for i, tc := range group {
			wg.Add(1)
			go func() {
				defer wg.Done()
				out[i], errs[i] = a.execTool(ctx, tc)
			}()
		}
		wg.Wait()
	}
	for i, err := range errs {
		if err != nil {
			a.emit(ctx, Event{Kind: KindToolError, Tool: &ToolEvent{ID: group[i].ID, Name: group[i].Function.Name}, Err: err})
		}
	}
	return out
}

// dripRoundsBeforeNudge is how many consecutive single-lookup rounds the loop
// tolerates. One follow-up lookup is normal — a result names something new.
// A third in a row means the agent is discovering its needs one round trip at
// a time, and every round trip re-sends the whole conversation.
const dripRoundsBeforeNudge = 2

// gatherTracker notices an agent fetching context one item per model call.
type gatherTracker struct {
	drip int
}

// observe records one model reply. It returns the length of the drip streak
// when the next request should carry the planning nudge, else zero. The
// streak restarts after a nudge so a persistent drip is not nagged every round.
func (g *gatherTracker) observe(calls []ToolCall) int {
	if len(calls) == 1 && readOnlyTools[calls[0].Function.Name] && !isBatchRead(calls[0]) {
		g.drip++
	} else {
		g.drip = 0
	}
	if g.drip < dripRoundsBeforeNudge {
		return 0
	}
	rounds := g.drip
	g.drip = 0
	return rounds
}

func isBatchRead(tc ToolCall) bool {
	if tc.Function.Name != toolRead {
		return false
	}
	p, _, err := parseAndValidateReadInput([]byte(tc.Function.Arguments), nopWorkspace{})
	return err == nil && len(p.Files) > 1
}

type nopWorkspace struct{}

func (nopWorkspace) Dir() string                 { return "" }
func (nopWorkspace) ResolvePath(p string) string { return p }
func (nopWorkspace) RecordEdit(string, string)   {}

// gatherNudge is appended, for one request only, to the newest tool result.
// It is never written to the session: it steers the next reply and would be
// noise in every request after that.
func gatherNudge(rounds int) string {
	return fmt.Sprintf("\n\n[runtime note: the last %d replies each fetched a single item. Before your next call, stop and plan: "+
		"what is the current step, and what exactly do you still need to know to finish it — every file, range, symbol and command? "+
		"Request all of it in this one reply, as parallel tool calls or a single read with files:[...]. "+
		"Only fetch again afterwards if a result reveals something you could not have predicted.]", rounds)
}

// withNudge returns messages with note appended to the final tool result, on
// a copy: the session's own messages are never modified. Trailing system
// messages (the goal block) are skipped; they used to make every note vanish
// once a task was registered.
func withNudge(messages []Msg, note string) []Msg {
	if note == "" {
		return messages
	}
	i := len(messages) - 1
	for i >= 0 && messages[i].Role == "system" {
		i--
	}
	if i < 0 || messages[i].Role != "tool" {
		return messages
	}
	out := append([]Msg(nil), messages...)
	out[i].Content += note
	return out
}

// withTurnNote appends a one-shot runtime note as a user message. It is never
// saved, so the session records only what the agent and user actually said.
func withTurnNote(messages []Msg, note string) []Msg {
	if note == "" {
		return messages
	}
	return append(append([]Msg(nil), messages...), Msg{Role: "user", Content: note})
}
