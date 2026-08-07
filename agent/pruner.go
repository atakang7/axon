package agent

// pruner.go — context curator.
//
// A small, fast model that runs out-of-band and decides which blocks the main
// agent no longer needs. The runtime parks what it names: the block is
// replaced in the model's view by a one-line breadcrumb, while the original
// stays in the session for human audit. The main agent has no memory tools and
// does not know any of this is happening.
//
// There is exactly one verb. An earlier design had three states — active,
// parked, forgotten — and a recall path back to the agent. Nothing ever used
// forget or recall, so they are gone: a block is either active or parked, and
// parking is one-way as far as the agent is concerned. One verb is also why
// the prompt and the parser can no longer disagree about what was asked for.
//
// The pruner fires only when the context is large enough for curation to pay
// for itself AND has grown meaningfully since the last pass. Sharp growth is
// the strongest signal that garbage just arrived.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Pruner is the curator: a client for a cheap, fast, long-context model plus
// the policy for when to fire. It owns its own bookkeeping — the agent holds
// no pruning state.
type Pruner struct {
	model Model

	// lastFire is the projected context size at the previous successful pass,
	// zero if it has never run.
	lastFire int
}

// Thresholds. Below the floor, curation costs more than it saves. Above it,
// the growth bar stops the pruner re-firing every single turn.
const (
	pruneFloor  = 10000
	pruneGrowth = 5000
)

// NewPruner wraps a model — typically a cheap, fast, long-context one. Pass
// nil to disable pruning; every method is safe on a nil *Pruner, so the caller
// needs no special case.
//
// The model may be shared with the agent. The output cap lives on the request,
// not on the model, so the pruner cannot narrow anyone else's budget. An
// earlier version set MaxTokens on the client it was handed, which would have
// silently capped a shared model at 256 tokens.
func NewPruner(m Model) *Pruner {
	if m == nil {
		return nil
	}
	return &Pruner{model: m}
}

// prunerMaxTokens caps the curator's reply. The answer is one line of JSON; a
// chatty model that wants to think out loud hits this wall instead of burning
// tokens on prose nobody reads. Increased to 4096 to accommodate reasoning models
// that must "think" before they output the final JSON object.
const prunerMaxTokens = 4096

// ContextTokens estimates what the next request will cost. Character count
// over four is plenty for a threshold decision; provider-accurate counting
// would cost a round trip to answer a question we only need roughly right.
func (p *Pruner) ContextTokens(s *Session) int {
	n := 0
	for _, m := range s.ContextMessages() {
		n += len(m.Content)
		for _, tc := range m.ToolCalls {
			n += len(tc.Function.Arguments) + len(tc.Function.Name)
		}
	}
	return n / 4
}

// ShouldFire reports whether a curator pass is worth making now.
func (p *Pruner) ShouldFire(s *Session) bool {
	if p == nil {
		return false
	}
	tokens := p.ContextTokens(s)
	if tokens < pruneFloor {
		return false
	}
	if p.lastFire == 0 {
		return true
	}
	return tokens-p.lastFire >= pruneGrowth
}

// Prune runs one curator pass and parks whatever the model names, returning
// the projected context size before and after.
//
// Any failure — network, parse, model nonsense — returns an error with the
// session untouched. Pruning is an optimisation: the turn continues without
// it rather than failing.
func (p *Pruner) Prune(ctx context.Context, s *Session) (before, after int, err error) {
	if p == nil {
		return 0, 0, nil
	}
	before = p.ContextTokens(s)

	// Correctness matters more than latency here, so the budget is generous.
	callCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	reply, err := p.model.Complete(callCtx, Request{
		Messages: []Msg{
			{Role: "system", Content: prunerSystemPrompt},
			{Role: "user", Content: prunerRequest(s)},
		},
		MaxTokens: prunerMaxTokens,
	})
	if err != nil {
		return before, before, fmt.Errorf("pruner: %w", err)
	}
	if reply == nil || strings.TrimSpace(reply.Content) == "" {
		return before, before, fmt.Errorf("pruner: empty response")
	}

	ids, err := parkList(reply.Content)
	if err != nil {
		return before, before, fmt.Errorf("pruner: %w", err)
	}
	// A model that names a block that does not exist, or one already parked,
	// is a bad answer rather than a broken session: park what is valid and
	// report the rest instead of discarding the whole pass. Silently ignoring
	// them would hide a pruner that had started hallucinating ids entirely.
	var rejected []string
	for _, id := range ids {
		block := fmt.Sprintf("m%d", id)
		if err := s.Park(block, gist(s, block), "pruner: not needed to continue"); err != nil {
			rejected = append(rejected, block)
		}
	}
	if err := s.Save(); err != nil {
		return before, before, err
	}

	p.lastFire = p.ContextTokens(s)
	if len(rejected) > 0 {
		return before, p.lastFire, fmt.Errorf("pruner: named %d unusable block(s): %s",
			len(rejected), strings.Join(rejected, ", "))
	}
	return before, p.lastFire, nil
}

// parkList pulls {"park":[...]} out of the reply, tolerating any prose the
// model wrapped it in.
func parkList(reply string) ([]int, error) {
	start := strings.IndexByte(reply, '{')
	end := strings.LastIndexByte(reply, '}')
	if start < 0 || end < start {
		return nil, fmt.Errorf("no JSON object in response")
	}
	var out struct {
		Park []int `json:"park"`
	}
	if err := json.Unmarshal([]byte(reply[start:end+1]), &out); err != nil {
		return nil, err
	}
	return out.Park, nil
}

// gist builds the one-line summary shown in a block's breadcrumb. The pruner
// only emits IDs, so the runtime synthesises this from the block's first line.
func gist(s *Session, id string) string {
	for _, m := range s.Messages {
		if m.ID != id {
			continue
		}
		label := m.Role
		if m.ToolName != "" {
			label = m.ToolName
		}
		first, _, _ := strings.Cut(m.Content, "\n")
		first = strings.TrimSpace(first)
		if len(first) > 80 {
			first = first[:77] + "..."
		}
		if first == "" {
			return "(" + label + ")"
		}
		return label + ": " + first
	}
	return "(pruned by curator)"
}

// prunerRequest renders the task and the active log, each block labelled with
// the ID the pruner will name it by. Already-parked blocks are omitted: they
// are not in the model's context any more, so they are not up for decision.
func prunerRequest(s *Session) string {
	var b strings.Builder

	b.WriteString("# TASK\n")
	if task := s.TaskBlock(); task != "" {
		b.WriteString(task)
	} else {
		b.WriteString("(none registered)")
	}

	b.WriteString("\n\n# LOG\n")
	for _, m := range s.Messages {
		if m.Role == "system" || m.ID == "" || m.Parked {
			continue
		}
		label := m.Role
		if m.ToolName != "" {
			label = "tool:" + m.ToolName
		}
		fmt.Fprintf(&b, "\n[%s | %s]\n", m.ID, label)

		// Show the shape of a block, not every byte of it. Deciding that a
		// 500-line file dump can be parked does not require re-reading it.
		const cap = 2000
		if len(m.Content) > cap {
			fmt.Fprintf(&b, "%s\n...[truncated, %d chars total]\n", m.Content[:cap], len(m.Content))
		} else {
			b.WriteString(m.Content + "\n")
		}
	}
	return b.String()
}

const prunerSystemPrompt = `You keep an agent's working memory small.

You are shown the agent's task and a numbered log of what has happened. Decide
which blocks it no longer needs in order to finish that task.

Answer with one JSON object and nothing that matters after it:

{"park":[3,7,9]}

Block ids are the integers in the labels (m7 is 7). {"park":[]} means keep
everything, and is a normal, common answer.

Never park:
- a user message
- the most recent assistant message
- a block holding an unresolved error or a failing test
- a block naming a file the agent has edited or is editing
- a block the agent will need to quote when it answers

Parking is one-way: the agent cannot get a parked block back. When a block is
merely probably useless, keep it.`
