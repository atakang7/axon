package axon

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
)

// CategoryAudit is saved locally for inspection, never injected into the prompt.
// Estimates use the calibrated token meter, not provider billing or a quality probability.
type CategoryAudit struct {
	Plans          []categoryPlan `json:"reviewers"`
	Removed        []string       `json:"removed"`
	Before         int            `json:"estimated_tokens_before"`
	After          int            `json:"estimated_tokens_after"`
	SnapshotBlocks int            `json:"snapshot_blocks"`
}

type categoryResult struct {
	decision categoryDecision
	err      error
}
type categoryJob struct {
	snapshot *Session
	mode     PruneMode
	cancel   context.CancelFunc
	done     chan categoryResult
}

func snapshotSession(s *Session) (*Session, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	var copy Session
	err = json.Unmarshal(b, &copy)
	return &copy, err
}

func (p *Pruner) markCategoryPass(s *Session) {
	p.lastFire = p.ContextTokens(s)
	if c := latestCheckpoint(s); c != nil {
		p.lastCheckpoint = c.ID
	}
}

func (p *Pruner) stopCategoryJob() {
	if p != nil && p.pending != nil {
		p.pending.cancel()
		p.pending = nil
	}
}

func (p *Pruner) startCategoryJob(ctx context.Context, s *Session) error {
	copy, err := snapshotSession(s)
	if err != nil {
		return err
	}
	candidates := p.semanticCandidates(copy)
	p.markCategoryPass(s)
	ctx, cancel := context.WithCancel(ctx)
	job := &categoryJob{snapshot: copy, mode: p.mode, cancel: cancel, done: make(chan categoryResult, 1)}
	p.pending = job
	// Capture values now; model switching and mode changes occur on the loop.
	model, mode, maxTokens := p.model, p.mode, p.maxTokens
	go func() {
		defer cancel()
		d, err := reviewCategories(ctx, model, copy, candidates, mode, maxTokens)
		job.done <- categoryResult{d, err}
	}()
	return nil
}

func (p *Pruner) pruneCategories(ctx context.Context, s *Session) (int, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	p.markCategoryPass(s)
	d, err := reviewCategories(ctx, p.model, s, p.semanticCandidates(s), p.mode, p.maxTokens)
	if err != nil {
		return p.ContextTokens(s), err
	}
	if err := ctx.Err(); err != nil {
		return p.ContextTokens(s), err
	}
	return p.applyCategoryDecision(s, d, len(s.Messages))
}

func (p *Pruner) applyCategoryDecision(s *Session, d categoryDecision, blocks int) (int, error) {
	before := p.ContextTokens(s)
	allowed := map[string]bool{}
	for _, m := range p.semanticCandidates(s) {
		allowed[m.ID] = true
	}
	original := append([]Msg(nil), s.Messages...)
	oldAudit := s.CategoryAudit
	var removed []string
	for id, summary := range d.Park {
		if !allowed[id] {
			continue
		}
		if err := s.Park(id, summary, "category review: unanimous removal; extractive summary; originals available via recall"); err != nil {
			s.Messages = original
			return before, err
		}
		removed = append(removed, id)
	}
	after := p.ContextTokens(s)
	// Includes all summary and breadcrumb overhead. Never buy a larger prompt.
	if after >= before {
		s.Messages = original
		removed = nil
		after = before
	}
	sort.Strings(removed)
	s.CategoryAudit = &CategoryAudit{Plans: d.Plans, Removed: removed, Before: before, After: after, SnapshotBlocks: blocks}
	if err := savePrunedSession(s); err != nil {
		s.Messages = original
		s.CategoryAudit = oldAudit
		return before, err
	}
	p.lastFire = after
	return after, nil
}

// Poll and apply only between complete tool rounds. A changed task, new user
// requirement, modified prefix or mode invalidates the background decision.
func (a *Agent) categoryBoundary(ctx context.Context, start bool) {
	p := a.pruner
	if p == nil || !p.categories {
		return
	}
	if ctx.Err() != nil {
		p.stopCategoryJob()
		return
	}
	if job := p.pending; job != nil {
		select {
		case result := <-job.done:
			p.pending = nil
			job.cancel()
			if result.err != nil {
				a.emit(ctx, Event{Kind: KindError, Err: result.err, Text: "category review skipped; context retained"})
				return
			}
			n := len(job.snapshot.Messages)
			valid := p.mode == job.mode && a.session.ID == job.snapshot.ID && n <= len(a.session.Messages) && a.session.TaskBlock() == job.snapshot.TaskBlock()
			if valid {
				valid = reflect.DeepEqual(a.session.Messages[:n], job.snapshot.Messages)
			}
			if valid {
				for _, m := range a.session.Messages[n:] {
					if m.Role == "user" || m.Role == "system" {
						valid = false
						break
					}
				}
			}
			if !valid {
				a.emit(ctx, Event{Kind: KindInfo, Text: "category review discarded: task or context changed"})
				p.lastFire = 0
				return
			}
			before := p.ContextTokens(a.session)
			after, err := p.applyCategoryDecision(a.session, result.decision, n)
			if err != nil {
				a.emit(ctx, Event{Kind: KindError, Err: err, Text: "category update not persisted"})
			} else {
				a.emit(ctx, Event{Kind: KindPruneEnd, Prune: &PruneInfo{Before: before, After: after}})
			}
			return
		default:
			return
		}
	}
	if start && p.ShouldFire(a.session) {
		before := p.ContextTokens(a.session)
		if err := p.startCategoryJob(ctx, a.session); err != nil {
			a.emit(ctx, Event{Kind: KindError, Err: err})
			return
		}
		a.emit(ctx, Event{Kind: KindInfo, Text: fmt.Sprintf("background category review started: %d estimated tokens, full active context", before)})
	}
}
