package axon

import (
	"context"
	"strings"
	"testing"
)

// The reply a free model gave every turn on 2026-10-26 once several folded
// runs were in its context as assistant turns: a copied card, no tool call.
const echoedCard = "[finished run #m241..#m242, folded; recall any id for the original text]\nread: fker/extension-fker.js (slice 80-90)\nran: git -C /c/Users/atakan/projects/vscode-cortex diff --stat"

func TestACopiedCardIsAskedAgainOnceInsteadOfEndingTheTurn(t *testing.T) {
	model := newQueueModel(
		scriptedReply{msg: finalMsg(echoedCard)},
		scriptedReply{msg: finalMsg("Done: the diff is clean.")},
	)
	cfg := noToolsConfig(t)
	cfg.Model = model
	var events []Event
	cfg.OnEvent = func(_ context.Context, e Event) { events = append(events, e) }
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	res, err := a.Step(context.Background(), "check the diff")
	if err != nil {
		t.Fatal(err)
	}
	if res.Assistant != "Done: the diff is clean." || len(model.requests) != 2 {
		t.Fatalf("answer %q after %d requests", res.Assistant, len(model.requests))
	}
	last := model.requests[1].Messages
	if note := last[len(last)-1]; note.Role != "user" || !strings.Contains(note.Content, "copied a runtime summary") {
		t.Fatalf("the second request carries no note: %+v", note)
	}
	for _, e := range events {
		if e.Kind == KindAssistantEnd && looksLikeCardEcho(e.Text) {
			t.Fatal("the copied card was reported as an answer")
		}
	}

	// A second copy ends the turn as it is: no loop.
	model = newQueueModel(scriptedReply{msg: finalMsg(echoedCard)}, scriptedReply{msg: finalMsg(echoedCard)})
	a.SetModel(model)
	if _, err := a.Step(context.Background(), "again"); err != nil || len(model.requests) != 2 {
		t.Fatalf("err %v after %d requests", err, len(model.requests))
	}
}

func TestFoldedRunsNeverOfferACopiedCardAsTheAnswer(t *testing.T) {
	b := newRunBuilder(t)
	b.user("check the diff")
	b.call(toolExec, map[string]any{"command": "git diff --stat"}, "exit_code: 0\n"+strings.Repeat("x", 2000), false)
	b.assistant("The diff is clean.")
	b.assistant(echoedCard)
	b.openRunTail()
	b.assistant(echoedCard) // recorded in the open run, which does not fold
	b.s.foldRun(closedRuns(b.s)[0])
	for _, m := range b.s.ContextMessages(-1) {
		if m.Role == "assistant" && looksLikeCardEcho(m.Content) {
			t.Fatalf("a card reached the context as an assistant turn: %.80s", m.Content)
		}
	}
	r := closedRuns(b.s)[0]
	w := summarizeWork(b.s, r.start+1, r.end, toolUses(b.s))
	if w.answer != "The diff is clean." || !strings.Contains(historyNote(b.s, []run{r}, toolUses(b.s)), "ran git diff --stat") {
		t.Fatalf("answer %q history %q", w.answer, historyNote(b.s, []run{r}, toolUses(b.s)))
	}
}
