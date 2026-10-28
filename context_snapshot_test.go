package axon

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestContextSnapshotShowsWhatTheModelSeesAndWhy(t *testing.T) {
	resetMeter(t)
	b, ids := checkpointFixture(t)
	// A repeated search in the open run leaves the first copy in place: the
	// open run is append-only, so its cached prefix holds.
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
	if _, in := states[ids[1]]; in {
		t.Fatal("a dropped result is among the blocks sent")
	}
	outside := map[string]ContextBlock{}
	for _, blk := range snap.Outside {
		outside[blk.ID] = blk
	}
	if got := outside[ids[1]]; got.State != "dropped" || !strings.HasSuffix(got.Note, "— all packages pass") || got.Reason != "checkpoint" || !strings.Contains(got.Text, "ok  pkg") {
		t.Fatalf("dropped block: %+v", got)
	}
	if got := states[ids[2]]; got.State != "full" || got.Tool != toolRead {
		t.Fatalf("kept block: %+v", got)
	}
	stale := 0
	for _, blk := range snap.Outside {
		if blk.State == "stale" {
			stale++
		}
	}
	for _, blk := range snap.Blocks {
		if blk.Role == "assistant" && strings.Contains(blk.Text, "→ search") && blk.Tool != toolSearch {
			t.Fatalf("assistant block does not name its calls: %+v", blk)
		}
	}
	if stale != 0 {
		t.Fatalf("%d stale blocks outside, want none: the open run is never rewritten", stale)
	}
	if last := snap.Blocks[len(snap.Blocks)-1]; last.State != "state" || !strings.Contains(last.Text, "[out of context") {
		t.Fatalf("the request does not end with the index: %+v", last)
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
	b.s.Todos = []Todo{{Content: "finish", Status: "in_progress"}}
	snap := a.ContextSnapshot()
	folded, history, state := 0, 0, 0
	for _, blk := range snap.Blocks {
		switch blk.State {
		case "folded":
			folded++
			if blk.Role == "user" && blk.Text != "request 0" {
				t.Fatalf("a folded request is not the user's words alone: %.60s", blk.Text)
			}
			if blk.Role == "assistant" && blk.Text != "done 0" {
				t.Fatalf("folded answer: %.60s", blk.Text)
			}
		case "history":
			history++
			if !strings.Contains(blk.Text, "read 6 file(s); wrote out0.go") {
				t.Fatalf("history note: %s", blk.Text)
			}
		case "state":
			state++
		}
	}
	if folded != 2 || history != 1 || state != 1 || snap.Blocks[len(snap.Blocks)-1].State != "state" {
		t.Fatalf("folded %d history %d state %d last %q", folded, history, state, snap.Blocks[len(snap.Blocks)-1].State)
	}
}
