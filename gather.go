package axon

import (
	"context"
	"sync"
)

// concurrentTools have no ordering dependency on each other: a run of them in
// one reply executes concurrently. Any other call is a barrier — everything
// before it finishes first, and nothing after it starts until it is done — so
// "write X, then read X" in one reply still reads the new contents. Explore
// only reads, and is the slowest of them, so it gains the most.
var concurrentTools = map[string]bool{toolRead: true, toolSearch: true, toolGlob: true, toolExplore: true}

// toolGroups splits one reply's calls into execution groups, in order.
func toolGroups(calls []ToolCall) [][]ToolCall {
	var groups [][]ToolCall
	for i := 0; i < len(calls); {
		j := i + 1
		if concurrentTools[calls[i].Function.Name] {
			for j < len(calls) && concurrentTools[calls[j].Function.Name] {
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
