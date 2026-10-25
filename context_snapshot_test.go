package axon

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestContextSnapshotShowsWhatTheModelSeesAndWhy(t *testing.T) {
	resetMeter(t)
	b, ids := checkpointFixture(t)
	// A repeated search makes the first copy stale.
	b.call(toolSearch, map[string]any{"pattern": "0"}, "no match", false)
	a := checkpointAgent(t, b, funcModel{})
	var args checkpointArgs
	_ = json.Unmarshal(checkpointJSON(t, map[string]any{
		"keep":  []map[string]any{{"id": ids[0], "lines": "1-5"}, {"id": ids[2]}},
		"notes": []map[string]any{{"id": ids[1], "note": "all packages pass"}},
	}), &args)
	out := applyCheckpoint(b.s, args, checkpointCandidates(b.s))
	a.recordCheckpoint(t.Context(), out, 9000, true)

	snap := a.ContextSnapshot()
	states := map[string]ContextBlock{}
	for _, blk := range snap.Blocks {
		if blk.ID != "" {
			states[blk.ID] = blk
		}
	}
	if snap.Blocks[0].State != "system" || snap.Blocks[0].Role != "system" {
		t.Fatalf("first block %+v", snap.Blocks[0])
	}
	if got := states[ids[0]]; got.State != "trimmed" || !strings.Contains(got.Text, "5 of 200 lines kept") {
		t.Fatalf("trimmed block: %+v", got.State)
	}
	if got := states[ids[1]]; got.State != "dropped" || got.Note != "all packages pass" || got.Reason != "checkpoint" {
		t.Fatalf("dropped block: %+v", got)
	}
	if got := states[ids[2]]; got.State != "full" || got.Tool != toolRead {
		t.Fatalf("kept block: %+v", got)
	}
	stale := 0
	for _, blk := range snap.Blocks {
		if blk.State == "stale" {
			stale++
		}
		if blk.Role == "assistant" && strings.Contains(blk.Text, "→ search") && blk.Tool != toolSearch {
			t.Fatalf("assistant block does not name its calls: %+v", blk)
		}
	}
	if stale != 1 {
		t.Fatalf("%d stale blocks, want the first copy of the repeated search", stale)
	}
	if len(snap.Checkpoints) != 1 || !snap.Checkpoints[0].Forced || snap.Trigger <= 0 || snap.Limit != HardContextLimit {
		t.Fatalf("snapshot header: %+v", snap)
	}
	sum := 0
	for _, blk := range snap.Blocks {
		sum += blk.Tokens
	}
	if snap.Tokens < sum || snap.Tools != snap.Tokens-sum {
		t.Fatalf("tokens %d tools %d blocks %d", snap.Tokens, snap.Tools, sum)
	}
}

func TestContextSnapshotMarksFoldedWorkAndTheGoal(t *testing.T) {
	resetMeter(t)
	b := bigHistory(t, 4)
	a := &Agent{session: b.s, settings: Settings{}.WithDefaults()}
	b.s.foldRun(closedRuns(b.s)[0])
	if _, err := GoalTool(b.s).Fn(t.Context(), json.RawMessage(`{"action":"criteria","criteria":[{"text":"tests pass","oracle":"go test ./..."}]}`)); err != nil {
		t.Fatal(err)
	}
	snap := a.ContextSnapshot()
	folded, goal := 0, 0
	for _, blk := range snap.Blocks {
		switch blk.State {
		case "folded":
			folded++
			if !strings.HasPrefix(blk.Text, "[finished run") {
				t.Fatalf("folded block is not a card: %.60s", blk.Text)
			}
		case "goal":
			goal++
		}
	}
	if folded != 1 || goal != 1 || snap.Blocks[len(snap.Blocks)-1].State != "goal" {
		t.Fatalf("folded %d goal %d last %q", folded, goal, snap.Blocks[len(snap.Blocks)-1].State)
	}
}
