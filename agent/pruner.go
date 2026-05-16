package agent

// pruner.go — context curator.
//
// The pruner is a separate small/fast LLM that runs out-of-band, never visible
// to the main agent. Its only job is to look at the conversation log and
// decide which blocks are no longer relevant to where the task is going. The
// runtime then calls Park / Forget on its behalf. The main agent has no
// memory tools and does not know any of this is happening.
//
// Triggering: only when the projected context is large enough that pruning
// pays for itself, AND the context is growing meaningfully (sharp growth is
// the strongest signal of incoming garbage). Below the threshold we do
// nothing — small contexts do not need curation.
//
// Output: tiny JSON. The pruner reasons internally; we only consume IDs.
//
//   {"park":[3,7,9],"forget":[4]}
//
// The pruner has no recall channel back to the main agent — wrong parks are
// unrecoverable. The prompt is therefore biased hard toward keeping. Evidence
// the agent will need to cite when justifying or concluding is untouchable.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Pruner is the curator client. It wraps a separate Client (typically a
// cheap, fast, long-context model) and the policy for when to fire.
type Pruner struct {
	client *Client
}

// NewPruner constructs a Pruner around an existing client. Pass nil to
// disable pruning entirely (the agent loop checks for nil).
func NewPruner(c *Client) *Pruner {
	if c == nil {
		return nil
	}
	// Hard cap on emission. Pruner output should never need more than this;
	// a chatty model that wants to "think" hits the wall and gets cut off
	// before it burns serious tokens. Parser tolerates the cutoff because
	// the JSON arrays close cleanly.
	c.MaxTokens = 256
	return &Pruner{client: c}
}

// approxTokens estimates token count from character length. Good enough for a
// threshold gate; we do not need provider-accurate counts to decide whether
// to fire.
func approxTokens(msgs []Msg) int {
	n := 0
	for _, m := range msgs {
		n += len(m.Content)
		for _, tc := range m.ToolCalls {
			n += len(tc.Function.Arguments) + len(tc.Function.Name)
		}
	}
	return n / 4
}

// pruneTriggerThreshold is the floor below which the pruner never fires.
// Below this size, curation cost exceeds benefit.
const pruneTriggerThreshold = 10000

// pruneGrowthThreshold: only re-fire above the floor when context has grown
// by at least this many tokens since the last successful prune. Without this,
// the pruner would fire every turn once we cross the floor.
const pruneGrowthThreshold = 5000

// ShouldFire returns true when the projected context warrants a pruner pass.
// lastFireTokens is the token count at the previous fire (0 if never).
func (p *Pruner) ShouldFire(s *Session, lastFireTokens int) bool {
	if p == nil {
		return false
	}
	tokens := approxTokens(s.ContextMessages())
	if tokens < pruneTriggerThreshold {
		return false
	}
	if lastFireTokens == 0 {
		return true // first crossing
	}
	return tokens-lastFireTokens >= pruneGrowthThreshold
}

// Prune runs the curator pass. On success it has already applied Park /
// Forget mutations to the session and returns the token count that triggered
// this fire (caller stores it as lastFireTokens). On any failure (network,
// parse, model nonsense) it returns the previous lastFireTokens unchanged
// and an error — the main loop logs and continues without pruning.
func (p *Pruner) Prune(ctx context.Context, s *Session, lastFireTokens int) (int, error) {
	if p == nil {
		return lastFireTokens, nil
	}
	tokens := approxTokens(s.ContextMessages())

	prompt := buildPrunerPrompt(s)
	// Tight output frame: two short committed lines, then JSON. The lines force
	// the model to commit a direction in a structured way before emitting the
	// removal list, instead of rambling in free prose.
	msgs := []Msg{
		{Role: "system", Content: prunerSystemPrompt},
		{Role: "user", Content: prompt},
	}

	// Pruner runs with no tools; we want a JSON object out. Use a generous
	// timeout because correctness matters more than latency here.
	pctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	out, err := p.client.Chat(pctx, msgs, nil, nil, nil)
	if err != nil {
		return lastFireTokens, fmt.Errorf("pruner chat failed: %w", err)
	}
	if out == nil || strings.TrimSpace(out.Content) == "" {
		return lastFireTokens, fmt.Errorf("pruner returned no content")
	}

	drop, err := parsePrunerResponse(out.Content)
	if err != nil {
		return lastFireTokens, fmt.Errorf("pruner response parse: %w", err)
	}

	for _, id := range drop {
		blockID := fmt.Sprintf("m%d", id)
		// Always Park (recoverable), never Forget. The model emits one list;
		// we keep the safety net by going through the parked path.
		summary := autoSummary(s, blockID)
		if err := s.Park(blockID, summary, "pruner: not needed to continue"); err != nil {
			continue
		}
	}
	if err := s.Save(); err != nil {
		return lastFireTokens, err
	}
	return tokens, nil
}

// autoSummary builds a short gist line from a block's content. The pruner
// did not author one — it only emitted the ID — so we synthesize from the
// first line, capped at a small width.
func autoSummary(s *Session, id string) string {
	for _, m := range s.Messages {
		if m.ID != id {
			continue
		}
		first := m.Content
		if i := strings.IndexByte(first, '\n'); i >= 0 {
			first = first[:i]
		}
		first = strings.TrimSpace(first)
		if len(first) > 80 {
			first = first[:77] + "..."
		}
		role := m.Role
		if m.ToolName != "" {
			role = m.ToolName
		}
		if first == "" {
			return fmt.Sprintf("(%s)", role)
		}
		return fmt.Sprintf("%s: %s", role, first)
	}
	return "(pruned by curator)"
}

// parsePrunerResponse extracts {"forget":[...]} from the model output.
// Tolerates surrounding text — finds the first well-formed JSON object.
func parsePrunerResponse(s string) ([]int, error) {
	body := extractJSONObject(s)
	if body == "" {
		return nil, fmt.Errorf("no JSON object found in response")
	}
	var p struct {
		Forget []int `json:"forget"`
	}
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		return nil, err
	}
	return p.Forget, nil
}

// extractJSONObject finds the outermost {...} in s. Naive brace-matching
// that ignores braces inside strings. Adequate for our small expected
// outputs.
func extractJSONObject(s string) string {
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return ""
	}
	depth := 0
	inStr := false
	esc := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if inStr {
			if esc {
				esc = false
				continue
			}
			if c == '\\' {
				esc = true
				continue
			}
			if c == '"' {
				inStr = false
			}
			continue
		}
		if c == '"' {
			inStr = true
			continue
		}
		if c == '{' {
			depth++
		} else if c == '}' {
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}

// buildPrunerPrompt assembles the user message: the task block, the original
// user spec (the first user message of the session), and the labelled log of
// all non-system messages with their IDs.
func buildPrunerPrompt(s *Session) string {
	var b strings.Builder
	b.WriteString("# CURRENT TASK\n")
	if tb := s.TaskBlock(); tb != "" {
		b.WriteString(tb)
		b.WriteString("\n\n")
	} else {
		b.WriteString("(no task registered)\n\n")
	}

	b.WriteString("# CONVERSATION LOG\n")
	b.WriteString("Each block is labelled with its ID. Decide for each: keep silently (omit from output), park (move to `park` array — replaced by a one-line breadcrumb the agent can no longer access), or forget (move to `forget` array — gone entirely). When uncertain: keep.\n\n")

	for _, m := range s.Messages {
		if m.Role == "system" || m.ID == "" {
			continue
		}
		// Already-parked / forgotten blocks should not be reconsidered. The
		// pruner only acts on currently active context.
		if m.Parked || m.Forgotten {
			continue
		}
		role := m.Role
		if m.ToolName != "" {
			role = "tool:" + m.ToolName
		}
		fmt.Fprintf(&b, "[%s | role=%s]\n", m.ID, role)
		content := m.Content
		// Cap each block at a generous width so the pruner sees the shape
		// without paying full bytes. The pruner does not need to re-read
		// every line of a 500-line file dump to decide it can be parked.
		const perBlockCap = 2000
		if len(content) > perBlockCap {
			content = content[:perBlockCap] + fmt.Sprintf("\n...[truncated, %d chars total]", len(m.Content))
		}
		b.WriteString(content)
		b.WriteString("\n\n")
	}

	b.WriteString("# YOUR OUTPUT\n")
	b.WriteString("Three lines:\nDIRECTION: ...\nNEEDED: ...\n{\"forget\":[...]}\n")
	return b.String()
}

// prunerSystemPrompt: tight three-line output frame. The model commits a
// direction and a needed-evidence line, then fires the JSON. The frame is
// narrow enough that there's no room to ramble.
const prunerSystemPrompt = `You decide which memory blocks no longer help the agent continue.

Output exactly three lines, in this order:

DIRECTION: <one short sentence — what the agent is doing next>
NEEDED: <one short sentence — what evidence is required to do it>
{"forget":[<ids>]}

Block ids are integers (m7 → 7). Use [] when nothing should be removed; that is a valid, common output.

A block goes in "forget" only if it does not serve DIRECTION or NEEDED.

Never remove:
- any user message
- the last assistant message
- any block with an unresolved error or failing test
- any block naming a file the agent has edited or is editing
- any block the agent will quote when concluding

Three lines total. No prose before, after, or between. No analysis.`
