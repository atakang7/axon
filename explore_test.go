package axon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const tokenGo = "package auth\n\n// Expired reports whether the token is past its expiry.\nfunc Expired(exp, now int64) bool {\n\treturn exp > now\n}\n\nfunc refresh() {}\n"

// exploreRepo is a workspace with one source file, auth/token.go.
func exploreRepo(t *testing.T) *Session {
	t.Helper()
	s := newTestSession(t)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "auth"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "auth", "token.go"), []byte(strings.ReplaceAll(tokenGo, "\n", "\r\n")), 0644); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCwd(dir); err != nil {
		t.Fatal(err)
	}
	return s
}

func fact(path string, start, end int, quote, claim string) exploreFact {
	return exploreFact{Path: path, StartLine: flexInt(start), EndLine: flexInt(end), Quote: quote, Claim: claim}
}

func TestVerifyFactsKeepsOnlyQuotesThatAreInTheFile(t *testing.T) {
	s := exploreRepo(t)
	facts, rejected := verifyFacts(s, []exploreFact{
		fact("auth/token.go", 5, 5, "return exp > now", "compares expiry with now"),
		fact("auth/token.go", 4, 5, "4\tfunc Expired(exp, now int64) bool {\n5\t\treturn exp > now", "gutter copied from the read"),
		fact("auth/token.go", 1, 1, "func refresh() {}", "line cited wrongly"),
		fact("auth/token.go", 5, 5, "return exp >= now", "invented"),
		fact("auth/missing.go", 1, 1, "x", "no file"),
		fact("auth", 1, 1, "x", "a directory"),
		fact("", 1, 1, "x", "no path"),
		fact("auth/token.go", 5, 5, "", "no quote"),
		fact("auth/token.go", 5, 5, "return exp > now", "duplicate"),
	})
	if len(facts) != 3 {
		t.Fatalf("verified %d facts, want 3: %+v", len(facts), facts)
	}
	if len(rejected) != 5 {
		t.Fatalf("rejected %d, want 5: %v", len(rejected), rejected)
	}
	byClaim := map[string]verifiedFact{}
	for _, f := range facts {
		byClaim[f.claim] = f
	}
	if f := byClaim["line cited wrongly"]; f.start != 8 || !f.moved {
		t.Fatalf("a quote found elsewhere must get its real line: %+v", f)
	}
	if f := byClaim["gutter copied from the read"]; f.start != 4 || f.end != 5 {
		t.Fatalf("multi-line quote lines: %+v", f)
	}
	if f := byClaim["compares expiry with now"]; f.start != 5 || f.moved || !strings.Contains(f.source, "exp > now") {
		t.Fatalf("exact fact: %+v", f)
	}
}

func TestFlexIntAcceptsWhatCheaperModelsSend(t *testing.T) {
	var f struct {
		A, B, C, D flexInt
	}
	if err := json.Unmarshal([]byte(`{"A":12,"B":"13","C":"14-20","D":null}`), &f); err != nil {
		t.Fatal(err)
	}
	if f.A != 12 || f.B != 13 || f.C != 14 || f.D != 0 {
		t.Fatalf("%+v", f)
	}
	if err := json.Unmarshal([]byte(`{"A":"twelve"}`), &f); err == nil {
		t.Fatal("a word was accepted as a line number")
	}
}

// scriptedExplorer replies in order and records every request.
type scriptedExplorer struct {
	mu      sync.Mutex
	replies []*Msg
	reqs    []Request
}

func (m *scriptedExplorer) Complete(_ context.Context, r Request) (*Msg, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reqs = append(m.reqs, r)
	if r.Stream.Usage != nil {
		r.Stream.Usage(1000, 100, 0.001)
	}
	if len(m.replies) == 0 {
		return finalMsg("I am done."), nil
	}
	reply := m.replies[0]
	m.replies = m.replies[1:]
	return reply, nil
}

func reportCall(t *testing.T, r exploreReport) *Msg {
	t.Helper()
	raw, _ := json.Marshal(r)
	return toolCallMsg("r1", toolReport, string(raw))
}

func exploreAgent(t *testing.T, explorer Model) *Agent {
	t.Helper()
	s := exploreRepo(t)
	a := &Agent{session: s, explorers: []Model{explorer}, settings: Settings{}.WithDefaults()}
	a.limits = a.settings.Tools.limits()
	return a
}

func TestExploreReturnsOnlyVerifiedFactsAndNeverTheExplorersReading(t *testing.T) {
	report := exploreReport{Facts: []exploreFact{
		fact("auth/token.go", 5, 5, "return exp > now", "expiry is compared with now"),
		fact("auth/token.go", 5, 5, "return exp >= now", "invented"),
	}}
	report.NotFound = append(report.NotFound, struct {
		What     string `json:"what"`
		Searched string `json:"searched"`
	}{"refresh rotation", "auth/"})
	explorer := &scriptedExplorer{replies: []*Msg{
		{Role: "assistant", Content: "Let me read the file; it probably needs a fix.", ToolCalls: toolCallMsg("c1", toolRead, `{"path":"auth/token.go"}`).ToolCalls},
		toolCallMsg("c2", toolWrite, `{"path":"auth/token.go","mode":"save","content":"x"}`),
		reportCall(t, report),
	}}
	a := exploreAgent(t, explorer)
	var events []Event
	a.onEvent = func(_ context.Context, e Event) { events = append(events, e) }

	out, err := a.exploreTool().Fn(context.Background(), json.RawMessage(`{"question":"where is expiry checked?","paths":["auth/"]}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"1 fact", "auth/token.go:5", "> \treturn exp > now", "explorer's reading: expiry is compared with now", "1 discarded", "refresh rotation — searched auth/", "not proof of absence"} {
		if !strings.Contains(out, want) {
			t.Fatalf("result lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "invented") || strings.Contains(out, "probably needs a fix") {
		t.Fatalf("unverified explorer text reached the deciding model:\n%s", out)
	}
	if data, _ := os.ReadFile(filepath.Join(a.session.Dir(), "auth", "token.go")); !strings.Contains(string(data), "exp > now") {
		t.Fatal("the explorer wrote to the repository")
	}
	// Its write call was refused inside its own conversation.
	third := explorer.reqs[2].Messages
	if got := third[len(third)-1].Content; !strings.Contains(got, `"write" is not available`) {
		t.Fatalf("write was not refused: %q", got)
	}
	for _, spec := range explorer.reqs[0].Tools {
		if spec.Name != toolRead && spec.Name != toolSearch && spec.Name != toolReport {
			t.Fatalf("explorer was offered %s", spec.Name)
		}
	}
	if !strings.Contains(explorer.reqs[0].Messages[1].Content, "Start from: auth/") {
		t.Fatal("starting paths were not passed on")
	}
	if a.session.ExplorerTokens != 3300 || a.session.PromptTokens != 0 || a.session.Cost < 0.0029 {
		t.Fatalf("explorer spend: tokens %d prompt %d cost %f", a.session.ExplorerTokens, a.session.PromptTokens, a.session.Cost)
	}
	if len(events) != 0 {
		t.Fatalf("explore emitted %d events from inside a tool call", len(events))
	}
}

func TestExploreStopsAtItsBudgetAndSaysSo(t *testing.T) {
	explorer := &scriptedExplorer{}
	for i := 0; i < 10; i++ {
		explorer.replies = append(explorer.replies, toolCallMsg("s", toolSearch, `{"pattern":"zzz"}`))
	}
	a := exploreAgent(t, explorer)
	a.settings.Explorer.MaxRounds = 3
	_, err := a.explore(context.Background(), "anything?", nil)
	if err == nil || !strings.Contains(err.Error(), "did not report") || !strings.Contains(err.Error(), "read and search yourself") {
		t.Fatalf("err %v", err)
	}
	if len(explorer.reqs) != 4 {
		t.Fatalf("%d requests, want 3 rounds and one final report-only round", len(explorer.reqs))
	}
	final := explorer.reqs[3]
	if len(final.Tools) != 1 || final.Tools[0].Name != toolReport {
		t.Fatalf("final round offered %v", final.Tools)
	}
}

func TestExploreNudgesOnceWhenTheExplorerAnswersInProse(t *testing.T) {
	explorer := &scriptedExplorer{replies: []*Msg{
		finalMsg("Expiry is in token.go."),
		reportCall(t, exploreReport{Facts: []exploreFact{fact("auth/token.go", 4, 4, "func Expired(exp, now int64) bool {", "defines Expired")}}),
	}}
	a := exploreAgent(t, explorer)
	out, err := a.explore(context.Background(), "where?", nil)
	if err != nil || !strings.Contains(out, "auth/token.go:4") {
		t.Fatalf("out %s err %v", out, err)
	}
}

func TestExploreFailsCleanlyWhenTheExplorerIsDown(t *testing.T) {
	a := exploreAgent(t, funcModel{fn: func(context.Context, Request) (*Msg, error) {
		return nil, &APIError{Status: 400, Body: "bad"}
	}})
	_, err := a.explore(context.Background(), "where?", nil)
	if err == nil || !strings.Contains(err.Error(), "read and search yourself") {
		t.Fatalf("err %v", err)
	}
	a.explorers = nil
	if _, err := a.explore(context.Background(), "where?", nil); err == nil {
		t.Fatal("explore without an explorer model succeeded")
	}
}

func TestExploreCallsRunConcurrently(t *testing.T) {
	var calls []ToolCall
	for _, name := range []string{toolExplore, toolRead, toolExplore, toolWrite, toolExplore} {
		var tc ToolCall
		tc.Function.Name = name
		calls = append(calls, tc)
	}
	groups := toolGroups(calls)
	if len(groups) != 3 || len(groups[0]) != 3 || len(groups[1]) != 1 || len(groups[2]) != 1 {
		t.Fatalf("groups %d: %v", len(groups), groups)
	}
	var g gatherTracker
	if g.observe(calls[:1]) != 0 || g.observe(calls[:1]) != 0 || g.observe(calls[:1]) != 0 {
		t.Fatal("single explore calls were treated as drip lookups")
	}
}

func TestSetExplorerModelOffersAndRemovesTheTool(t *testing.T) {
	a, err := New(baseConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.SetExplorerModel(&scriptedExplorer{})
	if !hasTool(a.tools, toolExplore) {
		t.Fatal("explore not offered after setting an explorer")
	}
	a.SetExplorerModel(nil)
	if hasTool(a.tools, toolExplore) {
		t.Fatal("explore still offered without an explorer")
	}
}

func TestExploreHonoursItsTimeout(t *testing.T) {
	a := exploreAgent(t, funcModel{fn: func(ctx context.Context, _ Request) (*Msg, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}})
	a.settings.Explorer.Timeout = Duration(50 * time.Millisecond)
	start := time.Now()
	if _, err := a.explore(context.Background(), "where?", nil); err == nil || time.Since(start) > 5*time.Second {
		t.Fatalf("err %v after %s", err, time.Since(start))
	}
}
