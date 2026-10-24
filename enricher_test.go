package axon

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func numberedBlock(n int, special map[int]string) string {
	var lines []string
	for i := 1; i <= n; i++ {
		if s, ok := special[i]; ok {
			lines = append(lines, s)
			continue
		}
		lines = append(lines, fmt.Sprintf("line %d filler text for the block", i))
	}
	return strings.Join(lines, "\n")
}

func lastID(s *Session) string { return s.Messages[len(s.Messages)-1].ID }

func TestEnricherDecisionsAreValidatedAgainstTheBlock(t *testing.T) {
	b := newRunBuilder(t)
	b.user("fix the build")
	ids := map[string]string{}
	for _, name := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		b.call(toolRead, map[string]any{"path": name + ".go"}, numberedBlock(200, map[int]string{120: "error: undefined: Foo"}), false)
		ids[name] = lastID(b.s)
	}
	var batch []Msg
	for _, m := range b.s.Messages {
		if m.Role == "tool" {
			batch = append(batch, m)
		}
	}
	reply := strings.Join([]string{
		ids["a"] + " lines 10-20 30-40",
		ids["b"] + ` match "undefined" context 1`,
		ids["c"] + " tail 5",
		ids["d"] + ` | note "only the import list mattered: fmt, os"`,
		ids["e"] + " lines 150-900", // out of range -> kept
		ids["f"] + ` match "nothing matches this"`,
		"m999 lines 1-2", // unknown id
		// g omitted -> kept
	}, "\n")
	trims := parseEnrichment("```\n"+reply+"\n```", batch, map[string]string{ids["d"]: `{"path":"d.go"}`})
	if parseEnrichment(ids["d"]+` note "see server/main.go"`, batch, nil)[ids["d"]].Note != "" {
		t.Fatal("a note naming a file the block never mentions was accepted")
	}
	if got := trims[ids["a"]].Spans; fmt.Sprint(got) != "[[10 20] [30 40]]" {
		t.Fatalf("lines: %v", got)
	}
	if got := trims[ids["b"]].Spans; fmt.Sprint(got) != "[[119 121]]" {
		t.Fatalf("match: %v", got)
	}
	if got := trims[ids["c"]].Spans; fmt.Sprint(got) != "[[196 200]]" {
		t.Fatalf("tail: %v", got)
	}
	if trims[ids["d"]].Note == "" {
		t.Fatal("note not recorded")
	}
	for _, k := range []string{"e", "f", "g"} {
		if !trims[ids[k]].Kept {
			t.Fatalf("%s should be kept, got %+v", k, trims[ids[k]])
		}
	}
	if len(trims) != 7 {
		t.Fatalf("decisions for unknown ids leaked in: %d", len(trims))
	}
}

func TestTrimShowsOnlySelectedLinesAndRecallKeepsTheOriginal(t *testing.T) {
	b := newRunBuilder(t)
	b.user("look")
	b.call(toolRead, map[string]any{"path": "x.go"}, numberedBlock(300, map[int]string{50: "func Target() {}"}), false)
	id := lastID(b.s)
	b.s.Trims = map[string]*Trim{id: {Spans: [][2]int{{48, 52}}}}
	got := viewByID(b.s.contextView(-1))[id].Content
	if !strings.Contains(got, "trimmed to lines 48-52 of 300") || !strings.Contains(got, "func Target() {}") || strings.Contains(got, "line 200 filler") {
		t.Fatalf("trim view wrong:\n%s", got)
	}
	out, err := RecallTool(b.s).Fn(context.Background(), json.RawMessage(fmt.Sprintf(`{"id":%q}`, id)))
	if err != nil || !strings.Contains(out, "line 200 filler") {
		t.Fatal("recall lost the trimmed text")
	}
}

func TestEnricherRunsInTheBackgroundAndSurvivesANewUserMessage(t *testing.T) {
	resetMeter(t)
	b := newRunBuilder(t)
	b.user("make the APK")
	for i := 0; i < 6; i++ {
		b.call(toolExec, map[string]any{"command": fmt.Sprintf("gradle step%d", i)}, numberedBlock(150, map[int]string{140: "BUILD FAILED: missing sdk"}), false)
	}
	target := b.s.Messages[3].ID // first result, outside the tail
	for i := 0; i < 3; i++ {
		b.call(toolSearch, map[string]any{"pattern": fmt.Sprint(i)}, "none", false)
	}
	release := make(chan struct{})
	var asked string
	p := &Pruner{semantic: true, model: funcModel{fn: func(_ context.Context, r Request) (*Msg, error) {
		asked = r.Messages[1].Content
		<-release
		return &Msg{Content: target + ` match "BUILD FAILED"`}, nil
	}}}
	var events []Event
	a := &Agent{session: b.s, pruner: p, settings: Settings{Pruner: PrunerSettings{Semantic: true}},
		onEvent: func(_ context.Context, e Event) { events = append(events, e) }}
	a.maybeEnrich()
	a.maybeEnrich() // one review at a time
	// The user speaks while the review is running.
	b.user("also send it to my phone")
	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for b.s.Trims[target] == nil {
		if time.Now().After(deadline) {
			t.Fatal("enrichment never applied")
		}
		time.Sleep(10 * time.Millisecond)
		a.applyEnrichment(context.Background())
	}
	if !strings.Contains(asked, "USER REQUEST:\nmake the APK") || !strings.Contains(asked, "140| BUILD FAILED") {
		t.Fatalf("request lacks the goal or numbered lines:\n%.400s", asked)
	}
	if got := viewByID(b.s.contextView(-1))[target].Content; !strings.Contains(got, "BUILD FAILED") || strings.Contains(got, "line 20 filler") {
		t.Fatalf("trim not in view: %s", got)
	}
	enriched := false
	for _, e := range events {
		enriched = enriched || (e.Kind == KindInfo && strings.HasPrefix(e.Text, "context enriched:"))
	}
	if !enriched {
		t.Fatal("no context enriched notice")
	}
	a.stopRunNotes()
}

func taskRun(b *runBuilder) (closers []string) {
	b.user("rebuild the chat screen")
	b.call(toolTask, map[string]any{"action": "register", "goal": "is the chat rebuilt?", "steps": []string{"inspect code", "build UI", "test"}}, "task: ok", false)
	for step := 0; step < 3; step++ {
		for i := 0; i < 3; i++ {
			b.call(toolRead, map[string]any{"path": fmt.Sprintf("s%d_%d.kt", step, i)}, strings.Repeat("kotlin code\n", 300), false)
		}
		if step < 2 {
			b.call(toolTask, map[string]any{"action": "advance"}, "next", false)
			closers = append(closers, lastID(b.s))
		}
	}
	return closers
}

func TestGoalTreeFoldsFinishedStepsButKeepsTheNewest(t *testing.T) {
	b := newRunBuilder(t)
	closers := taskRun(b)
	steps := openRunSteps(b.s, toolUses(b.s))
	if len(steps) != 3 || steps[0].title != "inspect code" || steps[1].title != "build UI" || steps[2].closer != "" {
		t.Fatalf("steps %+v", steps)
	}
	a := &Agent{session: b.s, settings: Settings{Pruner: PrunerSettings{Semantic: true}}}
	a.foldStepsByPolicy(context.Background())
	if !b.s.Steps[closers[0]] || b.s.Steps[closers[1]] {
		t.Fatalf("fold state %v; want only the first step folded", b.s.Steps)
	}
	view := b.s.ContextMessages(-1)
	calls := map[string]bool{}
	card := ""
	for _, m := range view {
		for _, tc := range m.ToolCalls {
			calls[tc.ID] = true
		}
		if m.Role == "tool" && !calls[m.ToolCallID] {
			t.Fatalf("result %s without its call", m.ToolCallID)
		}
		if strings.HasPrefix(m.Content, `[finished step "inspect code"`) {
			card = m.Content
		}
	}
	if !strings.Contains(card, "read: s0_0.kt, s0_1.kt, s0_2.kt") {
		t.Fatalf("step card: %q", card)
	}
	for _, m := range view {
		if strings.Contains(m.Content, "s0_0.kt") && m.Role == "tool" {
			t.Fatal("folded step content still shown")
		}
	}
}

func TestReviewerBaselineResetsAfterFolding(t *testing.T) {
	resetMeter(t)
	b := bigHistory(t, 12)
	p := &Pruner{semantic: true, categories: true, floor: 1000, growth: 5000, lastFire: 230000}
	a := &Agent{session: b.s, pruner: p, settings: Settings{Pruner: PrunerSettings{Semantic: true}}}
	if err := a.ensureContextBudget(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if p.lastFire >= 230000 || p.lastFire != p.ContextTokens(b.s) {
		t.Fatalf("baseline %d, context %d", p.lastFire, p.ContextTokens(b.s))
	}
}

func TestGovernorFoldsStepsBeforeParkingInALongTaskRun(t *testing.T) {
	resetMeter(t)
	b := newRunBuilder(t)
	b.user("big mission")
	b.call(toolTask, map[string]any{"action": "register", "goal": "done?", "steps": []string{"one", "two", "three", "four"}}, "ok", false)
	for step := 0; step < 4; step++ {
		for i := 0; i < 8; i++ {
			b.call(toolRead, map[string]any{"path": fmt.Sprintf("big%d_%d.go", step, i)}, strings.Repeat("payload ", 1500), false)
		}
		if step < 3 {
			b.call(toolTask, map[string]any{"action": "advance"}, "next", false)
		}
	}
	a := &Agent{session: b.s, settings: Settings{Pruner: PrunerSettings{Semantic: true}}}
	if err := a.ensureContextBudget(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if n, _ := requestInputTokens(Request{Messages: b.s.ContextMessages(-1)}, ""); n > HardContextLimit {
		t.Fatalf("over budget: %d", n)
	}
	if len(b.s.Steps) == 0 {
		t.Fatal("no step folded under pressure")
	}
}
