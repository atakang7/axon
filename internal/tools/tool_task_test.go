package tools

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atakang7/axon/internal/session"
)

// newTestSession builds a real *session.Session backed by a temp file, so the
// task tool can be driven against the actual Plan implementation and not only
// the fake — RegisterTask/AdvanceTask/ReplanTask persisting correctly matters
// as much as the tool calling them correctly.
func newTestSession(t *testing.T) *session.Session {
	t.Helper()
	t.Setenv("AXON_SESSION_PATH", filepath.Join(t.TempDir(), "session.json"))
	return session.LoadOrCreateSession()
}

// A missing or unknown action has no sensible default — register, advance and
// replan take incompatible parameters — so it must error and name all three
// so the model can self-correct.
func TestTaskMissingOrUnknownActionErrors(t *testing.T) {
	plan := &fakePlan{}
	tool := TaskTool(plan)

	for _, action := range []string{"", "bogus"} {
		_, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{"action": action}))
		if err == nil {
			t.Fatalf("action=%q: expected error", action)
		}
		for _, a := range []string{"register", "advance", "replan"} {
			if !strings.Contains(err.Error(), a) {
				t.Fatalf("action=%q: error does not name %q: %v", action, a, err)
			}
		}
	}
}

// register with no goal, or with only blank steps, must fail rather than
// register a plan with nothing to work toward.
func TestTaskRegisterValidation(t *testing.T) {
	plan := &fakePlan{}
	tool := TaskTool(plan)

	_, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{
		"action": "register", "steps": []string{"do a thing"},
	}))
	if err == nil {
		t.Fatal("register without a goal should error")
	}

	_, err = tool.Fn(context.Background(), mustJSON(t, map[string]any{
		"action": "register", "goal": "ship the feature", "steps": []string{"  ", ""},
	}))
	if err == nil {
		t.Fatal("register with only blank steps should error")
	}
	if plan.registerCalls != 0 {
		t.Fatalf("RegisterTask called %d times on a validation failure, want 0", plan.registerCalls)
	}
}

// register must trim the goal, drop blank steps before storing or counting
// them, and report the trimmed goal, step count and first step back to the
// model so it knows what actually got registered.
func TestTaskRegisterTrimsAndReports(t *testing.T) {
	plan := &fakePlan{}
	tool := TaskTool(plan)

	out, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{
		"action": "register",
		"goal":   "  is the answer complete?  ",
		"steps":  []string{"  step one  ", "", "step two", "   "},
	}))
	if err != nil {
		t.Fatalf("Fn: %v", err)
	}
	if plan.registerCalls != 1 {
		t.Fatalf("RegisterTask called %d times, want 1", plan.registerCalls)
	}
	// The tool passes the raw goal straight to RegisterTask — trimming for
	// storage is Session's job — but it must trim before rendering the report
	// the model reads.
	if plan.lastGoal != "  is the answer complete?  " {
		t.Fatalf("lastGoal = %q, want the raw untrimmed goal passed through to RegisterTask", plan.lastGoal)
	}
	if len(plan.lastSteps) != 2 || plan.lastSteps[0].Description != "step one" || plan.lastSteps[1].Description != "step two" {
		t.Fatalf("lastSteps = %+v, want exactly [step one, step two] with blanks dropped", plan.lastSteps)
	}
	want := "task: is the answer complete? (2 steps; current: step one)"
	if out != want {
		t.Fatalf("out = %q, want %q", out, want)
	}
}

// More than 4 steps is a signal the goal was not scoped tightly enough, so
// register appends a warning above that line and omits it at or under it.
func TestTaskRegisterWarnsAboveFourSteps(t *testing.T) {
	plan := &fakePlan{}
	tool := TaskTool(plan)

	out, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{
		"action": "register", "goal": "g", "steps": []string{"1", "2", "3", "4"},
	}))
	if err != nil {
		t.Fatalf("Fn: %v", err)
	}
	if strings.Contains(out, "warning") {
		t.Fatalf("4 steps should not warn: %q", out)
	}

	out, err = tool.Fn(context.Background(), mustJSON(t, map[string]any{
		"action": "register", "goal": "g", "steps": []string{"1", "2", "3", "4", "5"},
	}))
	if err != nil {
		t.Fatalf("Fn: %v", err)
	}
	if !strings.Contains(out, "warning") || !strings.Contains(out, ">4 steps") {
		t.Fatalf("5 steps should warn about being under-scoped: %q", out)
	}
}

// A RegisterTask failure from the Plan must propagate to the caller rather
// than being swallowed and reported as success.
func TestTaskRegisterPropagatesPlanError(t *testing.T) {
	plan := &fakePlan{registerErr: errBoom}
	tool := TaskTool(plan)

	_, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{
		"action": "register", "goal": "g", "steps": []string{"1"},
	}))
	if err != errBoom {
		t.Fatalf("err = %v, want the Plan's own error surfaced", err)
	}
}

// advance must translate AdvanceTask's three distinct outcomes (a plain next
// step, the string "done", and an empty string meaning nothing left) into
// three distinctly worded results, and pass through a Plan error untouched.
func TestTaskAdvanceOutcomes(t *testing.T) {
	cases := []struct {
		name    string
		result  string
		err     error
		want    string
		wantErr bool
	}{
		{"mid-plan", "clean up the report", nil, "next → clean up the report", false},
		{"last step", "done", nil, "done — answer the user", false},
		{"past the end", "", nil, "all steps already complete", false},
		{"no task registered", "", errBoom, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan := &fakePlan{advanceResult: tc.result, advanceErr: tc.err}
			tool := TaskTool(plan)

			out, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{"action": "advance"}))
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error to propagate")
				}
				return
			}
			if err != nil {
				t.Fatalf("Fn: %v", err)
			}
			if out != tc.want {
				t.Fatalf("out = %q, want %q", out, tc.want)
			}
		})
	}
}

// replan with no steps has nothing to replace the plan with and must error;
// a blank goal must keep the existing goal rather than blanking it out.
func TestTaskReplanValidationAndBlankGoal(t *testing.T) {
	plan := &fakePlan{}
	tool := TaskTool(plan)

	_, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{"action": "replan", "steps": []string{}}))
	if err == nil {
		t.Fatal("replan with no steps should error")
	}

	out, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{
		"action": "replan", "goal": "  ", "steps": []string{"new step"},
	}))
	if err != nil {
		t.Fatalf("Fn: %v", err)
	}
	if plan.lastGoal != "  " {
		t.Fatalf("tool should pass the raw (blank) goal through; ReplanTask itself decides to keep the old one")
	}
	if !strings.Contains(out, "replanned: 1 steps; current: new step") {
		t.Fatalf("out = %q, want the replanned report", out)
	}
}

// replan warns above 4 steps too, mirroring register's under-scoping signal.
func TestTaskReplanWarnsAboveFourSteps(t *testing.T) {
	plan := &fakePlan{}
	tool := TaskTool(plan)

	out, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{
		"action": "replan", "goal": "g", "steps": []string{"1", "2", "3", "4", "5"},
	}))
	if err != nil {
		t.Fatalf("Fn: %v", err)
	}
	if !strings.Contains(out, "warning") {
		t.Fatalf("5-step replan should warn: %q", out)
	}
}

// replan before any register must propagate "no task registered" rather than
// silently creating one — replan is explicitly the "the current plan no
// longer fits" action, not a backdoor register.
func TestTaskReplanBeforeRegisterPropagatesError(t *testing.T) {
	sess := newTestSession(t)
	tool := TaskTool(sess)

	_, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{
		"action": "replan", "goal": "g", "steps": []string{"1"},
	}))
	if err == nil {
		t.Fatal("replan with no task registered should error")
	}
	if !strings.Contains(err.Error(), "no task registered") {
		t.Fatalf("error = %v, want it to name the missing registration", err)
	}
}

// The task tool never reads the plan back — Plan (RegisterTask, AdvanceTask,
// ReplanTask) exposes no getter at all, so this is enforced structurally by
// the interface, not just by convention. This test pins the write-only
// invariant at the call-count level: each action calls exactly the one Plan
// method it needs and nothing else, across a full register/advance/replan
// sequence against the fake.
func TestTaskWriteOnlyPlanInteraction(t *testing.T) {
	plan := &fakePlan{advanceResult: "next step"}
	tool := TaskTool(plan)

	tool.Fn(context.Background(), mustJSON(t, map[string]any{"action": "register", "goal": "g", "steps": []string{"1", "2"}}))
	tool.Fn(context.Background(), mustJSON(t, map[string]any{"action": "advance"}))
	tool.Fn(context.Background(), mustJSON(t, map[string]any{"action": "replan", "goal": "g2", "steps": []string{"3"}}))

	if plan.registerCalls != 1 || plan.advanceCalls != 1 || plan.replanCalls != 1 {
		t.Fatalf("calls = register:%d advance:%d replan:%d, want exactly one of each",
			plan.registerCalls, plan.advanceCalls, plan.replanCalls)
	}
}

// End-to-end against the real session.Session: register, advance, and replan
// must actually mutate CurrentTask the way the fake asserts they are called,
// closing the gap between "the tool calls the right method" and "the real
// implementation behind that method does the right thing."
func TestTaskAgainstRealSession(t *testing.T) {
	sess := newTestSession(t)
	tool := TaskTool(sess)

	out, err := tool.Fn(context.Background(), mustJSON(t, map[string]any{
		"action": "register", "goal": "is the release ready?", "steps": []string{"run tests", "check changelog"},
	}))
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if !strings.Contains(out, "run tests") {
		t.Fatalf("register report missing first step: %q", out)
	}
	if sess.CurrentTask == nil || sess.CurrentTask.Goal != "is the release ready?" {
		t.Fatalf("session task not registered as expected: %+v", sess.CurrentTask)
	}

	out, err = tool.Fn(context.Background(), mustJSON(t, map[string]any{"action": "advance"}))
	if err != nil {
		t.Fatalf("advance: %v", err)
	}
	if !strings.Contains(out, "check changelog") {
		t.Fatalf("advance did not report the next step: %q", out)
	}
	if sess.CurrentTask.CurrentStep != 1 || !sess.CurrentTask.Steps[0].Done {
		t.Fatalf("session did not advance as expected: %+v", sess.CurrentTask)
	}

	out, err = tool.Fn(context.Background(), mustJSON(t, map[string]any{"action": "advance"}))
	if err != nil {
		t.Fatalf("final advance: %v", err)
	}
	if out != "done — answer the user" {
		t.Fatalf("out = %q, want the done report on the last step", out)
	}
}

// errBoom is a stable sentinel error used wherever a test needs to assert
// that a Plan error propagates unchanged rather than being wrapped or lost.
var errBoom = &sentinelErr{"boom"}

type sentinelErr struct{ msg string }

func (e *sentinelErr) Error() string { return e.msg }
