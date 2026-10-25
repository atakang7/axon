package axon

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

type runBuilder struct {
	s *Session
	n int
}

func (b *runBuilder) call(name string, args map[string]any, result string, failed bool) {
	b.n++
	id := fmt.Sprintf("c%d", b.n)
	raw, _ := json.Marshal(args)
	tc := ToolCall{ID: id, Type: "function"}
	tc.Function.Name, tc.Function.Arguments = name, string(raw)
	b.s.Append(Msg{Role: "assistant", ToolCalls: []ToolCall{tc}})
	b.s.Append(Msg{Role: "tool", ToolCallID: id, ToolName: name, Content: result, ToolFailed: failed})
}

func (b *runBuilder) user(text string)      { b.s.Append(Msg{Role: "user", Content: text}) }
func (b *runBuilder) assistant(text string) { b.s.Append(Msg{Role: "assistant", Content: text}) }

func newRunBuilder(t *testing.T) *runBuilder {
	s := newTestSession(t)
	s.Append(Msg{Role: "system", Content: "system prompt"})
	return &runBuilder{s: s}
}

// openRunTail adds a current run long enough that the tail rule does not
// protect the closed runs under test.
func (b *runBuilder) openRunTail() {
	b.user("next request")
	for i := 0; i < 4; i++ {
		b.call(toolSearch, map[string]any{"pattern": fmt.Sprintf("p%d", i)}, "no match", false)
	}
}

func viewByID(view []Msg) map[string]Msg {
	out := map[string]Msg{}
	for _, m := range view {
		if m.ID != "" {
			out[m.ID] = m
		}
	}
	return out
}

func TestStaleCacheEntriesBecomeStubsWithoutTouchingTheLog(t *testing.T) {
	b := newRunBuilder(t)
	b.user("fix a.go")
	b.call(toolRead, map[string]any{"path": "a.go"}, "package a // v1 "+strings.Repeat("x", 3000), false)
	firstRead := b.s.Messages[len(b.s.Messages)-1].ID
	b.call(toolExec, map[string]any{"command": "go test ./..."}, "FAIL old", false)
	oldTest := b.s.Messages[len(b.s.Messages)-1].ID
	b.call(toolWrite, map[string]any{"path": "a.go", "mode": "save", "content": strings.Repeat("new body ", 200)}, "wrote a.go", false)
	b.call(toolExec, map[string]any{"command": "go test ./..."}, "ok", false)
	b.assistant("fixed a.go")
	b.openRunTail()
	original := b.s.Messages[2].Content

	view := viewByID(b.s.contextView(-1))
	if got := view[firstRead].Content; !strings.Contains(got, "edited later") || !strings.Contains(got, "recall "+firstRead) {
		t.Fatalf("read of a file edited later was not stubbed: %q", got)
	}
	if got := view[oldTest].Content; !strings.Contains(got, "superseded") {
		t.Fatalf("repeated command output was not stubbed: %q", got)
	}
	for _, m := range b.s.contextView(-1) {
		for _, tc := range m.ToolCalls {
			if tc.Function.Name == toolWrite && strings.Contains(tc.Function.Arguments, "new body") {
				t.Fatal("write body of a closed run is still in context")
			}
		}
	}
	if b.s.Messages[2].Content != original {
		t.Fatal("the log itself was rewritten")
	}
	// Windowed (non-semantic) agents see the log unchanged.
	for _, m := range b.s.ContextMessages(0) {
		if strings.Contains(m.Content, "superseded") || strings.Contains(m.Content, "edited later") {
			t.Fatal("stubs leaked into a non-semantic view")
		}
	}
}

func TestOpenRunKeepsReadsOfFilesItIsEditing(t *testing.T) {
	b := newRunBuilder(t)
	b.user("edit b.go")
	b.call(toolRead, map[string]any{"path": "b.go"}, strings.Repeat("b", 2000), false)
	read := b.s.Messages[len(b.s.Messages)-1].ID
	b.call(toolWrite, map[string]any{"path": "b.go", "mode": "replace_string", "old_str": "b", "content": "c"}, "ok", false)
	for i := 0; i < 4; i++ {
		b.call(toolSearch, map[string]any{"pattern": fmt.Sprint(i)}, "none", false)
	}
	if got := viewByID(b.s.contextView(-1))[read].Content; strings.Contains(got, "edited later") {
		t.Fatal("the current run lost the read it is editing from")
	}
}

func TestFoldedRunKeepsTheRequestVerbatimAndPairsStayValid(t *testing.T) {
	b := newRunBuilder(t)
	request := "please make the build green and keep the API unchanged"
	b.user(request)
	b.call(toolRead, map[string]any{"path": "pkg/a.go"}, strings.Repeat("a", 4000), false)
	b.call(toolWrite, map[string]any{"path": "pkg/a.go", "mode": "save", "content": strings.Repeat("z", 4000)}, "ok", false)
	b.call(toolExec, map[string]any{"command": "go build ./..."}, "boom", true)
	failure := b.s.Messages[len(b.s.Messages)-1].ID
	b.assistant("build still fails in pkg/a.go")
	b.openRunTail()
	runs := closedRuns(b.s)
	if len(runs) != 1 || !b.s.foldRun(runs[0]) {
		t.Fatal("could not fold the finished run")
	}
	if b.s.foldRun(runs[0]) {
		t.Fatal("folding is not idempotent")
	}

	view := b.s.ContextMessages(-1)
	var card string
	calls := map[string]bool{}
	for i, m := range view {
		if m.Content == request && i+1 < len(view) {
			card = view[i+1].Content
		}
		for _, tc := range m.ToolCalls {
			calls[tc.ID] = true
		}
		if m.Role == "tool" && !calls[m.ToolCallID] {
			t.Fatalf("tool result %s has no call before it", m.ToolCallID)
		}
		if strings.Contains(m.Content, strings.Repeat("a", 4000)) {
			t.Fatal("folded run content is still in context")
		}
	}
	for _, want := range []string{"read: pkg/a.go", "wrote: pkg/a.go", "still failing at the end: go build ./... (#" + failure + ")", "final answer: build still fails"} {
		if !strings.Contains(card, want) {
			t.Fatalf("card lacks %q:\n%s", want, card)
		}
	}
	out, err := RecallTool(b.s).Fn(context.Background(), json.RawMessage(fmt.Sprintf(`{"id":%q}`, failure)))
	if err != nil || !strings.Contains(out, "boom") {
		t.Fatalf("recall of a folded block failed: %v %q", err, out)
	}
}

func bigHistory(t *testing.T, runs int) *runBuilder {
	b := newRunBuilder(t)
	for r := 0; r < runs; r++ {
		b.user(fmt.Sprintf("request %d", r))
		for i := 0; i < 6; i++ {
			b.call(toolRead, map[string]any{"path": fmt.Sprintf("f%d_%d.go", r, i)}, strings.Repeat("source line\n", 600), false)
		}
		b.call(toolWrite, map[string]any{"path": fmt.Sprintf("out%d.go", r), "mode": "save", "content": strings.Repeat("generated\n", 1500)}, "ok", false)
		b.assistant(fmt.Sprintf("done %d", r))
	}
	b.openRunTail()
	return b
}

func TestGovernorBringsAStuckSessionUnderBudgetWithoutAModelCall(t *testing.T) {
	resetMeter(t)
	b := bigHistory(t, 40)
	reviews := 0
	model := funcModel{fn: func(context.Context, Request) (*Msg, error) {
		reviews++
		return nil, fmt.Errorf("no model in the critical path")
	}}
	var events []Event
	a := &Agent{session: b.s, model: model, settings: Settings{Pruner: PrunerSettings{Mode: PruneModerate}}.WithDefaults(),
		onEvent: func(_ context.Context, e Event) { events = append(events, e) }}
	before, _ := requestInputTokens(Request{Messages: b.s.ContextMessages(-1)}, "")
	if before <= HardContextLimit*3 {
		t.Fatalf("fixture too small: %d", before)
	}
	start := time.Now()
	if err := a.ensureContextBudget(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	after, _ := requestInputTokens(Request{Messages: b.s.ContextMessages(-1)}, "")
	if after > contextBudgetTarget || reviews != 0 || time.Since(start) > 5*time.Second {
		t.Fatalf("before %d after %d reviews %d in %s", before, after, reviews, time.Since(start))
	}
	pruned := false
	for _, e := range events {
		pruned = pruned || (e.Kind == KindPruneEnd && e.Prune.After < e.Prune.Before)
	}
	if !pruned {
		t.Fatal("no prune_end reported the reduction")
	}
	// Persisted: a reload sees the same folded view.
	reloaded := LoadOrCreateSessionAt(b.s.Path())
	if again, _ := requestInputTokens(Request{Messages: reloaded.ContextMessages(-1)}, ""); again != after {
		t.Fatalf("folding was not persisted: %d vs %d", again, after)
	}
}

func TestPolicyFoldsOnlyBeyondKeepRunsAndInBatches(t *testing.T) {
	resetMeter(t)
	b := bigHistory(t, 4)
	a := &Agent{session: b.s, settings: Settings{Pruner: PrunerSettings{Mode: PruneModerate}}}
	estimate := func() (int, error) { return requestInputTokens(Request{Messages: b.s.ContextMessages(-1)}, "") }
	a.foldByPolicy(context.Background(), estimate)
	folded := 0
	for _, r := range closedRuns(b.s) {
		if b.s.runFolded(r) {
			folded++
		}
	}
	if folded != 0 {
		t.Fatalf("one run beyond keep_runs folded %d; batching should wait for two", folded)
	}
	b.s.Messages = b.s.Messages[:len(b.s.Messages)-9] // drop the open run tail
	b2 := &runBuilder{s: b.s, n: 1000}
	b2.user("request 4")
	b2.call(toolRead, map[string]any{"path": "x.go"}, strings.Repeat("y", 3000), false)
	b2.assistant("done 4")
	b2.openRunTail()
	a.foldByPolicy(context.Background(), estimate)
	var states []bool
	for _, r := range closedRuns(b.s) {
		states = append(states, b.s.runFolded(r))
	}
	if fmt.Sprint(states) != "[true true false false false]" {
		t.Fatalf("fold states %v, want the two oldest folded and keep_runs=3 kept", states)
	}
}

func TestGovernorParksTheCurrentRunWhenItAloneIsOverBudget(t *testing.T) {
	resetMeter(t)
	b := newRunBuilder(t)
	b.user("one enormous task")
	for i := 0; i < 30; i++ {
		b.call(toolRead, map[string]any{"path": fmt.Sprintf("big%d.go", i)}, strings.Repeat("payload ", 2000), false)
	}
	for i := 0; i < 3; i++ {
		b.call(toolSearch, map[string]any{"pattern": fmt.Sprint(i)}, "none", false)
	}
	a := &Agent{session: b.s, settings: Settings{}.WithDefaults()}
	if err := a.ensureContextBudget(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if n, _ := requestInputTokens(Request{Messages: b.s.ContextMessages(-1)}, ""); n > HardContextLimit {
		t.Fatalf("still over budget: %d", n)
	}
	if b.s.Messages[len(b.s.Messages)-1].Parked {
		t.Fatal("the tail was parked")
	}
}
