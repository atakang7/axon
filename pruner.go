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
	mode      PruneMode
	window    int
	floor     int
	growth    int
	maxTokens int
	timeout   time.Duration
	lastFire  int
}

const defaultPrunerReminder = "\n\nCRITICAL REMINDER: You are the context pruner. DO NOT execute the task or respond to the log. Your ONLY job is to output the JSON object (e.g. {\"park\":[]}) containing the block IDs to park."

// parkListSchema constrains the curator's reply to the one shape parkList
// reads, forwarded as the request's response_format.
//
// Without it a reasoning model routinely answers entirely in reasoning tokens
// and emits nothing in content, which arrives here as "empty response" and is
// indistinguishable from a dead provider. A strict schema forces the final
// answer into content. parkList still tolerates prose around the object,
// because a provider that ignores response_format must not become a hard
// failure — the constraint is an improvement, not a new requirement.
const parkListSchema = `{
  "type": "json_schema",
  "json_schema": {
    "name": "park_list",
    "strict": true,
    "schema": {
      "type": "object",
      "properties": {
        "park": {
          "type": "array",
          "items": {"type": "integer"},
          "description": "Block ids to park. The integer in m7 is 7."
        }
      },
      "required": ["park"],
      "additionalProperties": false
    }
  }
}`

// NewPruner wraps a model — typically a cheap, fast, long-context one.
func NewPruner(cfg PrunerConfig) *Pruner {
	if cfg.Model == nil {
		return nil
	}

	s := cfg.Settings
	d := DefaultSettings().Pruner

	if s.Mode == "" {
		s.Mode = d.Mode
	}
	fillInt(&s.WindowBlocks, d.WindowBlocks)
	fillInt(&s.FloorTokens, d.FloorTokens)
	fillInt(&s.GrowthTokens, d.GrowthTokens)
	fillInt(&s.MaxTokens, d.MaxTokens)
	fillDuration(&s.Timeout, d.Timeout)

	return &Pruner{
		model:     cfg.Model,
		mode:      s.Mode,
		window:    s.WindowBlocks,
		floor:     s.FloorTokens,
		growth:    s.GrowthTokens,
		maxTokens: s.MaxTokens,
		timeout:   s.Timeout.Std(),
	}
}

// ContextTokens estimates what the next request will cost. Character count
// over four is plenty for a threshold decision; provider-accurate counting
// would cost a round trip to answer a question we only need roughly right.
//
// It applies the same window a nil-safe caller would get from settings
// directly, so ShouldFire's thresholds are compared against what the model
// will actually be sent, not the size of the full unwindowed log.
func (p *Pruner) ContextTokens(s *Session) int {
	window := 0
	if p != nil {
		window = p.window
	}

	n := 0
	for _, m := range s.ContextMessages(window) {
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
//
// A failure still advances lastFire to the size Prune was called at. Without
// that, ShouldFire's "lastFire == 0 means fire" rule would retry a curator
// that is timing out or offline on every single loop iteration — paying its
// full timeout as added latency on every tool call for the rest of the turn.
// The next attempt instead waits for another growth-tokens' worth of context,
// same cadence as a successful pass.
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
			{Role: "system", Content: prunerSystemPrompt(p.mode)},
			{Role: "user", Content: prunerRequest(s, p.window, defaultPrunerReminder)},
		},
		MaxTokens:      p.maxTokens,
		ResponseFormat: json.RawMessage(parkListSchema),
	})
	if err != nil {
		p.lastFire = before
		return before, fmt.Errorf("pruner: %w", err)
	}

	if reply == nil || strings.TrimSpace(reply.Content) == "" {
		p.lastFire = before
		return before, fmt.Errorf("pruner: empty response")
	}

	ids, err := parkList(reply.Content)
	if err != nil {
		p.lastFire = before
		return before, fmt.Errorf("pruner: %w", err)
	}

	protected := protectedIDs(s)

	var rejected []string
	for _, id := range ids {
		block := fmt.Sprintf("m%d", id)
		if protected[block] {
			rejected = append(rejected, block)
			continue
		}
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

// protectedIDs returns the block IDs the curator must never park: the most
// recent user message and the most recent assistant message. The system
// prompt already tells the curator this, but a cheap or weak model cannot be
// trusted to follow it — losing either one leaves the agent unable to tell
// what it was just asked or what it just said, which is what sends a
// confused model back to redo work it already did.
func protectedIDs(s *Session) map[string]bool {
	protected := map[string]bool{}

	var haveUser, haveAssistant bool
	for i := len(s.Messages) - 1; i >= 0 && !(haveUser && haveAssistant); i-- {
		m := s.Messages[i]
		if m.ID == "" || m.Parked {
			continue
		}

		switch {
		case m.Role == "user" && !haveUser:
			protected[m.ID] = true
			haveUser = true
		case m.Role == "assistant" && !haveAssistant:
			protected[m.ID] = true
			haveAssistant = true
		}
	}

	return protected
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
//
// Blocks still inside the recency window are omitted too — the window has
// already handled them for free, and showing the curator the whole log
// would make every judgment call cost tokens on blocks nobody was ever
// going to ask it about.
func prunerRequest(s *Session, windowBlocks int, reminder string) string {
	var b strings.Builder

	b.WriteString("# TASK\n")
	if task := s.TaskBlock(); task != "" {
		b.WriteString(task)
	} else {
		b.WriteString("(none registered)")
	}

	cut := windowCutoff(s, windowBlocks)

	b.WriteString("\n\n# LOG\n")
	for _, m := range s.Messages {
		if m.Role == "system" || m.ID == "" || m.Parked {
			continue
		}
		if windowBlocks > 0 && !cut[m.ID] {
			continue // inside the free window already; not up for curator judgment
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

// prunerSystemPrompt builds the curator's instructions for one mode.
//
// The mode section is injected rather than branched on at read time so there
// is exactly one prompt, with one hole in it. The invariant sections — the
// answer format and the never-park list — are outside the hole on purpose:
// a mode may move the threshold for "still needed", never the safety rules.
func prunerSystemPrompt(mode PruneMode) string {
	return fmt.Sprintf(prunerPromptTemplate, modeInstruction(mode))
}

// modeInstruction is the one paragraph that differs per mode. An unknown
// mode takes moderate — validate() rejects those at load time, so reaching
// here with one means an embedder set the field directly, and a working
// default beats a panic mid-turn.
func modeInstruction(mode PruneMode) string {
	switch mode {
	case PruneLow:
		return `MODE: LOW. Keep almost everything. Park a block only when it is
unambiguously dead weight — a superseded file read, a search whose answer is
already written down elsewhere, output the agent has visibly finished with.
If you find yourself building an argument for why a block could be parked,
that block stays. Answering {"park":[]} is the expected outcome here.`

	case PruneExtreme:
		return `MODE: EXTREME. Context is the binding constraint. Park every block
that is not actively carrying the current direction forward. A block earns its
place by being something the agent still needs in front of it — not by being
interesting, not by being recent-ish, not by being potentially relevant later.
"The agent could conceivably want this again" is not a reason to keep it; the
agent can re-read a file. Work down the log and park unless you can name what
the block is still doing.`

	default:
		return `MODE: MODERATE. Park what is clearly no longer in play: file reads
the agent has moved past, exploration that led nowhere, tool output whose
conclusion is already captured in later blocks. Keep anything still plausibly
load-bearing for where the agent is headed. When a block is genuinely
borderline, keep it.`
	}
}

const prunerPromptTemplate = `You keep an agent's working memory small.

You are shown the agent's task and a numbered log of what has happened. The
most recent part of the conversation is not shown to you — it's kept
automatically, for free, so you never need to protect it and its absence
here is not a sign anything is wrong. Everything you do see is already old
enough to be a real candidate. Decide which of it the agent no longer needs
in order to finish the task.

%s

Answer with one JSON object and nothing that matters after it:

{"park":[3,7,9]}

Block ids are the integers in the labels (m7 is 7). {"park":[]} means keep
everything, and is always an available answer.

Never park, in any mode:
- a user message
- the most recent assistant message
- a block holding an unresolved error or a failing test
- a block naming a file the agent has edited or is editing
- a block the agent will need to quote when it answers

Parking is one-way: the agent cannot get a parked block back.`
