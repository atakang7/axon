package axon

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func categoryJSON(candidates []Msg, contributes bool, keep []string) string {
	ids := []string{}
	for _, m := range candidates {
		ids = append(ids, m.ID)
	}
	p := categoryPlan{Categories: []contextCategory{{Name: "exploration", IDs: ids, Contributes: &contributes, Keep: keep, Reason: "Result superseded by current direction", Summary: []categoryFact{{ID: ids[0], Quote: string([]rune(candidates[0].Content)[:10])}}}}}
	b, _ := json.Marshal(p)
	return string(b)
}

func categoryFixture(t *testing.T) (*Session, *Pruner, []Msg) {
	s, p := semanticFixture(t)
	p.categories = true
	return s, p, p.semanticCandidates(s)
}

func TestCategoryPlanEnforcesModeCoverageAndGrounding(t *testing.T) {
	_, _, c := categoryFixture(t)
	valid := categoryJSON(c, false, nil)
	if _, err := parseCategoryPlan(valid, c, PruneModerate); err != nil {
		t.Fatal(err)
	}
	drops := func(raw string, mode PruneMode) map[string]bool {
		t.Helper()
		plan, err := parseCategoryPlan(raw, c, mode)
		if err != nil {
			t.Fatalf("flaw confined to one category rejected the whole plan: %v\n%s", err, raw)
		}
		return categoryDrops(plan)
	}
	partial := categoryJSON(c, true, []string{c[0].ID})
	if len(drops(partial, PruneModerate)) != 0 {
		t.Fatal("normal mode pruned inside useful category")
	}
	if d := drops(partial, PruneExtreme); d[c[0].ID] || !d[c[1].ID] {
		t.Fatal("extreme mode did not prune inside useful category", d)
	}
	// Flaws retain the blocks they touch; nothing unvoted or unproven is removed.
	if len(drops(strings.Replace(valid, `"contributes":false`, `"contributes":null`, 1), PruneExtreme)) != 0 {
		t.Fatal("category without a decision was removed")
	}
	if d := drops(strings.Replace(valid, `"`+c[1].ID+`"`, `"m999"`, 1), PruneExtreme); d[c[1].ID] || d["m999"] {
		t.Fatal("unknown or uncategorized block was removed", d)
	}
	if d := drops(categoryJSON(c[:1], false, nil), PruneExtreme); d[c[1].ID] {
		t.Fatal("candidate the plan never mentioned was removed")
	}
	// A broken response is still an error.
	for _, raw := range []string{
		`{"categories":[]}`, valid + "junk", valid + valid,
	} {
		if _, err := parseCategoryPlan(raw, c, PruneExtreme); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	var plan categoryPlan
	_ = json.Unmarshal([]byte(valid), &plan)
	plan.Categories[0].Summary[0].Quote = "fabricated successful check"
	b, _ := json.Marshal(plan)
	// A fabricated quote never becomes memory.
	got, err := parseCategoryPlan(string(b), c, PruneExtreme)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Categories[0].Summary) != 0 || strings.Contains(groupEvidence(got.Categories[0].IDs, []categoryPlan{got}, c), "fabricated") {
		t.Fatal("fabricated memory accepted")
	}
}

func TestCategoryRequestIncludesMiddleAndAllCandidates(t *testing.T) {
	s, p, _ := categoryFixture(t)
	s.Messages[1].Content = strings.Repeat("a", 5000) + "MIDDLE_EVIDENCE" + strings.Repeat("z", 5000)
	for range 40 {
		s.Append(Msg{Role: "tool", ToolName: "read", Content: strings.Repeat("source", 200)})
	}
	c := p.semanticCandidates(s)
	if len(c) < 30 {
		t.Fatal("candidate count was capped")
	}
	r := categoryRequest(s, c, PruneExtreme)
	if !strings.Contains(r.Messages[1].Content, s.Messages[1].Content) {
		t.Fatal("full observation truncated")
	}
	if !strings.Contains(r.Messages[1].Content, s.Messages[0].Content) {
		t.Fatal("user requirements truncated")
	}
}

func TestCategoryReviewParallelUnanimityAndRecall(t *testing.T) {
	s, p, c := categoryFixture(t)
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	model := func(keep []string) Model {
		return funcModel{fn: func(ctx context.Context, r Request) (*Msg, error) {
			started <- struct{}{}
			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return &Msg{Content: categoryJSON(c, len(keep) > 0, keep)}, nil
		}}
	}
	pool, _ := NewConsensusPruner(model(nil), model([]string{c[1].ID}))
	p.model = pool
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := p.Prune(ctx, s); done <- err }()
	for range 2 {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("not parallel")
		}
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	parked := map[string]bool{}
	for _, m := range s.Messages {
		parked[m.ID] = m.Parked
	}
	if !parked[c[0].ID] || parked[c[1].ID] {
		t.Fatal("did not intersect per-block votes")
	}
	if s.CategoryAudit == nil || len(s.CategoryAudit.Plans) != 2 || s.CategoryAudit.After >= s.CategoryAudit.Before {
		t.Fatal("missing savings audit")
	}
	raw, _ := json.Marshal(map[string]string{"id": c[0].ID})
	out, err := RecallTool(s).Fn(ctx, raw)
	if err != nil || !strings.Contains(out, "old source") {
		t.Fatalf("recall: %s %v", out, err)
	}
	loaded := LoadOrCreateSessionAt(s.path)
	if loaded.CategoryAudit == nil {
		t.Fatal("audit not persisted")
	}
}

func TestCategoryReviewFailureRetainsHistory(t *testing.T) {
	s, p, c := categoryFixture(t)
	good := funcModel{fn: func(context.Context, Request) (*Msg, error) { return &Msg{Content: categoryJSON(c, false, nil)}, nil }}
	bad := funcModel{fn: func(context.Context, Request) (*Msg, error) { return nil, fmt.Errorf("429") }}
	pool, _ := NewConsensusPruner(good, bad)
	p.model = pool
	if _, err := p.Prune(context.Background(), s); err == nil {
		t.Fatal("missing vote accepted")
	}
	for _, m := range s.Messages {
		if m.Parked {
			t.Fatal("parked after failure")
		}
	}
}

func TestCategoryCommitRequiresNetSavingsAndPersistsAtomically(t *testing.T) {
	s, p, c := categoryFixture(t)
	before := p.ContextTokens(s)
	after, err := p.applyCategoryDecision(s, categoryDecision{Park: map[string]string{c[0].ID: strings.Repeat("summary", 20000)}}, len(s.Messages))
	if err != nil || after != before || s.Messages[1].Parked {
		t.Fatal("expanding summary applied")
	}
	s.path = t.TempDir()
	_, err = p.applyCategoryDecision(s, categoryDecision{Park: map[string]string{c[0].ID: "short"}}, len(s.Messages))
	if err == nil || s.Messages[1].Parked {
		t.Fatal("failed save did not roll back")
	}
}

func TestCategoryBackgroundDoesNotBlockAndRejectsChangedTask(t *testing.T) {
	s, p, c := categoryFixture(t)
	started := make(chan struct{})
	release := make(chan struct{})
	p.model = funcModel{fn: func(ctx context.Context, r Request) (*Msg, error) {
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return &Msg{Content: categoryJSON(c, false, nil)}, nil
	}}
	a := &Agent{session: s, pruner: p}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	a.categoryBoundary(ctx, true)
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("worker not started")
	}
	if p.pending == nil {
		t.Fatal("no pending background work")
	}
	s.Append(Msg{Role: "user", Content: "New requirement: preserve exploration"})
	close(release)
	job := p.pending
	select {
	case result := <-job.done:
		job.done <- result
	case <-ctx.Done():
		t.Fatal("review timed out")
	}
	a.categoryBoundary(ctx, false)
	if p.pending != nil {
		t.Fatal("worker not drained")
	}
	for _, m := range s.Messages {
		if m.Parked {
			t.Fatal("stale result applied")
		}
	}
}

// A bad quote never becomes memory, but it does not void the reviewer's
// decision either: the block's own first line is remembered instead.
func TestUngroundedQuoteIsNeverRemembered(t *testing.T) {
	_, _, c := categoryFixture(t)
	no := false
	ids := []string{}
	for _, m := range c {
		ids = append(ids, m.ID)
	}
	plan := categoryPlan{Categories: []contextCategory{
		{Name: "invented", IDs: ids[:1], Contributes: &no, Reason: "r", Summary: []categoryFact{{ID: ids[0], Quote: "text that is nowhere in the source"}}},
		{Name: "real", IDs: ids[1:], Contributes: &no, Reason: "r", Summary: []categoryFact{{ID: ids[1], Quote: strings.Join(strings.Fields(c[1].Content)[:2], "  \t ")}}},
	}}
	b, _ := json.Marshal(plan)
	got, err := parseCategoryPlan(string(b), c, PruneModerate)
	if err != nil {
		t.Fatal(err)
	}
	drops := categoryDrops(got)
	if !drops[ids[0]] || !drops[ids[1]] {
		t.Fatal("decision voided by quoting", drops)
	}
	if q := got.Categories[1].Summary[0].Quote; !strings.Contains(c[1].Content, q) {
		t.Fatalf("stored quote %q is not the exact source text", q)
	}
	memory := groupEvidence(ids, []categoryPlan{got}, c)
	if strings.Contains(memory, "nowhere in the source") {
		t.Fatal("ungrounded quote reached memory")
	}
	if !strings.Contains(memory, "["+ids[0]+"] "+firstLine(c[0].Content)) {
		t.Fatalf("unquoted block has no source excerpt:\n%s", memory)
	}
}

// A reviewer that omits candidates is asked once, in the same conversation,
// to complete its plan; the completed plan is the one that votes.
func TestReviewerThatOmitsCandidatesIsAskedToComplete(t *testing.T) {
	s, _, c := categoryFixture(t)
	calls := 0
	m := funcModel{fn: func(_ context.Context, r Request) (*Msg, error) {
		calls++
		if calls == 1 {
			return &Msg{Content: categoryJSON(c[:1], false, nil)}, nil
		}
		last := r.Messages[len(r.Messages)-1].Content
		if r.Messages[len(r.Messages)-2].Role != "assistant" || !strings.Contains(last, c[1].ID) {
			t.Fatalf("follow-up does not continue the conversation or name the missing IDs: %q", last)
		}
		return &Msg{Content: categoryJSON(c, false, nil)}, nil
	}}
	plan, err := reviewOnce(context.Background(), m, categoryRequest(s, c, PruneModerate), c, PruneModerate)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(uncategorized(plan, c)) != 0 {
		t.Fatalf("calls=%d missing=%v", calls, uncategorized(plan, c))
	}
}

func TestExtraFieldsDoNotVoidAReview(t *testing.T) {
	_, _, c := categoryFixture(t)
	raw := strings.Replace(categoryJSON(c, false, nil), `"reason":`, `"note":"x","reason":`, 1)
	plan, err := parseCategoryPlan(raw, c, PruneModerate)
	if err != nil || len(categoryDrops(plan)) != len(c) {
		t.Fatal(err)
	}
	// A fact whose text is under the wrong key is simply not memory.
	plan, err = parseCategoryPlan(strings.Replace(raw, `"quote":`, `"invented":`, 1), c, PruneModerate)
	if err != nil || len(plan.Categories[0].Summary) != 0 {
		t.Fatal("misnamed quote became memory", err)
	}
}
