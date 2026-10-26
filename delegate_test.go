package axon

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func locatingAgent(t *testing.T, explorer Model) (*Agent, *queueModel) {
	t.Helper()
	var replies []scriptedReply
	for i := 0; i < 5; i++ {
		replies = append(replies, scriptedReply{msg: toolCallMsg("c"+string(rune('0'+i)), toolRead, `{"path":"x.go"}`)})
	}
	replies = append(replies, scriptedReply{msg: finalMsg("done")})
	model := newQueueModel(replies...)
	cfg := noToolsConfig(t)
	cfg.Model = model
	cfg.Explorer = explorer
	cfg.Tools = []Tool{fnTool(toolRead, func(context.Context, json.RawMessage) (string, error) { return "package x", nil })}
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	return a, model
}

func TestTheDecidingModelIsToldOnceToDelegateLocating(t *testing.T) {
	a, model := locatingAgent(t, &scriptedExplorer{})
	if _, err := a.Step(context.Background(), "where is it?"); err != nil {
		t.Fatal(err)
	}
	notes := 0
	for i, r := range model.requests {
		for _, m := range r.Messages {
			if m.Role == "tool" && strings.Contains(m.Content, "Finding where things are is the explorer's work") {
				notes++
				if i != 3 {
					t.Fatalf("the note came with request %d, want the one after three locating replies", i)
				}
			}
		}
	}
	if notes != 1 {
		t.Fatalf("%d notes, want exactly one per run", notes)
	}
	// Without an explorer there is nobody to delegate to.
	b, model := locatingAgent(t, nil)
	if _, err := b.Step(context.Background(), "where is it?"); err != nil {
		t.Fatal(err)
	}
	for _, r := range model.requests {
		for _, m := range r.Messages {
			if strings.Contains(m.Content, "explorer's work") {
				t.Fatal("told to delegate without an explorer")
			}
		}
	}
}
