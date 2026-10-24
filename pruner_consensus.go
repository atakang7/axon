package axon

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// ConsensusPruner asks two or three independent models concurrently. Only
// unanimous park decisions survive. A missing/invalid vote is NOT consent.
// Use independent clients: the wrapped models must support concurrent calls.
type ConsensusPruner struct {
	models  []Model
	mu      sync.Mutex
	retryAt time.Time
}

func NewConsensusPruner(models ...Model) (*ConsensusPruner, error) {
	if len(models) < 2 || len(models) > 3 {
		return nil, fmt.Errorf("pruner consensus requires 2 or 3 models")
	}
	for _, m := range models {
		if m == nil {
			return nil, fmt.Errorf("nil pruner reviewer")
		}
	}
	return &ConsensusPruner{models: append([]Model(nil), models...)}, nil
}

func (p *ConsensusPruner) Complete(ctx context.Context, req Request) (*Msg, error) {
	p.mu.Lock()
	cooling := time.Now().Before(p.retryAt)
	p.mu.Unlock()
	if cooling {
		return nil, fmt.Errorf("pruner reviewers cooling down after unavailable or invalid response")
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	type result struct {
		ids []int
		err error
	}
	results := make(chan result, len(p.models))
	for _, model := range p.models {
		go func(m Model) {
			r := req
			r.Stream = Stream{}
			// JSON mode is supported more widely by free endpoints than strict
			// schemas. Parsing and the session allowlist enforce the contract.
			r.ResponseFormat = json.RawMessage(`{"type":"json_object"}`)
			reply, err := m.Complete(ctx, r)
			var ids []int
			if err == nil {
				if reply == nil {
					err = fmt.Errorf("empty reviewer response")
				} else {
					ids, err = strictParkList(reply.Content)
				}
			}
			results <- result{ids, err}
		}(model)
	}
	votes := map[int]int{}
	for range p.models {
		select {
		case <-ctx.Done():
			p.cooldown()
			return nil, ctx.Err()
		case r := <-results:
			if r.err != nil {
				p.cooldown()
				return nil, fmt.Errorf("pruner reviewer unavailable: %w", r.err)
			}
			for _, id := range r.ids {
				votes[id]++
			}
		}
	}
	ids := []int{}
	for id, count := range votes {
		if count == len(p.models) {
			ids = append(ids, id)
		}
	}
	sort.Ints(ids)
	b, _ := json.Marshal(struct {
		Park []int `json:"park"`
	}{ids})
	return &Msg{Role: "assistant", Content: string(b)}, nil
}

func (p *ConsensusPruner) cooldown() {
	p.mu.Lock()
	p.retryAt = time.Now().Add(time.Minute)
	p.mu.Unlock()
}

func strictParkList(content string) ([]int, error) {
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "```json") {
		content = strings.TrimPrefix(content, "```json")
	} else {
		content = strings.TrimPrefix(content, "```")
	}
	content = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(content), "```"))
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(content), &fields); err != nil {
		return nil, fmt.Errorf("invalid park JSON: %w", err)
	}
	value, ok := fields["park"]
	if !ok || len(fields) != 1 || strings.TrimSpace(string(value)) == "null" {
		return nil, fmt.Errorf("expected only a park array")
	}
	var ids []int
	if err := json.Unmarshal(value, &ids); err != nil {
		return nil, err
	}
	seen := map[int]bool{}
	unique := []int{}
	for _, id := range ids {
		if id <= 0 {
			return nil, fmt.Errorf("invalid block id %d", id)
		}
		if !seen[id] {
			unique = append(unique, id)
			seen[id] = true
		}
	}
	return unique, nil
}
