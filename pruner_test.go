package axon

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// newPrunerSession builds a *Session backed by a real temp file, isolated
// from the developer's own session — mirrors baseConfig's isolation but for
// tests that drive Pruner directly instead of through an Agent.
func newPrunerSession(t *testing.T) *Session {
	t.Helper()
	t.Setenv("AXON_SESSION_PATH", filepath.Join(t.TempDir(), "session.json"))
	return LoadOrCreateSession()
}

// ---------------------------------------------------------------------------
// M1: nil-safety
// ---------------------------------------------------------------------------

// Every Pruner method must tolerate a nil receiver — this is what lets
// Config.Pruner be nil-by-default and every call site skip a special case.
func TestNilPrunerIsSafe(t *testing.T) {
	var p *Pruner
	s := newPrunerSession(t)
	s.Append(Msg{Role: "user", Content: "hello"})

	if p.ShouldFire(s) {
		t.Fatal("nil Pruner.ShouldFire returned true")
	}

	after, err := p.Prune(context.Background(), s)
	if err != nil || after != 0 {
		t.Fatalf("nil Pruner.Prune = (%d, %v), want (0, nil)", after, err)
	}

	// ContextTokens touches no receiver state, so it works even on nil.
	if tok := p.ContextTokens(s); tok < 0 {
		t.Fatalf("nil Pruner.ContextTokens = %d", tok)
	}
}

// ---------------------------------------------------------------------------
// M2: ContextTokens
// ---------------------------------------------------------------------------

// ContextTokens must count message content plus tool-call name+arguments,
// divided by four — this is the whole basis ShouldFire's thresholds rest on.
func TestContextTokensCountsContentAndToolCalls(t *testing.T) {
	s := newPrunerSession(t)
	s.Append(Msg{Role: "user", Content: strings.Repeat("a", 40)}) // 40 chars

	var tc ToolCall
	tc.Function.Name = strings.Repeat("n", 8)       // 8 chars
	tc.Function.Arguments = strings.Repeat("x", 12) // 12 chars
	s.Append(Msg{Role: "assistant", Content: strings.Repeat("b", 20), ToolCalls: []ToolCall{tc}})

	p := &Pruner{}
	want := (40 + 20 + 8 + 12) / 4
	if got := p.ContextTokens(s); got != want {
		t.Fatalf("ContextTokens = %d, want %d", got, want)
	}
}

// ---------------------------------------------------------------------------
// M3: ShouldFire policy
// ---------------------------------------------------------------------------

// ShouldFire must stay false under the floor, fire on the first pass above
// it regardless of prior state, and then require pruneGrowth more tokens of
// growth before firing again — otherwise a context sitting just above the
// floor would fire on every single turn.
func TestShouldFirePolicy(t *testing.T) {
	s := newPrunerSession(t)
	p := &Pruner{floor: 10000, growth: 5000}

	// 9999 tokens: strictly under pruneFloor (10000).
	s.Append(Msg{Role: "user", Content: strings.Repeat("a", 39996)})
	if p.ShouldFire(s) {
		t.Fatal("ShouldFire true under the floor")
	}

	// Cross the floor: 10001 tokens. lastFire is still zero, so this must
	// fire regardless of "growth".
	s.Append(Msg{Role: "user", Content: strings.Repeat("b", 8)})
	if got := p.ContextTokens(s); got != 10001 {
		t.Fatalf("test arithmetic is off: ContextTokens = %d, want 10001", got)
	}
	if !p.ShouldFire(s) {
		t.Fatal("ShouldFire false on first crossing of the floor")
	}

	// Simulate a completed pass.
	p.lastFire = p.ContextTokens(s)

	// Grow by 4999 tokens: short of pruneGrowth (5000). Must not re-fire.
	s.Append(Msg{Role: "user", Content: strings.Repeat("c", 19996)})
	if got := p.ContextTokens(s) - p.lastFire; got != 4999 {
		t.Fatalf("test arithmetic is off: growth = %d, want 4999", got)
	}
	if p.ShouldFire(s) {
		t.Fatal("ShouldFire true before reaching the growth bar")
	}

	// One more small append reaches exactly pruneGrowth (5000) of growth.
	s.Append(Msg{Role: "user", Content: strings.Repeat("d", 4)})
	if got := p.ContextTokens(s) - p.lastFire; got != 5000 {
		t.Fatalf("test arithmetic is off: growth = %d, want 5000", got)
	}
	if !p.ShouldFire(s) {
		t.Fatal("ShouldFire false at exactly the growth bar")
	}
}

// ---------------------------------------------------------------------------
// M4-M5: Prune parking
// ---------------------------------------------------------------------------

// Prune must park exactly the IDs the model named, leave everything else
// active, keep the original content in Session.Messages (append-only), and
// show a breadcrumb in the projection instead.
func TestPruneParksNamedBlocksOnly(t *testing.T) {
	s := newPrunerSession(t)
	s.Append(Msg{Role: "user", Content: "first"})
	s.Append(Msg{Role: "assistant", Content: "second"})
	s.Append(Msg{Role: "user", Content: "third"})

	target := s.Messages[0].ID // "first" -> m1
	p := NewPruner(PrunerConfig{Model: funcModel{fn: func(context.Context, Request) (*Msg, error) {
		return &Msg{Role: "assistant", Content: `{"park":[1]}`}, nil
	}}})

	after, err := p.Prune(context.Background(), s)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if after == 0 {
		t.Fatalf("Prune after = %d, want nonzero", after)
	}

	for _, m := range s.Messages {
		wantParked := m.ID == target
		if m.Parked != wantParked {
			t.Fatalf("message %q Parked = %v, want %v", m.ID, m.Parked, wantParked)
		}
	}
	// Append-only: the original content is untouched.
	if s.Messages[0].Content != "first" {
		t.Fatalf("Park mutated stored content: %q", s.Messages[0].Content)
	}

	ctx := s.ContextMessages(0)
	if len(ctx) == 0 || !strings.Contains(ctx[0].Content, "parked") {
		t.Fatalf("ContextMessages does not show a breadcrumb for the parked block: %+v", ctx)
	}
	if !strings.HasPrefix(ctx[0].Content, "[#"+target+" parked") {
		t.Fatalf("ContextMessages[0] = %q, want the breadcrumb form for %s", ctx[0].Content, target)
	}
}

// ---------------------------------------------------------------------------
// M6: parkList parsing
// ---------------------------------------------------------------------------

func TestParkList(t *testing.T) {
	for _, tc := range []struct {
		name    string
		reply   string
		want    []int
		wantErr bool
	}{
		{"object with prose around it", `Sure, here you go: {"park":[3,7,9]} thanks!`, []int{3, 7, 9}, false},
		{"empty park list", `{"park":[]}`, nil, false},
		{"no JSON object at all", `no object here`, nil, true},
		{"malformed JSON", `{"park":[1,`, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parkList(tc.reply)
			if tc.wantErr {
				if err == nil {
					t.Fatal("parkList did not error on a bad reply")
				}
				return
			}
			if err != nil {
				t.Fatalf("parkList: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("parkList = %v, want %v", got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("parkList = %v, want %v", got, tc.want)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// M7: rejects don't discard the whole pass
// ---------------------------------------------------------------------------

// A model that names an unknown id alongside a valid one must still park the
// valid one and report success — the pass did its job. The unusable id is
// reported through Rejected, not as an error: a curator that invents one id
// out of ten has not failed, and calling that "pruning failed" told operators
// a pass had been skipped when it had actually just parked nine blocks.
func TestPruneNamingUnknownIDStillParksValid(t *testing.T) {
	s := newPrunerSession(t)
	s.Append(Msg{Role: "user", Content: "valid block"})      // m1
	s.Append(Msg{Role: "assistant", Content: "old answer"})  // m2
	s.Append(Msg{Role: "user", Content: "most recent user"}) // m3, protected
	s.Append(Msg{Role: "assistant", Content: "keep me"})     // m4, protected

	// Ask to park m1 (valid) and m99 (does not exist).
	p := NewPruner(PrunerConfig{Model: funcModel{fn: func(context.Context, Request) (*Msg, error) {
		return &Msg{Role: "assistant", Content: `{"park":[1,99]}`}, nil
	}}})

	if _, err := p.Prune(context.Background(), s); err != nil {
		t.Fatalf("Prune reported failure for a pass that parked a block: %v", err)
	}
	if !s.Messages[0].Parked {
		t.Fatal("the valid id was not parked despite the other id being rejected")
	}
	if s.Messages[1].Parked {
		t.Fatal("an id nobody named got parked")
	}

	rejected := p.Rejected()
	if len(rejected) != 1 || rejected[0] != "m99" {
		t.Fatalf("Rejected() = %v, want [m99]", rejected)
	}
	if again := p.Rejected(); again != nil {
		t.Fatalf("Rejected() did not clear: %v", again)
	}
}

// A curator naming only protected blocks — what production saw as
// "pruning failed: named 2 unusable block(s): m102, m104" — is a pass that
// parked nothing, but it is still not an error. Nothing went wrong; the model
// simply had no usable suggestion.
func TestPruneNamingOnlyProtectedBlocksIsNotAnError(t *testing.T) {
	s := newPrunerSession(t)
	s.Append(Msg{Role: "user", Content: "old user"})
	s.Append(Msg{Role: "assistant", Content: "old assistant"})
	s.Append(Msg{Role: "user", Content: "most recent user"})      // protected
	s.Append(Msg{Role: "assistant", Content: "most recent asst"}) // protected

	protectedUser := s.Messages[2].ID
	protectedAsst := s.Messages[3].ID

	p := NewPruner(PrunerConfig{Model: funcModel{fn: func(context.Context, Request) (*Msg, error) {
		return &Msg{Role: "assistant", Content: fmt.Sprintf(`{"park":[%s,%s]}`,
			strings.TrimPrefix(protectedUser, "m"), strings.TrimPrefix(protectedAsst, "m"))}, nil
	}}})

	if _, err := p.Prune(context.Background(), s); err != nil {
		t.Fatalf("Prune reported an error for a pass that simply parked nothing: %v", err)
	}
	if len(parkedIDs(s)) != 0 {
		t.Fatalf("a protected block was parked: %v", parkedIDs(s))
	}
	if got := p.Rejected(); len(got) != 2 {
		t.Fatalf("Rejected() = %v, want both protected ids", got)
	}
}

// Re-parking an already-parked block is accepted, not rejected — it still
// has content by way of already being parked (ParkSummary/ParkReason).
func TestPruneRepeatParkOfAlreadyParkedBlockIsAccepted(t *testing.T) {
	s := newPrunerSession(t)
	s.Append(Msg{Role: "user", Content: "block"})
	if err := s.Park(s.Messages[0].ID, "first pass gist", "pruner: not needed"); err != nil {
		t.Fatalf("seed park: %v", err)
	}

	p := NewPruner(PrunerConfig{Model: funcModel{fn: func(context.Context, Request) (*Msg, error) {
		return &Msg{Role: "assistant", Content: `{"park":[1]}`}, nil
	}}})
	_, err := p.Prune(context.Background(), s)
	if err != nil {
		t.Fatalf("re-parking an already-parked block should not be rejected: %v", err)
	}
}

// ---------------------------------------------------------------------------
// M8: failure modes leave the session untouched
// ---------------------------------------------------------------------------

func TestPruneFailureModesLeaveSessionUntouched(t *testing.T) {
	for _, tc := range []struct {
		name  string
		model Model
	}{
		{"model error", funcModel{fn: func(context.Context, Request) (*Msg, error) {
			return nil, errors.New("network exploded")
		}}},
		{"nil reply", funcModel{fn: func(context.Context, Request) (*Msg, error) {
			return nil, nil
		}}},
		{"empty reply", funcModel{fn: func(context.Context, Request) (*Msg, error) {
			return &Msg{Role: "assistant", Content: "   "}, nil
		}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newPrunerSession(t)
			s.Append(Msg{Role: "user", Content: "untouched"})
			p := NewPruner(PrunerConfig{Model: tc.model})

			_, err := p.Prune(context.Background(), s)
			if err == nil {
				t.Fatal("Prune did not report the failure")
			}
			if s.Messages[0].Parked {
				t.Fatal("Prune parked a block despite failing")
			}
			if s.Messages[0].Content != "untouched" {
				t.Fatalf("Prune mutated content on failure: %q", s.Messages[0].Content)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// M9: the pruner's cap is per-request, never mutates the shared model
// ---------------------------------------------------------------------------

// Prune sets MaxTokens:256 on its own request. Since Model.Complete takes
// the cap on the Request rather than on the client, sharing a model between
// the pruner and the main agent cannot leak that cap onto the agent's own
// calls — this test proves it by driving the same model instance through
// both and inspecting what each request actually carried.
func TestPruneSetsRequestCapWithoutMutatingSharedModel(t *testing.T) {
	model := newQueueModel(
		scriptedReply{msg: &Msg{Role: "assistant", Content: `{"park":[]}`}}, // pruner call
		scriptedReply{msg: finalMsg("agent reply")},                         // agent's own call
	)

	s := newPrunerSession(t)
	s.Append(Msg{Role: "user", Content: "hello"})
	p := NewPruner(PrunerConfig{Model: model})

	if _, err := p.Prune(context.Background(), s); err != nil {
		t.Fatalf("Prune: %v", err)
	}

	cfg := Config{Model: model, SystemPrompt: "you are a test agent", ExcludeBuiltins: allBuiltins, Session: s}
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Close()

	if _, err := a.Step(context.Background(), "go"); err != nil {
		t.Fatalf("Step: %v", err)
	}

	reqs := model.reqs()
	if len(reqs) != 2 {
		t.Fatalf("model saw %d requests, want 2", len(reqs))
	}
	if reqs[0].MaxTokens != DefaultSettings().Pruner.MaxTokens {
		t.Fatalf("pruner request MaxTokens = %d, want %d", reqs[0].MaxTokens, DefaultSettings().Pruner.MaxTokens)
	}
	if reqs[1].MaxTokens != 0 {
		t.Fatalf("agent's own request MaxTokens = %d, want 0 (unset) — the pruner's cap leaked onto the shared model", reqs[1].MaxTokens)
	}
}

// ---------------------------------------------------------------------------
// M10: prunerRequest rendering
// ---------------------------------------------------------------------------

func TestPrunerRequestRendering(t *testing.T) {
	s := newPrunerSession(t)
	s.Append(Msg{Role: "system", Content: "system messages are never parkable"})
	s.Append(Msg{Role: "user", Content: "a normal block"}) // gets an ID
	long := strings.Repeat("x", 2500)
	s.Append(Msg{Role: "assistant", Content: long})
	if err := s.Park(s.Messages[2].ID, "gist", "pruner: not needed"); err != nil {
		t.Fatalf("seed park: %v", err)
	}

	req := prunerRequest(s, 0, defaultPrunerReminder)

	if strings.Contains(req, "system messages are never parkable") {
		t.Fatal("prunerRequest included a system message")
	}
	if !strings.Contains(req, "a normal block") {
		t.Fatal("prunerRequest omitted a normal, ID-bearing block")
	}
	if strings.Contains(req, long) {
		t.Fatal("prunerRequest included an already-parked block's content")
	}
	if strings.Contains(req, "(none registered)") == false {
		t.Fatalf("prunerRequest = %q, want the no-task placeholder", req)
	}

	// A separate session to test truncation and the task block, without the
	// park state interfering.
	s2 := newPrunerSession(t)
	if err := s2.RegisterTask("ship it", []TaskStep{{Description: "step one"}}); err != nil {
		t.Fatalf("RegisterTask: %v", err)
	}
	s2.Append(Msg{Role: "user", Content: strings.Repeat("y", 2500)})
	req2 := prunerRequest(s2, 0, defaultPrunerReminder)
	if !strings.Contains(req2, "...[truncated, 2500 chars total]") {
		t.Fatalf("prunerRequest did not truncate an over-cap block: %q", req2)
	}
	if !strings.Contains(req2, "ship it") {
		t.Fatal("prunerRequest omitted the registered task block")
	}
}

// ---------------------------------------------------------------------------
// M11: gist
// ---------------------------------------------------------------------------

func TestGist(t *testing.T) {
	s := newPrunerSession(t)
	s.Append(Msg{Role: "assistant", ToolName: "read", Content: "file contents\nmore lines"})
	s.Append(Msg{Role: "user", Content: strings.Repeat("z", 100)})
	s.Append(Msg{Role: "assistant", Content: ""})

	toolID := s.Messages[0].ID
	if got, want := gist(s, toolID), "read: file contents"; got != want {
		t.Fatalf("gist(tool) = %q, want %q", got, want)
	}

	longID := s.Messages[1].ID
	got := gist(s, longID)
	const wantPrefix = "user: "
	if !strings.HasPrefix(got, wantPrefix) {
		t.Fatalf("gist(long) = %q, want it labelled %q", got, wantPrefix)
	}
	content := strings.TrimPrefix(got, wantPrefix)
	if !strings.HasSuffix(content, "...") || len(content) != 80 {
		t.Fatalf("gist(long) content = %q (len %d), want an 80-char string ending in ...", content, len(content))
	}

	if !s.Messages[2].Parked {
		// empty-content message never got an ID (assignIDs/Append skip
		// empty content), so gist() falls through to its not-found default.
		got := gist(s, "m-does-not-exist")
		if got != "(pruned by curator)" {
			t.Fatalf("gist(unknown id) = %q, want the fallback", got)
		}
	}
}

// ---------------------------------------------------------------------------
// M12: lastFire tracks post-prune size
// ---------------------------------------------------------------------------

// lastFire must be the *post-prune* size, so a successful pass actually
// raises the bar for the next one — otherwise the growth check in
// ShouldFire would use a stale, pre-prune baseline and re-fire immediately.
func TestPruneUpdatesLastFireToPostPruneSize(t *testing.T) {
	s := newPrunerSession(t)
	s.Append(Msg{Role: "user", Content: strings.Repeat("a", 4000)})
	s.Append(Msg{Role: "assistant", Content: strings.Repeat("b", 4000)})
	s.Append(Msg{Role: "user", Content: "most recent user"})       // protected
	s.Append(Msg{Role: "assistant", Content: "most recent reply"}) // protected

	target := s.Messages[0].ID
	p := NewPruner(PrunerConfig{Model: funcModel{fn: func(context.Context, Request) (*Msg, error) {
		return &Msg{Role: "assistant", Content: `{"park":[1]}`}, nil
	}}})

	beforeTokens := p.ContextTokens(s)
	if _, err := p.Prune(context.Background(), s); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	afterTokens := p.ContextTokens(s)

	if p.lastFire != afterTokens {
		t.Fatalf("lastFire = %d, want the post-prune size %d", p.lastFire, afterTokens)
	}
	if afterTokens >= beforeTokens {
		t.Fatalf("parking m1(%s) did not shrink the projected context: before=%d after=%d", target, beforeTokens, afterTokens)
	}
}

// ---------------------------------------------------------------------------
// M13: a Prune failure inside Step does not abort the turn
// ---------------------------------------------------------------------------

// When ShouldFire trips during Step, a failing Prune must emit KindError
// "prune skipped" and let the turn continue — pruning is an optimisation,
// never a hard dependency of answering the user.
func TestStepContinuesWhenPruneFails(t *testing.T) {
	t.Setenv("AXON_SESSION_PATH", filepath.Join(t.TempDir(), "session.json"))
	sess := LoadOrCreateSession()
	// Push the session over pruneFloor before the agent even starts, so the
	// very first Step triggers ShouldFire.
	sess.Append(Msg{Role: "user", Content: strings.Repeat("a", 45000)})

	prunerModel := funcModel{fn: func(context.Context, Request) (*Msg, error) {
		return nil, errors.New("pruner model is down")
	}}
	agentModel := newQueueModel(scriptedReply{msg: finalMsg("answered anyway")})

	cfg := Config{
		Model:           agentModel,
		SystemPrompt:    "you are a test agent",
		ExcludeBuiltins: allBuiltins,
		Session:         sess,
		Pruner:          prunerModel,
	}
	var log eventLog
	cfg.OnEvent = log.record

	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Close()

	res, err := a.Step(context.Background(), "go")
	if err != nil {
		t.Fatalf("Step returned an error despite pruning being an optimisation: %v", err)
	}
	if res.Assistant != "answered anyway" {
		t.Fatalf("turn did not complete after the pruner failed: %+v", res)
	}

	sawPruneSkipped := false
	for _, e := range log.all() {
		if e.Kind == KindError && e.Text == "prune skipped" {
			sawPruneSkipped = true
		}
	}
	if !sawPruneSkipped {
		t.Fatal("no KindError 'prune skipped' event emitted for the failed prune pass")
	}
}

// ---------------------------------------------------------------------------
// Prune modes
// ---------------------------------------------------------------------------

// Each mode must produce its own instruction — otherwise the knob is wired
// to nothing and a config change silently does nothing.
func TestPruneModeInjectsDistinctInstruction(t *testing.T) {
	seen := map[string]PruneMode{}
	for _, mode := range PruneModes {
		p := prunerSystemPrompt(mode)
		if prior, dup := seen[p]; dup {
			t.Fatalf("modes %q and %q produce an identical prompt", prior, mode)
		}
		seen[p] = mode
	}
}

// The safety rules and the answer format are invariants: a mode moves the
// threshold for "still needed", never what may be parked or how to reply.
// An unknown mode must still be a working prompt, not a broken one.
func TestPruneModeKeepsInvariants(t *testing.T) {
	invariants := []string{
		`{"park":[3,7,9]}`,
		"Never park, in any mode:",
		"- a user message",
		"- the most recent assistant message",
		"- a block holding an unresolved error or a failing test",
		"- a block naming a file the agent has edited or is editing",
		"Parking is one-way",
	}

	for _, mode := range append(append([]PruneMode{}, PruneModes...), PruneMode("nonsense")) {
		p := prunerSystemPrompt(mode)
		for _, want := range invariants {
			if !strings.Contains(p, want) {
				t.Errorf("mode %q dropped invariant %q", mode, want)
			}
		}
		if !strings.Contains(p, "MODE:") {
			t.Errorf("mode %q injected no mode instruction", mode)
		}
	}
}

// An unset mode must land on moderate rather than an empty string, which
// would inject nothing and leave the curator with no threshold at all.
func TestPrunerDefaultsToModerate(t *testing.T) {
	p := NewPruner(PrunerConfig{Model: &scriptedModel{}})
	if p.mode != PruneModerate {
		t.Fatalf("zero-value PrunerSettings gave mode %q, want %q", p.mode, PruneModerate)
	}
}

// SetPruneMode must reach the live Pruner, and must refuse a bad value
// rather than applying it.
func TestSetPruneMode(t *testing.T) {
	p := NewPruner(PrunerConfig{Model: &scriptedModel{}})
	a := &Agent{pruner: p, settings: DefaultSettings()}

	a.SetPruneMode(PruneExtreme)
	if p.mode != PruneExtreme || a.settings.Pruner.Mode != PruneExtreme {
		t.Fatalf("SetPruneMode(extreme): pruner=%q settings=%q", p.mode, a.settings.Pruner.Mode)
	}

	a.SetPruneMode(PruneMode("aggressive-ish"))
	if p.mode != PruneExtreme {
		t.Fatalf("SetPruneMode applied an invalid mode: %q", p.mode)
	}
}
