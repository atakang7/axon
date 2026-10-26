package axon

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// Any shape that repeats in the context is a template the model is pulled
// toward; on 2026-10-26 a model copied one until it stopped calling tools.
// However much work is folded or dropped, the view says each thing once.
func TestTheWorkingViewRepeatsNoShape(t *testing.T) {
	resetMeter(t)
	b := bigHistory(t, 12)
	for _, r := range closedRuns(b.s)[:8] {
		b.s.foldRun(r)
	}
	// The open request: three finished steps (two folded) and many results,
	// half of them dropped by a checkpoint, and a command run three times.
	b.user("keep going")
	b.call(toolTask, map[string]any{"action": "register", "goal": "g", "steps": []string{"one", "two", "three", "four"}}, "ok", false)
	for step := 0; step < 3; step++ {
		for i := 0; i < 3; i++ {
			b.call(toolRead, map[string]any{"path": fmt.Sprintf("s%d_%d.go", step, i)}, numbered(1, 80, "line"), false)
		}
		b.call(toolExec, map[string]any{"command": "go test ./..."}, "exit_code: 0\n"+strings.Repeat("ok\n", 200), false)
		b.call(toolTask, map[string]any{"action": "advance"}, "ok", false)
	}
	for i := 0; i < 8; i++ {
		b.call(toolRead, map[string]any{"path": fmt.Sprintf("late%d.go", i)}, numbered(1, 120, "late"), false)
	}
	for i := 0; i < 4; i++ {
		b.call(toolSearch, map[string]any{"pattern": fmt.Sprint(i)}, "no match", false)
	}
	for _, st := range openRunSteps(b.s, toolUses(b.s))[:2] {
		b.s.foldStep(st)
	}
	var keep []map[string]any
	cands := checkpointCandidates(b.s)
	for i, m := range cands {
		if i%2 == 0 {
			keep = append(keep, map[string]any{"id": m.ID})
		}
	}
	var args checkpointArgs
	_ = json.Unmarshal(checkpointJSON(t, map[string]any{"keep": keep}), &args)
	applyCheckpoint(b.s, args, cands)

	view := b.s.contextView(-1)
	notes, history, steps := 0, 0, 0
	calls := map[string]bool{}
	for _, m := range view {
		if strings.HasPrefix(m.Content, "[runtime note") {
			notes++
			if strings.Contains(m.Content, "requests above") {
				history++
			}
			if strings.Contains(m.Content, "earlier steps") {
				steps++
			}
		}
		if m.Role == "tool" && (strings.Contains(m.Content, " parked | gist") || strings.Contains(m.Content, "superseded by") || strings.Contains(m.Content, "edited later")) {
			t.Fatalf("a stub is in context: %.80s", m.Content)
		}
		if m.Role == "assistant" && looksLikeCardEcho(m.Content) {
			t.Fatalf("a card is an assistant turn: %.80s", m.Content)
		}
		for _, tc := range m.ToolCalls {
			calls[tc.ID] = true
		}
		if m.Role == "tool" && !calls[m.ToolCallID] {
			t.Fatalf("result %s without its call", m.ToolCallID)
		}
	}
	if history != 1 || steps != 1 || notes != 2 {
		t.Fatalf("runtime notes %d (history %d, steps %d), want exactly one of each", notes, history, steps)
	}
	index := stateBlock(view)
	if strings.Count(index, "[out of context") != 1 || strings.Count(index, "\n#") < 4 {
		t.Fatalf("the index is not one list of what is out:\n%s", index)
	}
	// Every call left in context still has its result, and every result its call.
	results := map[string]bool{}
	for _, m := range view {
		if m.Role == "tool" {
			results[m.ToolCallID] = true
		}
	}
	for id := range calls {
		if !results[id] {
			t.Fatalf("call %s left without its result", id)
		}
	}
}
