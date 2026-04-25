package main

// memory.go — context-cost management.
//
// =============================================================================
// DESIGN RATIONALE
// =============================================================================
//
// The agent is a single LLM. Every turn the entire conversation is resent to
// the provider, so an unbounded message history multiplies dollar cost
// linearly with context length. Aggressive forgetting is therefore not a
// nicety; it is the primary cost-control mechanism in this system.
//
// We considered three alternative architectures and rejected each:
//
//   (a) Subagent investigators with isolated context. Sounds cheap because the
//       child runs on a small context, but the parent must brief it precisely
//       — and writing that brief costs about as much as just doing the work.
//       Net: complexity without savings.
//
//   (b) Tier-based archive (short_term / long_term). Forces the LLM to
//       pre-classify importance, which is exactly the judgment it cannot make
//       reliably without seeing how the task unfolds. Tiering creates
//       ambiguity ("which one applies here?") and the LLM hedges by always
//       picking the safer/longer tier.
//
//   (c) Pure prompt-level cost guidance. Tried previously; the LLM ignores
//       polite instructions when context pressure is invisible.
//
// What we ship instead is a single uniform mechanism: every block has a TTL
// that decrements each turn. When TTL hits zero the block is auto-parked.
// The LLM's only judgments are the ones it is actually equipped to make:
// "do I still need this?" (refresh) and "this is noise, remove it"
// (park or forget).
//
// =============================================================================
// STATE MODEL
// =============================================================================
//
// A block — meaning a single Msg in the session — is in exactly one of three
// states. Transitions are deliberate; there is no automatic promotion between
// the recoverable and irrecoverable layers.
//
//   active
//     Full content lives in the message stream sent to the model. Carries a
//     TTL counter that decrements each turn via DecayBlocks.
//
//   parked
//     The active message stream now shows a one-line breadcrumb in this
//     block's slot: "[#m12 parked | reason: <r> | gist: <s>]". The original
//     content is preserved off-stream in Session.ParkedBlocks so that Recall
//     can restore it. Parked is the recoverable layer.
//
//     A block becomes parked either:
//       - automatically, when its TTL reaches zero during DecayBlocks; or
//       - explicitly, when the LLM calls the park tool.
//
//   deleted
//     Hidden from the agent's reachable memory. ContextMessages emits a
//     tombstone instead of the original content, and Session.ParkedBlocks no
//     longer stores the block. The original Msg remains in Session.Messages,
//     so the session JSON still preserves the historical fact that this block
//     existed. Deletion is therefore irreversible for the agent while still
//     preserving auditability for humans.
//
// We deliberately do NOT auto-delete on long disuse. Letting the LLM lose
// content silently would re-introduce the very ambiguity we removed by
// dropping tiers. Deletion is always an explicit, reasoned act.
//
// =============================================================================
// THE GAMIFICATION LOOP (TTL as a UI mechanism)
// =============================================================================
//
// TTL is not a garbage-collection knob; it is a signal we surface to the LLM
// every turn. The decay-status block injected at turn start lists each
// active block's remaining TTL. When the LLM sees "#m8 TTL=1 EXPIRES NEXT
// TURN" it has three responses, each of which is the right answer in a
// different situation:
//
//   - refresh #m8         → "I still need this, keep it active"
//   - park #m8            → "I have what I need, leave a breadcrumb"
//   - forget #m8          → "this was noise, remove it entirely"
//   - (do nothing)        → "I trust the auto-park; this can quietly fade"
//
// The pressure is real because the consequence is real: the next turn the
// block either is or is not in the prompt. Prompt-level guidance ("please
// archive aggressively") is invisible; TTL on the screen is not.
//
// =============================================================================
// WHAT THIS FILE DOES NOT DO
// =============================================================================
//
//   - It does not enforce quality. Summaries can be bad, reasons can be vague.
//     The LLM is the quality layer; tools record and execute.
//   - It does not gate destructive actions with rules ("must contain the word
//     irrelevant"). The reason field plus a system-message-protection floor
//     is the entirety of the safety. Nagging gates were considered and
//     dropped; they encourage the LLM to game the gate rather than think.
//   - It does not auto-summarize. If the LLM parks without a summary, the
//     summary is empty. We do not silently pretend understanding the agent
//     did not provide.
//
// =============================================================================

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// -----------------------------------------------------------------------------
// Types and constants
// -----------------------------------------------------------------------------

// ParkedBlock is the side-table record for a parked Msg. Self-describing:
// holds the metadata the LLM authored (summary, reason) AND the exact
// breadcrumb string that will be substituted into the active context for
// this block. We compute the breadcrumb at park time and store it here so
// that ContextMessages — the function that builds the prompt — has only
// to look up "what should I emit for parked block X" without re-running
// any formatting logic. One source of truth per concern.
type ParkedBlock struct {
	ID              string    `json:"id"`
	Role            string    `json:"role"`
	OriginalContent string    `json:"original_content"`
	Summary         string    `json:"summary"`
	Reason          string    `json:"reason"`
	Breadcrumb      string    `json:"breadcrumb"`
	ParkedAt        time.Time `json:"parked_at"`
	ParkedAtTurn    int       `json:"parked_at_turn"`
}

// defaultActiveTTL is the TTL assigned to a freshly appended Msg. Tuned to
// give the LLM a few turns of grace before pressure starts. Configurable.
const defaultActiveTTL = 5

// defaultParkTTL is unused by the runtime — parked blocks have no TTL —
// but we keep the symbol for symmetry if a future feature wants it.
const defaultParkTTL = 0

// -----------------------------------------------------------------------------
// Active message stream
// -----------------------------------------------------------------------------

// ContextMessages builds the slice of Msg sent to the model on the next call.
//
// GOLDEN RULE: Session.Messages is an immutable historical log. We never
// mutate stored Msgs to reflect park/forget state. Instead, we DERIVE the
// LLM-visible context here at emission time:
//
//   - active block      → emit Content as-is.
//   - parked block      → emit the stored breadcrumb from ParkedBlocks[ID].
//   - forgotten block   → emit the tombstone marker (forgotten blocks; see Forget).
//
// This keeps the chat history pure: Messages always reflects what actually
// happened in chronological order. Park, forget, and recall are projections
// over that log, not destructive edits to it. Reconstruction of any prior
// turn's context is therefore deterministic.
//
// Internal bookkeeping (ID, TTL, park metadata) is stripped — the provider
// sees only role + content + tool-call fields.
func (s *Session) ContextMessages() []Msg {
	s.ensure()
	out := make([]Msg, 0, len(s.Messages))
	for _, m := range s.Messages {
		content := m.Content
		if m.Forgotten {
			content = tombstone(m.ID, m.ForgetReason)
		} else if m.Parked {
			if rec, ok := s.ParkedBlocks[m.ID]; ok && rec.Breadcrumb != "" {
				content = rec.Breadcrumb
			} else {
				// Defensive: parked flag set but no record. Emit a minimal
				// breadcrumb from inline metadata rather than leak the original.
				content = breadcrumb(m.ID, m.ParkReason, m.ParkSummary)
			}
		}
		out = append(out, Msg{
			Role:       m.Role,
			Content:    content,
			ToolCalls:  m.ToolCalls,
			ToolCallID: m.ToolCallID,
			ToolName:   m.ToolName,
		})
	}
	// Task and decay-status blocks are transient: derived at emission time,
	// never stored in Session.Messages. Appending at the TAIL keeps the prefix
	// stable across turns so prompt caching hits on everything before them.
	// Task block comes first — it is the objective anchor; decay status follows
	// as the memory pressure signal. Both are only meaningful for the next
	// model decision; after that turn they are irrelevant.
	if tb := s.TaskBlock(); tb != "" {
		out = append(out, Msg{Role: "system", Content: tb})
	}
	out = append(out, Msg{Role: "system", Content: s.DecayStatusBlock()})
	return out
}

// breadcrumb is the one-line in-context replacement for a parked block.
// Surfaces id, reason, and summary so the LLM remembers what was set aside
// and why — recall is one tool call away if the reason turns out wrong.
func breadcrumb(id, reason, summary string) string {
	return fmt.Sprintf("[#%s parked | reason: %s | gist: %s]", id, reason, summary)
}

func tombstone(id, reason string) string {
	return fmt.Sprintf("[#%s forgotten | reason: %s]", id, reason)
}

// -----------------------------------------------------------------------------
// DecayBlocks — the per-turn tick
// -----------------------------------------------------------------------------

// DecayBlocks runs at the start of every user turn. It performs two jobs:
//
//  1. Decrement TTL on every active, non-system block.
//  2. Auto-park any block whose TTL reaches zero, using a generated reason
//     ("auto-parked: ttl expired") so the breadcrumb still carries a story.
//     The summary is empty for auto-park; the LLM did not author one. This
//     is deliberate — we surface honesty about the absence of a summary
//     rather than fabricate one.
//
// System messages never decay. They carry the prompt and protocol; their
// presence is non-negotiable.
func (s *Session) DecayBlocks() {
	s.ensure()
	s.Turn++
	for i := range s.Messages {
		m := &s.Messages[i]
		if m.Role == "system" || m.Parked {
			continue
		}
		if m.TTL <= 0 {
			// Already at zero from a prior turn but somehow still active —
			// auto-park it now to keep the invariant clean.
			s.autoPark(m)
			continue
		}
		m.TTL--
		if m.TTL == 0 {
			s.autoPark(m)
		}
	}
}

// autoPark flips a block to parked state when its TTL expires. We do NOT
// mutate m.Content — Messages is the immutable log; ContextMessages will
// substitute the stored breadcrumb at emission time.
func (s *Session) autoPark(m *Msg) {
	if m.Content == "" {
		return
	}
	const reason = "auto-parked: ttl expired"
	const summary = "(no summary — auto-parked)"
	bc := breadcrumb(m.ID, reason, summary)
	s.ParkedBlocks[m.ID] = ParkedBlock{
		ID:              m.ID,
		Role:            m.Role,
		OriginalContent: m.Content,
		Summary:         summary,
		Reason:          reason,
		Breadcrumb:      bc,
		ParkedAt:        time.Now(),
		ParkedAtTurn:    s.Turn,
	}
	m.Parked = true
	m.ParkSummary = summary
	m.ParkReason = reason
	m.Forgotten = false
	m.ForgetReason = ""
	m.TTL = 0
}

// -----------------------------------------------------------------------------
// Park — explicit, reasoned move from active to parked
// -----------------------------------------------------------------------------

// Park records a model-authored summary and reason and marks the block as
// parked. Idempotent: re-parking an already-parked block
// updates its summary and reason but does not overwrite OriginalContent.
//
// Returns an error for system messages (cannot be parked) and for unknown ids.
func (s *Session) Park(id, summary, reason string) error {
	s.ensure()
	summary = strings.TrimSpace(summary)
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return fmt.Errorf("reason is required to park a block")
	}
	for i := range s.Messages {
		m := &s.Messages[i]
		if m.ID != id {
			continue
		}
		if m.Role == "system" {
			return fmt.Errorf("cannot park system message %s", id)
		}
		if m.Parked {
			// Update metadata in both places, leave OriginalContent alone.
			rec := s.ParkedBlocks[id]
			rec.Summary = summary
			rec.Reason = reason
			rec.Breadcrumb = breadcrumb(id, reason, summary)
			s.ParkedBlocks[id] = rec
		} else {
			if m.Content == "" {
				return fmt.Errorf("block %s has no content to park", id)
			}
			s.ParkedBlocks[id] = ParkedBlock{
				ID:              id,
				Role:            m.Role,
				OriginalContent: m.Content,
				Summary:         summary,
				Reason:          reason,
				Breadcrumb:      breadcrumb(id, reason, summary),
				ParkedAt:        time.Now(),
				ParkedAtTurn:    s.Turn,
			}
		}
		m.Parked = true
		m.ParkSummary = summary
		m.ParkReason = reason
		m.Forgotten = false
		m.ForgetReason = ""
		m.TTL = 0
		return nil
	}
	return fmt.Errorf("block %s not found", id)
}

// -----------------------------------------------------------------------------
// Recall — parked back to active
// -----------------------------------------------------------------------------

// Recall is a pure retrieval primitive. It returns the parked records the
// caller asked for, by exact id match and/or by substring query against
// id+summary+reason+content. No limit: the LLM is the quality layer.
//
// Recall does NOT mutate state — it only reads. The LLM uses the returned
// content directly in its next reasoning; if it wants the block back in the
// active stream as a true active message it would have to re-park-then-Recall
// which is not currently a flow. Today, recall is "show me what was parked",
// not "make it active again." This keeps the protocol simple; promotion
// back to active can be added later if it turns out the LLM needs it.
func (s *Session) Recall(ids []string, query string) []ParkedBlock {
	s.ensure()
	seen := map[string]bool{}
	var out []ParkedBlock

	for _, id := range ids {
		r, ok := s.ParkedBlocks[id]
		if !ok || seen[id] {
			continue
		}
		out = append(out, r)
		seen[id] = true
	}

	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return out
	}
	for _, r := range s.ParkedBlocks {
		if seen[r.ID] {
			continue
		}
		hay := strings.ToLower(r.ID + "\n" + r.Summary + "\n" + r.Reason + "\n" + r.OriginalContent)
		if strings.Contains(hay, q) {
			out = append(out, r)
		}
	}
	return out
}

// -----------------------------------------------------------------------------
// Forget — destructive removal
// -----------------------------------------------------------------------------

// Forget hides a block from the agent's reachable memory while preserving the
// original Msg in Session.Messages for auditability. ContextMessages emits a
// tombstone for forgotten blocks. The block is removed from ParkedBlocks so
// Recall can no longer restore it.
//
// System messages are protected. User messages are not, deliberately: in a
// future iteration we plan to compile the user's query into a programmatic
// "core constraints" object and forget the raw user message, so we leave
// the path open today.
func (s *Session) Forget(id, reason string) error {
	s.ensure()
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return fmt.Errorf("reason is required to forget a block")
	}
	for i := range s.Messages {
		m := &s.Messages[i]
		if m.ID != id {
			continue
		}
		if m.Role == "system" {
			return fmt.Errorf("cannot forget system message %s", id)
		}
		delete(s.ParkedBlocks, id)
		m.Parked = false
		m.ParkSummary = ""
		m.ParkReason = ""
		m.Forgotten = true
		m.ForgetReason = reason
		m.TTL = 0
		return nil
	}
	if _, ok := s.ParkedBlocks[id]; ok {
		delete(s.ParkedBlocks, id)
		return nil
	}
	return fmt.Errorf("block %s not found", id)
}

// -----------------------------------------------------------------------------
// Refresh — bump TTL on an active block
// -----------------------------------------------------------------------------

// Refresh sets a block's TTL back up. The LLM uses this to keep load-bearing
// active context from expiring. Only works on active blocks; refreshing a
// parked block is a no-op (use Recall instead to read it).
func (s *Session) Refresh(id string, newTTL int) error {
	s.ensure()
	if newTTL <= 0 {
		newTTL = defaultActiveTTL
	}
	for i := range s.Messages {
		m := &s.Messages[i]
		if m.ID != id {
			continue
		}
		if m.Parked {
			return fmt.Errorf("block %s is parked; use recall to read it", id)
		}
		if m.Role == "system" {
			return fmt.Errorf("cannot refresh system message %s", id)
		}
		m.TTL = newTTL
		return nil
	}
	return fmt.Errorf("block %s not found", id)
}

// -----------------------------------------------------------------------------
// Decay status — the gamification surface
// -----------------------------------------------------------------------------

// DecayStatusBlock builds the per-turn status string injected into the
// model's context. Lists active blocks with their TTLs (highlighting ones
// that expire next turn) and the count of parked blocks recallable by id or
// query. This is the screen the LLM looks at when deciding whether to
// refresh, park, or forget.
//
// Sorted by TTL ascending so the most-pressured blocks appear first.
func (s *Session) DecayStatusBlock() string {
	s.ensure()

	type row struct {
		id, role string
		ttl      int
	}
	var active []row
	for _, m := range s.Messages {
		if m.Role == "system" || m.Parked || m.Forgotten || m.ID == "" {
			continue
		}
		active = append(active, row{m.ID, m.Role, m.TTL})
	}
	sort.Slice(active, func(i, j int) bool { return active[i].ttl < active[j].ttl })

	var parkedIDs []string
	for id := range s.ParkedBlocks {
		parkedIDs = append(parkedIDs, id)
	}
	sort.Strings(parkedIDs)

	var b strings.Builder
	fmt.Fprintf(&b, "[memory — turn %d | active: %d | parked: %d — dispose what you no longer need]\n",
		s.Turn, len(active), len(parkedIDs))

	// One line per block: ID, role, TTL pressure. No preview — the LLM already
	// read these blocks; it does not need a re-summary. The job of this list is
	// to give handles for park/forget calls, not to re-display content.
	for _, r := range active {
		pressure := ""
		if r.ttl == 1 {
			pressure = " !"
		} else if r.ttl == 0 {
			pressure = " !!"
		}
		fmt.Fprintf(&b, "  #%s %s ttl=%d%s\n", r.id, r.role, r.ttl, pressure)
	}

	if len(parkedIDs) > 0 {
		fmt.Fprintf(&b, "  parked: %s\n", strings.Join(parkedIDs, " "))
	}

	b.WriteString("  park <id> | forget <id> | refresh <id> | recall <id|query>")
	return b.String()
}

// -----------------------------------------------------------------------------
// TASK tool — register or update the current objective
// -----------------------------------------------------------------------------

// TaskTool sets the session's current task. It is the LLM's commitment to
// what it is accomplishing and how it knows when it is done. One task at a
// time; calling again overwrites the previous task entirely.
//
// The task block appears transiently on the dashboard every turn, just before
// the decay-status block, so the LLM re-binds to the objective each turn
// without holding it in active message context.
//
// When to call:
//   - Once, at the start of non-trivial multi-round work.
//   - When the objective shifts materially (user changes direction, scope expands).
//
// When NOT to call:
//   - Every round to update current_focus. That wastes a round trip per step.
//     The task block on the dashboard already shows the registered state; the LLM
//     reads it and knows where it is without re-calling.
//   - One-shot requests that finish in a single answer.
const taskDescription = `Register your current task. Call once at the start of non-trivial multi-round work. Do NOT call every round — that wastes a full round trip per step. Update only when the objective materially shifts. The task block appears on your dashboard automatically every turn after registration.`

func TaskTool(s *Session) Tool {
	type input struct {
		Objective        string `json:"objective"`
		DefinitionOfDone string `json:"definition_of_done"`
		CurrentFocus     string `json:"current_focus"`
		Reason           string `json:"reason"`
	}
	return Tool{
		Name:        toolTask,
		Description: taskDescription,
		Schema: obj("object", props{
			"objective":          strSchema("Precise statement of what is being accomplished. One sentence. Not a plan — the goal."),
			"definition_of_done": strSchema("The observable condition under which this task is complete. Concrete and checkable."),
			"current_focus":      strSchema("What you are working on right now, this round. Updated each round to reflect the current step."),
			"reason":             reasonField(),
		}, []string{"objective", "definition_of_done", "current_focus", "reason"}),
		Fn: func(raw json.RawMessage) (string, error) {
			var p input
			if err := json.Unmarshal(raw, &p); err != nil {
				return "", err
			}
			if err := requireReason(p.Reason); err != nil {
				return "", err
			}
			s.CurrentTask = &Task{
				Objective:        strings.TrimSpace(p.Objective),
				DefinitionOfDone: strings.TrimSpace(p.DefinitionOfDone),
				CurrentFocus:     strings.TrimSpace(p.CurrentFocus),
			}
			return fmt.Sprintf("task registered: %s", s.CurrentTask.Objective), s.Save()
		},
	}
}

// -----------------------------------------------------------------------------
// PARK tool — explicit park with model-authored summary and reason
// -----------------------------------------------------------------------------

const parkDescription = `Park a block: replace its content in the active stream with a one-line breadcrumb (id + reason + gist). The original content is preserved and can be restored via the recall tool. Use park when you have extracted what you needed from a block but want a recoverable trace.

Call park at the END of a turn after producing your final answer; parked breadcrumbs are how future turns will see this block.`

func ParkTool(s *Session) Tool {
	type block struct {
		ID      string `json:"id"`
		Summary string `json:"summary"`
		Reason  string `json:"reason"`
	}
	type input struct {
		Blocks   []block `json:"blocks"`
		NextStep string  `json:"next_step"`
	}
	return Tool{
		Name:        toolPark,
		Description: parkDescription,
		Schema: obj("object", props{
			"next_step": nextStepField(),
			"blocks": arr(obj("object", props{
				"id":      strSchema("Block id, e.g. m12."),
				"summary": strSchema("One-line gist of what was here. The only trace of content future turns will see — make it specific."),
				"reason":  strSchema("Why this block is being parked. Required."),
			}, []string{"id", "summary", "reason"})),
		}, []string{"blocks"}),
		Fn: func(raw json.RawMessage) (string, error) {
			var p input
			if err := json.Unmarshal(raw, &p); err != nil {
				return "", err
			}
			if len(p.Blocks) == 0 {
				return "", fmt.Errorf("blocks are required")
			}
			var lines []string
			for _, b := range p.Blocks {
				if err := s.Park(b.ID, b.Summary, b.Reason); err != nil {
					return "", err
				}
				lines = append(lines, fmt.Sprintf("%s parked: %q", b.ID, b.Summary))
			}
			return strings.Join(lines, "\n"), s.Save()
		},
	}
}

// -----------------------------------------------------------------------------
// RECALL tool — restore parked content into the response, by id or query
// -----------------------------------------------------------------------------

const recallDescription = `Recall parked blocks. Provide ids (exact match) or query (substring across id, summary, reason, content), or both. Returns full original content. Recalling often signals you parked too aggressively — prefer to refresh load-bearing blocks rather than park-then-recall.`

func RecallTool(s *Session) Tool {
	type input struct {
		IDs      []string `json:"ids"`
		Query    string   `json:"query"`
		Reason   string   `json:"reason"`
		NextStep string   `json:"next_step"`
	}
	return Tool{
		Name:        toolRecall,
		Description: recallDescription,
		Schema: obj("object", props{
			"next_step": nextStepField(),
			"ids":       arr(strSchema("Parked block id, e.g. m12.")),
			"query":     strSchema("Substring matched against id, summary, reason, original content."),
			"reason":    reasonField(),
		}, []string{"reason"}),
		Fn: func(raw json.RawMessage) (string, error) {
			var p input
			if err := json.Unmarshal(raw, &p); err != nil {
				return "", err
			}
			if err := requireReason(p.Reason); err != nil {
				return "", err
			}
			if len(p.IDs) == 0 && strings.TrimSpace(p.Query) == "" {
				return "", fmt.Errorf("provide ids or query (or both)")
			}
			recs := s.Recall(p.IDs, p.Query)
			if len(recs) == 0 {
				return "no parked blocks matched", nil
			}
			var b strings.Builder
			for i, r := range recs {
				if i > 0 {
					b.WriteString("\n\n")
				}
				fmt.Fprintf(&b, "== %s [%s] (parked turn %d) ==\n", r.ID, r.Role, r.ParkedAtTurn)
				if r.Reason != "" {
					b.WriteString("reason: " + r.Reason + "\n")
				}
				if r.Summary != "" {
					b.WriteString("summary: " + r.Summary + "\n")
				}
				b.WriteString(r.OriginalContent)
			}
			return b.String(), nil
		},
	}
}

// -----------------------------------------------------------------------------
// FORGET tool — irreversible removal
// -----------------------------------------------------------------------------

const forgetDescription = `Forget a block: remove it from the active stream AND from parked storage. Cannot be recalled. Use only for blocks you are certain have zero future value to the task — pure noise, redundant tool output, etc. Reasoning is required and recorded; the session log preserves the audit trail even though the agent can no longer reach the content.`

func ForgetTool(s *Session) Tool {
	type input struct {
		IDs      []string `json:"ids"`
		Reason   string   `json:"reason"`
		NextStep string   `json:"next_step"`
	}
	return Tool{
		Name:        toolForget,
		Description: forgetDescription,
		Schema: obj("object", props{
			"next_step": nextStepField(),
			"ids":       arr(strSchema("Block id to forget. System messages are protected.")),
			"reason":    reasonField(),
		}, []string{"ids", "reason"}),
		Fn: func(raw json.RawMessage) (string, error) {
			var p input
			if err := json.Unmarshal(raw, &p); err != nil {
				return "", err
			}
			if err := requireReason(p.Reason); err != nil {
				return "", err
			}
			if len(p.IDs) == 0 {
				return "", fmt.Errorf("ids are required")
			}
			var lines []string
			for _, id := range p.IDs {
				if err := s.Forget(id, p.Reason); err != nil {
					return "", err
				}
				lines = append(lines, fmt.Sprintf("%s forgotten", id))
			}
			return strings.Join(lines, "\n"), s.Save()
		},
	}
}

// -----------------------------------------------------------------------------
// REFRESH tool — keep an active block from expiring
// -----------------------------------------------------------------------------

const refreshDescription = `Refresh a block: reset its TTL so it does not auto-park this turn. Use when the decay status shows a block expiring that you still need. If you find yourself refreshing the same block repeatedly, that block is load-bearing context — consider whether the surrounding turns have what they need to make it stop being load-bearing.`

func RefreshTool(s *Session) Tool {
	type input struct {
		IDs      []string `json:"ids"`
		TTL      int      `json:"ttl"`
		Reason   string   `json:"reason"`
		NextStep string   `json:"next_step"`
	}
	return Tool{
		Name:        toolRefresh,
		Description: refreshDescription,
		Schema: obj("object", props{
			"next_step": nextStepField(),
			"ids":       arr(strSchema("Active block id to refresh.")),
			"ttl":       intSchema(fmt.Sprintf("New TTL. Default %d if omitted.", defaultActiveTTL)),
			"reason":    reasonField(),
		}, []string{"ids", "reason"}),
		Fn: func(raw json.RawMessage) (string, error) {
			var p input
			if err := json.Unmarshal(raw, &p); err != nil {
				return "", err
			}
			if err := requireReason(p.Reason); err != nil {
				return "", err
			}
			if len(p.IDs) == 0 {
				return "", fmt.Errorf("ids are required")
			}
			ttl := p.TTL
			if ttl <= 0 {
				ttl = defaultActiveTTL
			}
			var lines []string
			for _, id := range p.IDs {
				if err := s.Refresh(id, ttl); err != nil {
					return "", err
				}
				lines = append(lines, fmt.Sprintf("%s refreshed → ttl=%d", id, ttl))
			}
			return strings.Join(lines, "\n"), s.Save()
		},
	}
}
