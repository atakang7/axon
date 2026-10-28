package axon

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// numbered renders lines like the read tool does, from line first.
func numbered(first, n int, text string) string {
	var lines []string
	for i := 0; i < n; i++ {
		lines = append(lines, fmt.Sprintf("%d\t%s %d", first+i, text, first+i))
	}
	return strings.Join(lines, "\n")
}

// checkpointFixture is a run with three large results and a current round.
// m-ids: the read of a.go, the go test output, the read of b.go.
func checkpointFixture(t *testing.T) (*runBuilder, []string) {
	b := newRunBuilder(t)
	b.user("fix the login test")
	var ids []string
	b.call(toolRead, map[string]any{"path": "a.go"}, numbered(1, 200, "func a"), false)
	ids = append(ids, lastID(b.s))
	b.call(toolExec, map[string]any{"command": "go test ./..."}, "exit_code: 0\n"+strings.Repeat("ok  pkg\n", 300), false)
	ids = append(ids, lastID(b.s))
	b.call(toolRead, map[string]any{"path": "b.go"}, numbered(1, 150, "func b"), false)
	ids = append(ids, lastID(b.s))
	for i := 0; i < 4; i++ {
		b.call(toolSearch, map[string]any{"pattern": fmt.Sprint(i)}, "no match", false)
	}
	return b, ids
}

func lastID(s *Session) string { return s.Messages[len(s.Messages)-1].ID }

func checkpointJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func msgByID(s *Session, id string) Msg {
	for _, m := range s.Messages {
		if m.ID == id {
			return m
		}
	}
	return Msg{}
}

func TestCheckpointKeepsTrimsAndDropsWithNotes(t *testing.T) {
	b, ids := checkpointFixture(t)
	read, test, other := ids[0], ids[1], ids[2]
	cands := checkpointCandidates(b.s)
	if len(cands) != 3 {
		t.Fatalf("candidates %d, want the three large results", len(cands))
	}
	var args checkpointArgs
	_ = json.Unmarshal(checkpointJSON(t, map[string]any{
		"keep":  []map[string]any{{"id": read, "lines": "40-45, 100"}, {"id": "#" + other}},
		"notes": []map[string]any{{"id": test, "note": "all packages pass\nsecond line"}},
	}), &args)
	out := applyCheckpoint(b.s, args, cands)

	if fmt.Sprint(out.trimmed, out.kept, out.dropped) != fmt.Sprintf("[%s] [%s] [%s]", read, other, test) {
		t.Fatalf("trimmed %v kept %v dropped %v", out.trimmed, out.kept, out.dropped)
	}
	view := viewByID(b.s.contextView(-1))
	trimmed := view[read].Content
	if !strings.Contains(trimmed, "40\tfunc a 40") || !strings.Contains(trimmed, "100\tfunc a 100") || strings.Contains(trimmed, "46\tfunc a 46") {
		t.Fatalf("trim did not keep exactly the file lines asked for:\n%s", trimmed)
	}
	if !strings.Contains(trimmed, "7 of 200 lines kept") {
		t.Fatalf("trim header: %s", firstLine(trimmed))
	}
	if got := view[test].Content; !strings.Contains(got, "#"+test+" ran go test ./... → exit 0") || !strings.Contains(got, "all packages pass second line") || strings.Contains(got, "ok  pkg") {
		t.Fatalf("a dropped result is not one line with its call and note: %s", got)
	}
	if view[other].Content != msgByID(b.s, other).Content {
		t.Fatal("a kept block changed")
	}
	// The log is untouched: recall still returns the original.
	if !strings.Contains(msgByID(b.s, test).Content, "ok  pkg") {
		t.Fatal("the log lost the dropped block's content")
	}
}

func TestCheckpointValidatesEveryEntryAndNeverAppliesAnInvalidOne(t *testing.T) {
	b, ids := checkpointFixture(t)
	read := ids[0]
	userID := b.s.Messages[1].ID
	cands := checkpointCandidates(b.s)
	var args checkpointArgs
	_ = json.Unmarshal(checkpointJSON(t, map[string]any{
		"keep": []map[string]any{
			{"id": "m999"},
			{"id": userID},
			{"id": read, "lines": "900-950"},
		},
		"notes": []map[string]any{{"id": "m998", "note": "x"}, {"id": ids[1], "note": "   "}},
	}), &args)
	out := applyCheckpoint(b.s, args, cands)

	if len(out.ignored) != 5 {
		t.Fatalf("ignored %d entries, want 5: %v", len(out.ignored), out.ignored)
	}
	if !contains(out.kept, read) || b.s.Trims[read] != nil {
		t.Fatal("a keep with lines outside the block must keep it whole, not trim it")
	}
	if msgByID(b.s, userID).Parked {
		t.Fatal("a user message was dropped")
	}
	// Results not kept are dropped with the call's gist, never silently lost.
	if got := msgByID(b.s, ids[1]); !got.Parked || !strings.Contains(got.ParkSummary, "ran go test ./... → exit 0") {
		t.Fatalf("dropped without a gist: %+v", got.ParkSummary)
	}
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func TestCheckpointCandidatesProtectTheNewestRoundFailuresAndSmallResults(t *testing.T) {
	b := newRunBuilder(t)
	b.user("task")
	b.call(toolExec, map[string]any{"command": "make"}, "exit_code: 2\n"+strings.Repeat("error\n", 200), false)
	failure := lastID(b.s)
	b.call(toolRead, map[string]any{"path": "small.go"}, "tiny", false)
	small := lastID(b.s)
	for i := 0; i < 3; i++ {
		b.call(toolSearch, map[string]any{"pattern": fmt.Sprint(i)}, "no match", false)
	}
	// The newest round: two parallel reads the model has not seen yet.
	var calls []ToolCall
	for i, p := range []string{"x.go", "y.go"} {
		tc := ToolCall{ID: fmt.Sprintf("p%d", i)}
		tc.Function.Name, tc.Function.Arguments = toolRead, fmt.Sprintf(`{"path":%q}`, p)
		calls = append(calls, tc)
	}
	b.s.Append(Msg{Role: "assistant", ToolCalls: calls})
	for i := range calls {
		b.s.Append(Msg{Role: "tool", ToolCallID: calls[i].ID, ToolName: toolRead, Content: numbered(1, 300, "new")})
	}
	newest := lastID(b.s)

	for _, m := range checkpointCandidates(b.s) {
		switch m.ID {
		case failure:
			t.Fatal("an unresolved failure is a candidate")
		case small:
			t.Fatal("a result too small to matter is a candidate")
		case newest:
			t.Fatal("the newest round, not yet seen by the model, is a candidate")
		}
	}
}

func TestSelectLinesUsesFileNumbersAndSectionPaths(t *testing.T) {
	multi := strings.Join([]string{
		"=== a.go ===", numbered(1, 5, "a"),
		"",
		"=== pkg/b.go (from line 10) ===", numbered(10, 5, "b"),
	}, "\n")
	lines := strings.Split(multi, "\n")
	sel, err := selectLines(lines, "11-12", "b.go")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, sp := range sel {
		got = append(got, lines[sp[0]-1:sp[1]]...)
	}
	want := []string{"=== pkg/b.go (from line 10) ===", "11\tb 11", "12\tb 12"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	if _, err := selectLines(lines, "3", "missing.go"); err == nil {
		t.Fatal("a path that is not in the read was accepted")
	}

	plain := strings.Split("one\ntwo\nthree\nfour", "\n")
	if sel, err := selectLines(plain, "2-3", ""); err != nil || fmt.Sprint(sel) != "[[2 3]]" {
		t.Fatalf("unnumbered result: %v %v", sel, err)
	}
	for _, bad := range []string{"", "a-b", "5-2", "0", "9"} {
		if _, err := selectLines(plain, bad, ""); err == nil {
			t.Fatalf("spec %q accepted", bad)
		}
	}
}

// checkpointAgent is an agent over the fixture whose model is scripted: the
// first request is the forced checkpoint, the second the ordinary request.
func checkpointAgent(t *testing.T, b *runBuilder, model Model) *Agent {
	t.Helper()
	a := &Agent{session: b.s, model: model, settings: Settings{Pruner: PrunerSettings{TriggerTokens: 2000}}.WithDefaults()}
	a.tools = []Tool{a.checkpointTool(), ReadTool(b.s, a.settings.Tools.limits())}
	return a
}

func TestTheAgentCanCheckpointOnItsOwn(t *testing.T) {
	resetMeter(t)
	b, ids := checkpointFixture(t)
	a := checkpointAgent(t, b, funcModel{fn: func(context.Context, Request) (*Msg, error) { return finalMsg("done"), nil }})
	var events []Event
	a.onEvent = func(_ context.Context, e Event) { events = append(events, e) }
	out, err := a.checkpointTool().Fn(context.Background(), checkpointJSON(t, map[string]any{"keep": []map[string]any{{"id": ids[0], "lines": "1-10"}}}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Checkpoint applied") || !strings.Contains(out, "trimmed 1, dropped 2") {
		t.Fatalf("report: %s", out)
	}
	if len(events) != 2 || events[0].Kind != KindPruneStart || events[1].Kind != KindPruneEnd {
		t.Fatalf("events %v", events)
	}
	if out, _ := a.checkpointTool().Fn(context.Background(), json.RawMessage(`{}`)); !strings.Contains(out, "Checkpoint applied") && !strings.Contains(out, "Nothing to decide") {
		t.Fatalf("second checkpoint: %s", out)
	}
}

func TestDefaultToolsAreTheStandardSetAndExcludable(t *testing.T) {
	cfg := baseConfig(t)
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	var names []string
	for _, tool := range a.tools {
		names = append(names, tool.Name)
	}
	if got := strings.Join(names, " "); got != "read_file edit write_file bash bash_output kill_shell grep glob todo_write" {
		t.Fatalf("default tools: %s", got)
	}
	cfg = baseConfig(t)
	cfg.ExcludeBuiltins = []string{toolTask}
	b, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if hasTool(b.tools, toolTask) {
		t.Fatal("todo_write could not be excluded")
	}
}

// The deciding model may be a weaker one; the shapes models were seen to send
// must all read as intended, and the live run that failed on "notes": ["..."]
// must not happen again.
func TestCheckpointArgumentsAcceptTheShapesModelsSend(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{`{"keep":[{"id":"m12","lines":"40-85"}],"notes":[{"id":"m14","note":"tests pass"}]}`, `[{m12 40-85 }] [{m14 tests pass}]`},
		{`{"keep":["m12","#m13: 40-85, 90"],"notes":["m14: tests pass","m15 - build ok"]}`, `[{m12  } {m13 40-85, 90 }] [{m14 tests pass} {m15 build ok}]`},
		{`{"keep":{"id":"m12"},"notes":{"m15":"b","m14":"a"}}`, `[{m12  }] [{m14 a} {m15 b}]`},
		{`{}`, `[] []`},
	} {
		var a checkpointArgs
		if err := json.Unmarshal([]byte(tc.raw), &a); err != nil {
			t.Fatalf("%s: %v", tc.raw, err)
		}
		if got := fmt.Sprint(a.Keep, a.Notes); got != tc.want {
			t.Fatalf("%s:\n got %s\nwant %s", tc.raw, got, tc.want)
		}
	}
	var a checkpointArgs
	if err := json.Unmarshal([]byte(`{"keep":["no id here"]}`), &a); err == nil {
		t.Fatal("a keep entry without a block id was accepted")
	}
}
