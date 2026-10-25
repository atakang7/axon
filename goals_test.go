package axon

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// goalWorld is an agent with fake exec/write tools and the real goal tool,
// driven by a scripted model. outputs maps a command to what it prints.
type goalWorld struct {
	t        *testing.T
	a        *Agent
	s        *Session
	outputs  map[string]string
	script   []func(r Request) *Msg
	requests []Request
}

func newGoalWorld(t *testing.T) *goalWorld {
	w := &goalWorld{t: t, outputs: map[string]string{}}
	w.s = newTestSession(t)
	w.s.Append(Msg{Role: "system", Content: "system prompt"})
	exec := Tool{Name: toolExec, Description: "x", Schema: obj("object", props{"command": strSchema("c")}, []string{"command"}),
		Fn: func(_ context.Context, raw json.RawMessage) (string, error) {
			var a struct{ Command string }
			json.Unmarshal(raw, &a)
			out, ok := w.outputs[a.Command]
			code := 0
			if !ok {
				out, code = "not found", 127
			}
			return fmt.Sprintf("$ %s\nexit_code: %d\n%s", a.Command, code, out), nil
		}}
	write := Tool{Name: toolWrite, Description: "x", Schema: obj("object", props{"path": strSchema("p")}, []string{"path"}),
		Fn: func(context.Context, json.RawMessage) (string, error) { return "wrote", nil }}
	w.a = &Agent{session: w.s, tools: []Tool{exec, write, GoalTool(w.s)}, settings: DefaultSettings(),
		model: funcModel{fn: func(_ context.Context, r Request) (*Msg, error) {
			w.requests = append(w.requests, r)
			if len(w.script) == 0 {
				t.Fatalf("model called with no script left; last message: %+v", r.Messages[len(r.Messages)-1])
			}
			next := w.script[0]
			w.script = w.script[1:]
			return next(r), nil
		}}}
	return w
}

var callSeq int

func call(name string, args any) func(Request) *Msg {
	raw, _ := json.Marshal(args)
	return func(Request) *Msg {
		callSeq++
		tc := ToolCall{ID: fmt.Sprintf("call-%s-%d", name, callSeq), Type: "function"}
		tc.Function.Name, tc.Function.Arguments = name, string(raw)
		return &Msg{Role: "assistant", ToolCalls: []ToolCall{tc}}
	}
}

func answer(text string) func(Request) *Msg {
	return func(Request) *Msg { return &Msg{Role: "assistant", Content: text} }
}

func (w *goalWorld) step(input string, script ...func(Request) *Msg) {
	w.t.Helper()
	w.script = script
	w.requests = nil
	if _, err := w.a.Step(context.Background(), input); err != nil {
		w.t.Fatal(err)
	}
	if len(w.script) != 0 {
		w.t.Fatalf("%d scripted replies unused", len(w.script))
	}
}

func (w *goalWorld) noted(i int) bool {
	msgs := w.requests[i].Messages
	last := msgs[len(msgs)-1]
	return last.Role == "user" && strings.Contains(last.Content, "goal is not proven")
}

func (w *goalWorld) status(goal, id string) critStatus {
	for _, c := range w.s.Goals[goal].Criteria {
		if c.ID == id {
			return w.s.criterionStatus(c, toolUses(w.s))
		}
	}
	w.t.Fatalf("no criterion %s on %s", id, goal)
	return critStatus{}
}

func TestGoldenAPKIsNotDoneUntilTheOracleAgrees(t *testing.T) {
	w := newGoalWorld(t)
	w.outputs["stat -c %s app.apk"] = "14334212"
	w.outputs["ssh phone stat -c %s app.apk"] = "0"
	w.step("apk'yı telefona gönder",
		call(toolExec, map[string]string{"command": "ssh phone stat -c %s app.apk"}),
		call(toolGoal, map[string]any{"action": "criteria", "criteria": []map[string]string{{
			"text": "phone apk has the local size", "oracle": "ssh phone stat -c %s app.apk", "expect": "14334212",
			"baseline": "m2", "baseline_quote": "0", "source": "user"}}}),
		answer("Gönderdim."), // claims done while K1 is failed
		call(toolExec, map[string]string{"command": "ssh phone stat -c %s app.apk"}), // still 0
		answer("Tamam, bitti."),                         // ends again with K1 failed: second, last note
		answer("K1 failed: phone shows 0 bytes (#m4)."), // honest ending: no third note
	)
	goal := w.s.activeGoal()
	last := func(i int) string { m := w.requests[i].Messages; return m[len(m)-1].Content }
	if !w.noted(3) || !strings.Contains(last(5), "still not proven") || len(w.requests) != 6 {
		t.Fatalf("want a note after the first ending and a final one after the second; %d requests", len(w.requests))
	}
	if st := w.status(goal, "K1"); st.state != critFailed {
		t.Fatalf("K1 %s, want failed", st.state)
	}
	if b := w.s.GoalBlock(); !strings.Contains(b, "K1 [failed #") || !strings.Contains(b, `baseline #m2 "0"`) {
		t.Fatalf("goal block:\n%s", b)
	}

	// The user repeats; the agent continues the same goal and fixes it.
	w.outputs["ssh phone stat -c %s app.apk"] = "14334212"
	first := goal
	w.step("apk yok",
		call(toolGoal, map[string]string{"action": "continue", "goal": first}),
		call(toolExec, map[string]string{"command": "ssh phone stat -c %s app.apk"}),
		answer("Telefonda 14334212 bayt."),
	)
	if w.s.activeGoal() != first {
		t.Fatalf("active goal %s, want the continued %s", w.s.activeGoal(), first)
	}
	if st := w.status(first, "K1"); st.state != critMet {
		t.Fatalf("K1 %s, want met", st.state)
	}
	if !strings.Contains(w.s.GoalBlock(), "(continued by #") {
		t.Fatal("goal block does not show the continuation")
	}
}

func TestGoldenBugFixGoesStaleAfterALaterWrite(t *testing.T) {
	w := newGoalWorld(t)
	w.outputs["go test ./..."] = "ok  	pkg	0.1s"
	w.step("fix TestX",
		call(toolGoal, map[string]any{"action": "criteria", "criteria": []map[string]string{{
			"text": "tests pass", "oracle": "go test ./...", "expect": "ok", "source": "found"}}}),
		call(toolWrite, map[string]string{"path": "x.go"}),
		call(toolExec, map[string]string{"command": "go test ./..."}),
		call(toolWrite, map[string]string{"path": "y.go"}), // a change after the proof
		answer("Fixed."),
		call(toolExec, map[string]string{"command": "go test ./..."}),
		answer("Fixed and re-verified."),
	)
	if !w.noted(5) {
		t.Fatal("stale proof did not trigger the note")
	}
	if st := w.status(w.s.activeGoal(), "K1"); st.state != critMet {
		t.Fatalf("K1 %s after re-running the oracle", st.state)
	}
}

func TestGoldenQuestionHasNoCeremony(t *testing.T) {
	w := newGoalWorld(t)
	w.step("what does Foo do?", answer("Foo parses the header."))
	if len(w.requests) != 1 || w.s.GoalBlock() != "" {
		t.Fatalf("requests %d, block %q", len(w.requests), w.s.GoalBlock())
	}
}

func TestGoldenUnverifiableIsReportedNotPassed(t *testing.T) {
	w := newGoalWorld(t)
	w.step("make the microphone work",
		call(toolGoal, map[string]any{"action": "criteria", "criteria": []map[string]string{{"text": "user hears speech recognized on the phone"}}}),
		call(toolGoal, map[string]string{"action": "unverifiable", "criterion": "K1", "reason": "needs a human speaking into the phone"}),
		answer("I could not verify K1 myself: it needs you to speak into the phone."),
	)
	if len(w.requests) != 3 {
		t.Fatalf("an unverifiable criterion should not trigger the note: %d requests", len(w.requests))
	}
	if b := w.s.GoalBlock(); !strings.Contains(b, "[unverifiable]") || !strings.Contains(b, "cannot verify: needs a human") {
		t.Fatalf("goal block:\n%s", b)
	}
}

func TestGoldenFakeEvidenceAndFakeReferencesAreRejected(t *testing.T) {
	w := newGoalWorld(t)
	w.outputs["curl localhost"] = "<html>blank</html>"
	w.step("show the new button",
		call(toolExec, map[string]string{"command": "curl localhost"}),
		call(toolGoal, map[string]any{"action": "criteria",
			"criteria":    []map[string]string{{"text": "page shows the button"}, {"text": "x", "baseline": "m2", "baseline_quote": "<button>"}},
			"constraints": []string{"keep the old layout"}}),
		call(toolGoal, map[string]string{"action": "check", "criterion": "K1", "evidence": "m2", "quote": "<button>Save</button>"}),
		answer("Not verified: the page renders blank."),
		answer("Not verified: the page renders blank."),
	)
	g := w.s.Goals[w.s.activeGoal()]
	if len(g.Criteria) != 1 || len(g.Constraints) != 0 {
		t.Fatalf("criteria %d constraints %d; the bad baseline quote and the invented constraint must be rejected", len(g.Criteria), len(g.Constraints))
	}
	if st := w.status(w.s.activeGoal(), "K1"); st.state != critOpen {
		t.Fatalf("K1 %s; a quote that is not in the block proves nothing", st.state)
	}
}

func TestGoldenEditsNeedACheckSomeoneChose(t *testing.T) {
	w := newGoalWorld(t)
	w.outputs["go build ./..."] = ""
	w.outputs["ls"] = "main.go"
	w.step("rename the flag",
		call(toolWrite, map[string]string{"path": "main.go"}),
		call(toolExec, map[string]string{"command": "ls"}), // passes, proves nothing
		answer("Renamed."),
		call(toolGoal, map[string]any{"action": "criteria", "criteria": []map[string]string{{
			"text": "the program still builds", "oracle": "go build ./...", "source": "default"}}}),
		call(toolExec, map[string]string{"command": "go build ./..."}),
		answer("Renamed; build passes."),
	)
	if !w.noted(3) {
		t.Fatal("edits with no recorded criteria did not trigger the note, although an unrelated command passed")
	}
	if open := w.s.openCriteria(); len(open) != 0 {
		t.Fatalf("still open after the chosen oracle passed: %v", open)
	}
}

func TestGoalNotesCanBeDisabled(t *testing.T) {
	w := newGoalWorld(t)
	w.a.settings.Goals.DisableNudges = true
	w.step("rename the flag", call(toolWrite, map[string]string{"path": "main.go"}), answer("Renamed."))
	if len(w.requests) != 2 {
		t.Fatalf("requests %d", len(w.requests))
	}
}

// Mid-run notes interrupted work without changing what agents did (they
// recorded criteria at the end anyway); the contract is checked at turn end.
func TestEditsAreNotInterruptedMidRun(t *testing.T) {
	w := newGoalWorld(t)
	w.outputs["go build ./..."] = ""
	w.step("rename the flag",
		call(toolWrite, map[string]string{"path": "main.go"}),
		call(toolGoal, map[string]any{"action": "criteria", "criteria": []map[string]string{{
			"text": "still builds", "oracle": "go build ./..."}}}),
		call(toolExec, map[string]string{"command": "go build ./..."}),
		answer("Renamed; build passes."),
	)
	for i, r := range w.requests {
		for _, m := range r.Messages {
			if strings.Contains(m.Content, "[runtime note") {
				t.Fatalf("request %d carried a runtime note: %q", i, m.Content)
			}
		}
	}
	if len(w.requests) != 4 {
		t.Fatalf("requests %d", len(w.requests))
	}
}

func TestNudgeSurvivesATrailingGoalBlock(t *testing.T) {
	msgs := []Msg{{Role: "user", Content: "q"}, {Role: "tool", Content: "out"}, {Role: "system", Content: "[goal]"}}
	got := withNudge(msgs, " NOTE")
	if got[1].Content != "out NOTE" || msgs[1].Content != "out" {
		t.Fatalf("nudge lost or session mutated: %q / %q", got[1].Content, msgs[1].Content)
	}
}

func TestBaselineQuotesMayOmitTheReadGutter(t *testing.T) {
	content := "[#m2]\n1\tdef add(a, b):\n2\t    return a - b\n3\t"
	if _, ok := groundBlockQuote(content, "def add(a, b):\n    return a - b"); !ok {
		t.Fatal("a multi-line quote without line numbers was rejected")
	}
	if _, ok := groundBlockQuote(content, "return a + b"); ok {
		t.Fatal("a quote that is not in the block was accepted")
	}
}

func TestActiveGoalViewForTheEditor(t *testing.T) {
	w := newGoalWorld(t)
	w.outputs["go test ./..."] = "ok"
	w.step("fix it",
		call(toolGoal, map[string]any{"action": "criteria", "criteria": []map[string]string{{"text": "tests pass", "oracle": "go test ./...", "expect": "ok"}}}),
		call(toolExec, map[string]string{"command": "go test ./..."}),
		answer("Fixed."),
	)
	v := w.s.ActiveGoalView()
	if v == nil || len(v.Criteria) != 1 || v.Criteria[0].State != critMet || v.Criteria[0].Evidence == "" || v.Intent != "fix it" {
		t.Fatalf("view %+v", v)
	}
	q := newGoalWorld(t)
	q.step("what is this?", answer("A parser."))
	if q.s.ActiveGoalView() != nil {
		t.Fatal("a question should not show a goal panel")
	}
}

func TestOracleMatchesTheCommandOrTheCommandWithProse(t *testing.T) {
	for _, tc := range []struct {
		command, oracle string
		want            bool
	}{
		{"go test ./...", "go test ./...", true},
		{"go test ./... -v", "go test ./...", true},
		{"cd repo && go test ./...", "go test ./...", true},
		{"go test ./...", "go test ./... run in repo root", true},
		{"go test ./...", "go test ./... -run TestTotal", false},
		{"go test ./...", "go test ./... && go vet ./...", false},
		{"go test ./...", "go test ./... in pkg/shop", false},
		{"go vet ./...", "go test ./...", false},
		{"", "go test ./...", false},
		{"go test ./...", "", false},
	} {
		if got := oracleMatches(tc.command, tc.oracle); got != tc.want {
			t.Errorf("oracleMatches(%q, %q) = %v, want %v", tc.command, tc.oracle, got, tc.want)
		}
	}
}
