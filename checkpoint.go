package axon

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Checkpoints: the deciding model chooses, in one tool call, which older tool
// results stay in context. See WORKING_CONTEXT.md.

const toolCheckpoint = "checkpoint"

// minCheckpointBytes is the smallest result worth deciding about: below it,
// the stub that would replace a block is about as long as the block.
const minCheckpointBytes = 400

// maxCheckpointNote caps one note; a note is a line, not a summary.
const maxCheckpointNote = 300

// CheckpointRecord is one applied checkpoint, kept for audit and display.
type CheckpointRecord struct {
	// At is the newest block id when the checkpoint was taken.
	At     string `json:"at"`
	Forced bool   `json:"forced,omitempty"`
	// Before and After are estimated context tokens.
	Before  int      `json:"before"`
	After   int      `json:"after"`
	Kept    []string `json:"kept,omitempty"`
	Trimmed []string `json:"trimmed,omitempty"`
	Dropped []string `json:"dropped,omitempty"`
}

const checkpointDescription = `Choose what stays in your context. Every older tool result you do not keep is replaced by a one-line stub naming its id (your note, if you give one); recall <id> brings back the original.
- keep {id}: the whole result stays. Use it for what you will still edit, quote exactly or compare against.
- keep {id, lines}: only those lines stay, e.g. "120-180, 300-310". For read results the numbers are the file line numbers shown in the result; for a read of several files, also give path. For other results they count the result's lines from 1.
- notes {id, note}: for a result you are not keeping, one line with what you learned from it and where (file:line). Without a note the stub names the call.
User messages, your newest messages and failures that have not been fixed always stay. Call it when a step is done and its material is no longer needed, or when the runtime asks.`

// CheckpointTool is bound to the agent: applying a checkpoint changes what
// every later request contains.
func (a *Agent) checkpointTool() Tool {
	return Tool{
		Name:        toolCheckpoint,
		Description: checkpointDescription,
		Schema: obj("object", props{
			"keep": arr(obj("object", props{
				"id":    strSchema("Block id, e.g. m12."),
				"lines": strSchema("Optional line ranges to keep, e.g. \"40-85, 120-140\"."),
				"path":  strSchema("Optional: for a read of several files, the file the lines belong to."),
			}, []string{"id"})),
			"notes": arr(obj("object", props{
				"id":   strSchema("Block id of a result you are not keeping."),
				"note": strSchema("One line: what you learned from it, with file:line where it applies."),
			}, []string{"id", "note"})),
		}, nil),
		Fn: func(ctx context.Context, raw json.RawMessage) (string, error) {
			if err := ctx.Err(); err != nil {
				return "", err
			}
			var args checkpointArgs
			if err := json.Unmarshal(raw, &args); err != nil {
				return "", fmt.Errorf("checkpoint arguments: %w", err)
			}
			candidates := checkpointCandidates(a.session)
			if len(candidates) == 0 {
				return "Nothing to decide: every older result is already compact or must stay.", nil
			}
			before := a.viewTokens()
			a.emit(ctx, Event{Kind: KindPruneStart, Prune: &PruneInfo{Before: before}})
			out := applyCheckpoint(a.session, args, candidates)
			after := a.recordCheckpoint(ctx, out, before, false)
			return out.report(before, after), nil
		},
	}
}

type checkpointArgs struct {
	Keep  []keepEntry `json:"keep"`
	Notes []noteEntry `json:"notes"`
}

type keepEntry struct {
	ID    string `json:"id"`
	Lines string `json:"lines"`
	Path  string `json:"path"`
}

type noteEntry struct {
	ID   string `json:"id"`
	Note string `json:"note"`
}

// The checkpoint call is made by whichever model decides, and models write
// the same intent several ways. A shape that cannot be read as intended fails
// the whole call, so these accept what models were seen to send: {"id":"m12"},
// "m12", "m12: 40-85" for keep; {"id":"m12","note":"x"}, "m12: x" for notes;
// and notes as a {"m12":"x"} object. Ids and lines are still validated
// against the candidates by applyCheckpoint.

var entryPrefix = regexp.MustCompile(`^\s*#?(m\d+)\s*(?:[:\-–—=,]\s*|\s+)?(.*)$`)

func (k *keepEntry) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		g := entryPrefix.FindStringSubmatch(s)
		if g == nil {
			return fmt.Errorf("keep entry %q does not start with a block id", s)
		}
		k.ID, k.Lines = g[1], strings.TrimSpace(g[2])
		return nil
	}
	type plain keepEntry
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	*k = keepEntry(p)
	return nil
}

func (n *noteEntry) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		g := entryPrefix.FindStringSubmatch(s)
		if g == nil {
			return fmt.Errorf("note %q does not start with a block id", s)
		}
		n.ID, n.Note = g[1], strings.TrimSpace(g[2])
		return nil
	}
	type plain noteEntry
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	*n = noteEntry(p)
	return nil
}

func (a *checkpointArgs) UnmarshalJSON(b []byte) error {
	var raw struct {
		Keep  json.RawMessage `json:"keep"`
		Notes json.RawMessage `json:"notes"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*a = checkpointArgs{}
	if len(raw.Keep) > 0 && string(raw.Keep) != "null" {
		var one keepEntry
		if err := json.Unmarshal(raw.Keep, &a.Keep); err != nil {
			if json.Unmarshal(raw.Keep, &one) != nil {
				return fmt.Errorf("keep: %w", err)
			}
			a.Keep = []keepEntry{one}
		}
	}
	if len(raw.Notes) > 0 && string(raw.Notes) != "null" {
		if err := json.Unmarshal(raw.Notes, &a.Notes); err != nil {
			var byID map[string]string
			if json.Unmarshal(raw.Notes, &byID) != nil {
				return fmt.Errorf("notes: %w", err)
			}
			ids := make([]string, 0, len(byID))
			for id := range byID {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			for _, id := range ids {
				a.Notes = append(a.Notes, noteEntry{ID: id, Note: byID[id]})
			}
		}
	}
	return nil
}

// checkpointCandidates are the tool results a checkpoint decides about, in log
// order. Excluded: the newest round (the model may not have used it yet) and
// the last few messages, unresolved failures, anything already folded,
// stubbed or parked, and results too small to matter.
func checkpointCandidates(s *Session) []Msg {
	end := len(s.Messages) - tailKeep
	for i := len(s.Messages) - 1; i >= 0; i-- {
		if s.Messages[i].Role == "assistant" {
			end = min(end, i)
			break
		}
	}
	failures := unresolvedToolFailures(s)
	compacted := s.compactedIDs()
	var out []Msg
	for i := 0; i < end; i++ {
		m := s.Messages[i]
		if m.Role != "tool" || m.ID == "" || m.Parked || failures[m.ID] || compacted[m.ID] {
			continue
		}
		if t := s.Trims[m.ID]; t != nil && t.Note != "" {
			continue
		}
		if len(blockText(m)) < minCheckpointBytes {
			continue
		}
		out = append(out, m)
	}
	return out
}

// checkpointOutcome is what applyCheckpoint did, in block ids.
type checkpointOutcome struct {
	kept, trimmed, dropped []string
	ignored                []string
}

// applyCheckpoint applies a selection to the session. Code, not the model,
// decides validity: an entry that names a block outside the candidates, or
// lines that do not exist, is ignored and reported, and a block whose keep
// entry cannot be applied stays whole. Nothing is ever deleted; parked blocks
// remain in the log for recall.
func applyCheckpoint(s *Session, args checkpointArgs, candidates []Msg) checkpointOutcome {
	var out checkpointOutcome
	byID := map[string]Msg{}
	for _, m := range candidates {
		byID[m.ID] = m
	}
	// A block kept whole by any entry stays whole; line entries for the same
	// block add up.
	keep := map[string]bool{}
	whole := map[string]bool{}
	spans := map[string][][2]int{}
	for _, k := range args.Keep {
		id := normalizeBlockID(k.ID)
		m, ok := byID[id]
		if !ok {
			out.ignored = append(out.ignored, fmt.Sprintf("keep %s: not one of the results this checkpoint decides about", orQuoted(k.ID)))
			continue
		}
		keep[id] = true
		if strings.TrimSpace(k.Lines) == "" {
			whole[id] = true
			continue
		}
		sel, err := selectLines(blockLines(m), k.Lines, k.Path)
		if err != nil {
			out.ignored = append(out.ignored, fmt.Sprintf("keep %s lines %q: %v; kept whole", id, k.Lines, err))
			whole[id] = true
			continue
		}
		spans[id] = mergeSpans(append(spans[id], sel...))
	}
	notes := map[string]string{}
	for _, n := range args.Notes {
		id := normalizeBlockID(n.ID)
		note := oneLine(n.Note, maxCheckpointNote)
		switch {
		case byID[id].ID == "":
			out.ignored = append(out.ignored, fmt.Sprintf("note %s: not one of the results this checkpoint decides about", orQuoted(n.ID)))
		case keep[id]:
			// A kept block needs no stand-in; the note would never show.
		case note == "":
			out.ignored = append(out.ignored, fmt.Sprintf("note %s: empty", id))
		default:
			notes[id] = note
		}
	}

	if s.Trims == nil {
		s.Trims = map[string]*Trim{}
	}
	for _, m := range candidates {
		id := m.ID
		if !keep[id] {
			summary := notes[id]
			if summary == "" {
				summary = gist(s, id)
			}
			if err := s.Park(id, summary, "checkpoint"); err == nil {
				out.dropped = append(out.dropped, id)
			}
			continue
		}
		sel := spans[id]
		if whole[id] || sel == nil || coversAll(sel, len(blockLines(m))) {
			out.kept = append(out.kept, id)
			continue
		}
		s.Trims[id] = &Trim{Spans: sel}
		out.trimmed = append(out.trimmed, id)
	}
	return out
}

func (o checkpointOutcome) report(before, after int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Checkpoint applied: ~%d → ~%d tokens. Kept %d, trimmed %d, dropped %d (recall any id for the original).",
		before, after, len(o.kept), len(o.trimmed), len(o.dropped))
	for _, line := range o.ignored {
		b.WriteString("\nignored: " + line)
	}
	return b.String()
}

// recordCheckpoint persists an applied checkpoint, reports it and makes it the
// floor for the next forced one. It returns the context size after it.
func (a *Agent) recordCheckpoint(ctx context.Context, out checkpointOutcome, before int, forced bool) int {
	after := a.viewTokens()
	at := ""
	if n := len(a.session.Messages); n > 0 {
		at = a.session.Messages[n-1].ID
	}
	a.session.Checkpoints = append(a.session.Checkpoints, CheckpointRecord{
		At: at, Forced: forced, Before: before, After: after,
		Kept: out.kept, Trimmed: out.trimmed, Dropped: out.dropped,
	})
	a.keptTokens = after
	if err := saveSessionAtomic(a.session); err != nil {
		a.emit(ctx, Event{Kind: KindError, Err: err, Text: "session not persisted"})
	}
	a.emit(ctx, Event{Kind: KindPruneEnd, Prune: &PruneInfo{Before: before, After: after, Rejected: out.ignored}})
	return after
}

// checkpointTrigger is the context size that forces a checkpoint now: the
// configured trigger, or, when the last checkpoint of this run kept more than
// that, its size plus the growth allowance. The trigger cuts bloat, never a
// working set the model chose to keep.
func (a *Agent) checkpointTrigger() int {
	trigger := a.settings.Pruner.Trigger()
	if grown := a.keptTokens + a.settings.Pruner.GrowthTokens; a.keptTokens > 0 && grown > trigger {
		return grown
	}
	return trigger
}

// forceCheckpoint asks the deciding model for a checkpoint call, with the
// same context and tools the next request would carry plus one note, so a
// main-model checkpoint is served from the prompt cache. It reports whether a
// checkpoint was applied. Two failed attempts give up; the deterministic
// floor in ensureContextBudget still guarantees progress.
func (a *Agent) forceCheckpoint(ctx context.Context, tools []Tool, before int) bool {
	if !hasTool(tools, toolCheckpoint) {
		return false
	}
	candidates := checkpointCandidates(a.session)
	if len(candidates) == 0 {
		return false
	}
	model := a.prunerModel
	if model == nil {
		model = a.model
	}
	a.emit(ctx, Event{Kind: KindPruneStart, Prune: &PruneInfo{Before: before}})
	callCtx, cancel := context.WithTimeout(ctx, a.settings.Pruner.Timeout.Std())
	defer cancel()

	view := a.session.ContextMessages(a.settings.Pruner.ContextWindow())
	note := checkpointRequest(candidates, before, a.checkpointTrigger())
	var failure string
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			note += "\n\nYour previous reply did not call the checkpoint tool. Reply with exactly one checkpoint tool call and nothing else."
		}
		reply, err := a.complete(callCtx, model, Request{
			Messages:   withTurnNote(view, note),
			Tools:      toolSpecs(tools),
			MaxTokens:  a.settings.Pruner.MaxTokens,
			InputLimit: PrunerInputLimit,
		}, a.recordUsage(ctx), false)
		if err != nil {
			failure = err.Error()
			break
		}
		call := findToolCall(reply, toolCheckpoint)
		if call == nil {
			failure = "the model did not call the checkpoint tool"
			continue
		}
		var args checkpointArgs
		if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
			failure = "checkpoint arguments: " + err.Error()
			continue
		}
		out := applyCheckpoint(a.session, args, candidates)
		a.recordCheckpoint(ctx, out, before, true)
		return true
	}
	if ctx.Err() != nil {
		return false
	}
	a.emit(ctx, Event{Kind: KindInfo, Text: "prune skipped: checkpoint failed (" + failure + "); folding by rule instead"})
	return false
}

// checkpointRequest is the one-shot note that asks for a forced checkpoint.
// It is never saved to the session.
func checkpointRequest(candidates []Msg, size, trigger int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[runtime: checkpoint] Your context is ~%d tokens (checkpoint size %d). Call the checkpoint tool now, and only it, to choose what stays. "+
		"Keep what the remaining work needs; drop the rest with a note where you learned something. Nothing is lost: every dropped result stays recallable by id.\n\nResults to decide about:", size, trigger)
	for _, m := range candidates {
		text := blockText(m)
		fmt.Fprintf(&b, "\n%s  %s — %d lines, ~%d tokens", m.ID, oneLine(firstLine(text), 100), strings.Count(text, "\n")+1, contextTokens(len(m.Content)))
	}
	return b.String()
}

// viewTokens estimates the next request with the agent's current tools.
func (a *Agent) viewTokens() int {
	n, err := requestInputTokens(Request{Messages: a.session.ContextMessages(a.settings.Pruner.ContextWindow()), Tools: toolSpecs(a.tools)}, modelName(a.model))
	if err != nil {
		return 0
	}
	return n
}

func hasTool(tools []Tool, name string) bool {
	for _, t := range tools {
		if t.Name == name {
			return true
		}
	}
	return false
}

func findToolCall(m *Msg, name string) *ToolCall {
	if m == nil {
		return nil
	}
	for i := range m.ToolCalls {
		if m.ToolCalls[i].Function.Name == name {
			return &m.ToolCalls[i]
		}
	}
	return nil
}

func normalizeBlockID(id string) string {
	return strings.TrimPrefix(strings.TrimSpace(id), "#")
}

func orQuoted(s string) string {
	if strings.TrimSpace(s) == "" {
		return `""`
	}
	return strings.TrimSpace(s)
}

// ---------------------------------------------------------------------------
// Line selection
// ---------------------------------------------------------------------------

var (
	gutterLine  = regexp.MustCompile(`^(\d+)\t`)
	sectionLine = regexp.MustCompile(`^=== (.+?)(?: \(from line \d+\))? ===$`)
	rangeSpec   = regexp.MustCompile(`^(\d+)(?:\s*-\s*(\d+))?$`)
)

// selectLines maps a "40-85, 120" spec onto 1-based block line spans. In a
// result with a numbered gutter (a read), numbers are the file line numbers
// shown, and path picks the file in a read of several; otherwise they count
// the block's own lines. Section headers of the selected files are kept so
// the trimmed text still says which file it is.
func selectLines(lines []string, spec, path string) ([][2]int, error) {
	var ranges [][2]int
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		g := rangeSpec.FindStringSubmatch(part)
		if g == nil {
			return nil, fmt.Errorf("%q is not a line or a range", part)
		}
		lo, _ := strconv.Atoi(g[1])
		hi := lo
		if g[2] != "" {
			hi, _ = strconv.Atoi(g[2])
		}
		if lo < 1 || hi < lo {
			return nil, fmt.Errorf("%q is not a valid range", part)
		}
		ranges = append(ranges, [2]int{lo, hi})
	}
	if len(ranges) == 0 {
		return nil, fmt.Errorf("no ranges")
	}
	in := func(n int) bool {
		for _, r := range ranges {
			if n >= r[0] && n <= r[1] {
				return true
			}
		}
		return false
	}

	numbered := false
	for _, l := range lines {
		if gutterLine.MatchString(l) {
			numbered = true
			break
		}
	}
	var picked []int
	if !numbered {
		for i := range lines {
			if in(i + 1) {
				picked = append(picked, i+1)
			}
		}
		if len(picked) == 0 {
			return nil, fmt.Errorf("the result has %d lines", len(lines))
		}
		return toSpans(picked), nil
	}

	path = strings.TrimSpace(path)
	section, header := "", -1
	headerKept := false
	for i, l := range lines {
		if g := sectionLine.FindStringSubmatch(l); g != nil {
			section, header, headerKept = g[1], i, false
			continue
		}
		if path != "" && !samePath(section, path) {
			continue
		}
		g := gutterLine.FindStringSubmatch(l)
		if g == nil {
			continue
		}
		n, _ := strconv.Atoi(g[1])
		if !in(n) {
			continue
		}
		if header >= 0 && !headerKept {
			picked = append(picked, header+1)
			headerKept = true
		}
		picked = append(picked, i+1)
	}
	if len(picked) == 0 {
		if path != "" {
			return nil, fmt.Errorf("no shown line of %s is in that range", path)
		}
		return nil, fmt.Errorf("no shown line number is in that range")
	}
	return toSpans(picked), nil
}

// samePath compares a section label with a path the model gave, tolerating
// "./" and separator differences.
func samePath(label, path string) bool {
	clean := func(p string) string {
		p = strings.ReplaceAll(strings.TrimSpace(p), `\`, "/")
		return strings.TrimPrefix(p, "./")
	}
	l, p := clean(label), clean(path)
	return l == p || strings.HasSuffix(l, "/"+p) || strings.HasSuffix(p, "/"+l)
}

// toSpans turns sorted line numbers into contiguous spans.
func toSpans(lines []int) [][2]int {
	sort.Ints(lines)
	var out [][2]int
	for _, n := range lines {
		if k := len(out); k > 0 && n <= out[k-1][1]+1 {
			out[k-1][1] = max(out[k-1][1], n)
			continue
		}
		out = append(out, [2]int{n, n})
	}
	return out
}

func mergeSpans(spans [][2]int) [][2]int {
	var lines []int
	for _, sp := range spans {
		for n := sp[0]; n <= sp[1]; n++ {
			lines = append(lines, n)
		}
	}
	return toSpans(lines)
}

func coversAll(spans [][2]int, total int) bool {
	n := 0
	for _, sp := range spans {
		n += sp[1] - sp[0] + 1
	}
	return n >= total
}
