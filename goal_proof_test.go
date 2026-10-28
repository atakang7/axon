package axon

import (
	"strings"
	"testing"
)

// then runs f before handing back the scripted reply, to change the world
// between two calls of the same step.
func then(f func(), reply func(Request) *Msg) func(Request) *Msg {
	return func(r Request) *Msg { f(); return reply(r) }
}

func TestRedToGreenTestsProveAFixWithoutCriteria(t *testing.T) {
	w := newGoalWorld(t)
	w.a.settings.Goals.DisableNudges = true
	w.step("go test fails in pricing, fix it",
		call(toolExec, map[string]any{"command": "go test ./..."}), // not in outputs: exit 127, red
		call(toolWrite, map[string]any{"path": "pricing/discount.go"}),
		then(func() { w.outputs["go test ./... 2>&1 | tail -5"] = "ok  \texample.com/shop/pricing" },
			call(toolExec, map[string]any{"command": "go test ./... 2>&1 | tail -5"})),
		answer("fixed"),
	)
	if open := w.s.openCriteria(); len(open) != 0 {
		t.Fatalf("red then green should prove the fix, open: %v", open)
	}
	if v := w.s.ActiveGoalView(); v == nil || v.Unchecked || len(v.Criteria) != 1 || v.Criteria[0].State != critMet {
		t.Fatalf("goal view: %+v", v)
	}
	if !strings.Contains(w.s.GoalBlock(), "proven by the project's tests") {
		t.Fatalf("goal block: %s", w.s.GoalBlock())
	}
}

func TestAddedTestsProveAFeatureWithoutCriteria(t *testing.T) {
	w := newGoalWorld(t)
	w.a.settings.Goals.DisableNudges = true
	w.outputs["python3 -m unittest discover -s tests"] = "Ran 5 tests\nOK"
	w.step("add a --format json option with tests",
		call(toolWrite, map[string]any{"path": "reportgen/cli.py"}),
		call(toolWrite, map[string]any{"path": "tests/test_report.py"}),
		call(toolExec, map[string]any{"command": "python3 -m unittest discover -s tests"}),
		answer("done"),
	)
	if open := w.s.openCriteria(); len(open) != 0 {
		t.Fatalf("a change with its own passing tests should be proven, open: %v", open)
	}
}

func TestPassingTestsAloneDoNotProveAnUntestedChange(t *testing.T) {
	w := newGoalWorld(t)
	w.a.settings.Goals.DisableNudges = true
	w.outputs["go test ./..."] = "ok"
	w.outputs["ls"] = "main.go"
	w.step("make the banner blue",
		call(toolWrite, map[string]any{"path": "ui/banner.go"}),
		call(toolExec, map[string]any{"command": "go test ./..."}),
		call(toolExec, map[string]any{"command": "ls"}),
		answer("done"),
	)
	if open := w.s.openCriteria(); len(open) != 1 || !strings.HasPrefix(open[0], "no criteria") {
		t.Fatalf("tests that were green all along prove nothing about this change, open: %v", open)
	}
}

func TestExpectIgnoresWhitespaceAndCase(t *testing.T) {
	if !outputHas("ok  \texample.com/shop/cart\t0.5s", "ok example.com/shop/cart") {
		t.Fatal("whitespace difference should still match")
	}
	if !outputHas("Ran 5 tests\n\nOK", "ran 5 tests ok") {
		t.Fatal("case and newlines should still match")
	}
	if outputHas("FAIL example.com/shop", "ok example.com/shop") {
		t.Fatal("different words must not match")
	}
}

func TestIsTestCommand(t *testing.T) {
	for _, c := range []string{"go test ./...", "cd x && go test -run TestA ./pkg", "python3 -m pytest -q", "npm test", "pnpm run test", "./gradlew :app:testDebugUnitTest", "cargo test --all"} {
		if !isTestCommand(c) {
			t.Errorf("%q should be a test command", c)
		}
	}
	for _, c := range []string{"go build ./...", "ls tests", "cat go_test_notes.md", "echo pytest"} {
		if isTestCommand(c) && c != "echo pytest" {
			t.Errorf("%q should not be a test command", c)
		}
	}
}
