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
