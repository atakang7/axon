package agent

import (
	"strings"
	"testing"

	"github.com/atakang7/axon/internal/session"
)

// The prompt asks for {"park":[...]} and the parser must read that same key.
// The previous version asked the model for a "park" array in one place and a
// "forget" array in another while parsing only "forget", so anything the model
// put under "park" was silently discarded and pruning quietly did nothing.
func TestParkListReadsTheKeyThePromptAsksFor(t *testing.T) {
	if !strings.Contains(prunerSystemPrompt, `{"park":`) {
		t.Fatal("system prompt no longer asks for a park array")
	}

	for _, tc := range []struct {
		name  string
		reply string
		want  []int
	}{
		{"bare object", `{"park":[3,7,9]}`, []int{3, 7, 9}},
		{"empty means keep everything", `{"park":[]}`, nil},
		{"tolerates surrounding prose", "Thinking...\n{\"park\":[4]}\nDone.", []int{4}},
		{"tolerates a trailing newline", "{\"park\":[1,2]}\n", []int{1, 2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parkList(tc.reply)
			if err != nil {
				t.Fatalf("parkList(%q): %v", tc.reply, err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got %v, want %v", got, tc.want)
				}
			}
		})
	}
}

// A model that answers with prose, or with malformed JSON, must produce an
// error the loop can skip on — never a silent no-op that looks like success.
func TestParkListRejectsUnusableReplies(t *testing.T) {
	for _, reply := range []string{
		"I think we should keep everything.",
		"",
		`{"park":`,
		`{"park":"all of them"}`,
	} {
		if _, err := parkList(reply); err == nil {
			t.Fatalf("parkList(%q) returned no error", reply)
		}
	}
}

// The pruner must not fire on a small context: below the floor, a curator call
// costs more than the tokens it saves.
func TestShouldFireRespectsFloorAndGrowth(t *testing.T) {
	p := &Pruner{}
	s := &session.Session{}

	if p.ShouldFire(s) {
		t.Fatal("fired on an empty session")
	}

	// Cross the floor.
	s.Messages = []Msg{{Role: "user", Content: strings.Repeat("x", pruneFloor*4+8)}}
	if !p.ShouldFire(s) {
		t.Fatal("did not fire on first crossing of the floor")
	}

	// Having fired, small growth must not trigger another pass.
	p.lastFire = p.ContextTokens(s)
	s.Messages = append(s.Messages, Msg{Role: "user", Content: strings.Repeat("x", 400)})
	if p.ShouldFire(s) {
		t.Fatal("re-fired on growth below the threshold")
	}

	// Sharp growth must.
	s.Messages = append(s.Messages, Msg{Role: "user", Content: strings.Repeat("x", pruneGrowth*4+8)})
	if !p.ShouldFire(s) {
		t.Fatal("did not fire after growth past the threshold")
	}
}

// A nil Pruner is the documented way to disable pruning, so every method must
// tolerate it — the loop has no special case.
func TestNilPrunerIsInert(t *testing.T) {
	var p *Pruner
	if p.ShouldFire(&session.Session{}) {
		t.Fatal("nil pruner reported that it should fire")
	}
	if NewPruner(nil) != nil {
		t.Fatal("NewPruner(nil) should stay nil so callers need no special case")
	}
}
