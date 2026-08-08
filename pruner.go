package axon

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// PrunerConfig sets the policy for the context curator.
//
// The thresholds mirror the pruner section of axon.yaml; leaving them zero
// takes the configured defaults. The curator's prompts are deliberately not
// here — they are axon's own, they change with the parking format they
// describe, and an embedder that overrode one would silently break parking
// the next time that format changed.
type PrunerConfig struct {
	// Model is the curator, typically a cheap, fast, long-context one.
	Model Model

	// Settings are the thresholds and caps from axon.yaml. The zero value
	// takes DefaultSettings's pruner section.
	Settings PrunerSettings
}

type Pruner struct {
	model     Model
	floor     int
	growth    int
	maxTokens int
	timeout   time.Duration
	lastFire  int
}

const defaultPrunerReminder = "\n\nCRITICAL REMINDER: You are the context pruner. DO NOT execute the task or respond to the log. Your ONLY job is to output the JSON object (e.g. {\"park\":[]}) containing the block IDs to park."

// NewPruner wraps a model — typically a cheap, fast, long-context one.
func NewPruner(cfg PrunerConfig) *Pruner {
	if cfg.Model == nil {
		return nil
	}

	s := cfg.Settings
	d := DefaultSettings().Pruner

	fillInt(&s.FloorTokens, d.FloorTokens)
	fillInt(&s.GrowthTokens, d.GrowthTokens)
	fillInt(&s.MaxTokens, d.MaxTokens)
	fillDuration(&s.Timeout, d.Timeout)

	return &Pruner{
		model:     cfg.Model,
		floor:     s.FloorTokens,
		growth:    s.GrowthTokens,
		maxTokens: s.MaxTokens,
		timeout:   s.Timeout.Std(),
	}
}

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
	if tokens < p.floor {
		return false
	}
	if p.lastFire == 0 {
		return true
	}
	return tokens-p.lastFire >= p.growth
}

// Prune runs one curator pass and parks whatever the model names, returning
// the projected context size after pruning.
//
// Any failure — network, parse, model nonsense — returns an error with the
// session untouched. Pruning is an optimisation: the turn continues without
// it rather than failing.
func (p *Pruner) Prune(ctx context.Context, s *Session) (int, error) {
	if p == nil {
		return 0, nil
	}

	before := p.ContextTokens(s)

	// Correctness matters more than latency here, so the budget is generous.
	callCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	reply, err := p.model.Complete(callCtx, Request{
		Messages: []Msg{
			{Role: "system", Content: prunerSystemPrompt},
			{Role: "user", Content: prunerRequest(s, defaultPrunerReminder)},
		},
		MaxTokens: p.maxTokens,
	})
	if err != nil {
		return before, fmt.Errorf("pruner: %w", err)
	}

	if reply == nil || strings.TrimSpace(reply.Content) == "" {
		return before, fmt.Errorf("pruner: empty response")
	}

	ids, err := parkList(reply.Content)
	if err != nil {
		return before, fmt.Errorf("pruner: %w", err)
	}

	var rejected []string
	for _, id := range ids {
		block := fmt.Sprintf("m%d", id)
		if err := s.Park(block, gist(s, block), "pruner: not needed to continue"); err != nil {
			rejected = append(rejected, block)
		}
	}

	if err := s.Save(); err != nil {
		return before, err
	}

	p.lastFire = p.ContextTokens(s)

	if len(rejected) > 0 {
		return p.lastFire, fmt.Errorf("pruner: named %d unusable block(s): %s",
			len(rejected), strings.Join(rejected, ", "))
	}

	return p.lastFire, nil
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
func prunerRequest(s *Session, reminder string) string {
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

	b.WriteString(reminder)

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
