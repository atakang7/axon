package axon

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Working context: see WORKING_CONTEXT.md. Everything here derives a smaller
// view of an immutable log; nothing rewrites Session.Messages.

// tailKeep is how many of the newest messages are always shown as they are.
// The model is answering from them right now.
const tailKeep = 6

// RunNote is the persisted state of one run, keyed by its user message ID.
type RunNote struct {
	// Folded runs render as the user's message plus a run card.
	Folded bool `json:"folded,omitempty"`
	// Summary is a note written by the background run notes removed in v2.7.
	// Sessions that have one still show it; nothing writes it any more.
	Summary string `json:"summary,omitempty"`
}

// run is one user message and the agent work that answered it, as the
// half-open index range [start, end) of Session.Messages.
type run struct{ start, end int }

func sessionRuns(s *Session) []run {
	var runs []run
	for i, m := range s.Messages {
		if m.Role != "user" {
			continue
		}
		if n := len(runs); n > 0 {
			runs[n-1].end = i
		}
		runs = append(runs, run{start: i, end: len(s.Messages)})
	}
	return runs
}

// closedRuns are the runs a later user message has finished. They never
// change again, which is what makes folding and noting them safe at any time.
func closedRuns(s *Session) []run {
	runs := sessionRuns(s)
	if len(runs) == 0 {
		return nil
	}
	return runs[:len(runs)-1]
}

func (s *Session) runNote(r run) *RunNote {
	id := s.Messages[r.start].ID
	if id == "" || s.Runs == nil {
		return nil
	}
	return s.Runs[id]
}

func (s *Session) runFolded(r run) bool {
	n := s.runNote(r)
	return n != nil && n.Folded
}

// foldRun marks a closed run as folded. Folding is monotonic: nothing unfolds
// a run, so the provider's cached prompt prefix stays stable.
func (s *Session) foldRun(r run) bool {
	id := s.Messages[r.start].ID
	if id == "" || r.end >= len(s.Messages) || s.runFolded(r) {
		return false
	}
	if s.Runs == nil {
		s.Runs = map[string]*RunNote{}
	}
	if s.Runs[id] == nil {
		s.Runs[id] = &RunNote{}
	}
	s.Runs[id].Folded = true
	return true
}

// ---------------------------------------------------------------------------
// Stale cache entries
// ---------------------------------------------------------------------------

type toolUse struct {
	name string
	args map[string]any
	raw  string
}

func toolUses(s *Session) map[string]toolUse {
	uses := map[string]toolUse{}
	for _, m := range s.Messages {
		for _, tc := range m.ToolCalls {
			var args map[string]any
			_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
			uses[tc.ID] = toolUse{name: tc.Function.Name, args: args, raw: tc.Function.Arguments}
		}
	}
	return uses
}

func argString(args map[string]any, key string) string {
	v, _ := args[key].(string)
	return v
}

// readPaths lists the files one read call covered.
func readPaths(u toolUse) []string {
	if p := argString(u.args, "path"); p != "" {
		return []string{p}
	}
	files, _ := u.args["files"].([]any)
	var out []string
	for _, f := range files {
		if obj, ok := f.(map[string]any); ok {
			if p := argString(obj, "path"); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

// writePaths lists the files one write call changed: its path, and the path
// of each of its edits (an edit without one changes the call's path).
func writePaths(u toolUse) []string {
	top := argString(u.args, "path")
	edits, _ := u.args["edits"].([]any)
	if len(edits) == 0 {
		return []string{top}
	}
	var out []string
	seen := map[string]bool{}
	for _, e := range edits {
		p := top
		if obj, ok := e.(map[string]any); ok && argString(obj, "path") != "" {
			p = argString(obj, "path")
		}
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

// repeatKey identifies a call whose newer repetition makes an older result
// redundant: the same single-file read with the same range, or the same
// command or search run again.
func repeatKey(u toolUse) string {
	switch u.name {
	case toolRead:
		if argString(u.args, "path") == "" {
			return ""
		}
		return "read\x00" + canonicalJSON(u.args)
	case toolExec:
		if b, _ := u.args["run_in_background"].(bool); b {
			return ""
		}
		return "exec\x00" + argString(u.args, "command") + "\x00" + argString(u.args, "dir")
	case toolSearch:
		return "search\x00" + canonicalJSON(u.args)
	}
	return ""
}

func canonicalJSON(v map[string]any) string {
	b, _ := json.Marshal(v) // map keys are sorted
	return string(b)
}

// staleResults maps message index to the stub that replaces a tool result
// whose content a newer message has made redundant. Only the result is
// replaced; its call stays, so call/result pairing is untouched.
//
//   - In closed runs, a result repeated later by the identical call.
//   - In closed runs, a read of a file that was written afterwards: the disk
//     holds the current text and the agent re-reads before editing.
//
// The open run is never touched: its requests share one growing prefix, and
// the provider bills a cached prefix at a fraction of the price. Stubbing an
// older result mid-run changed that prefix on every repeated command, so each
// request after it paid the whole context again. Budget pressure in the open
// run is the checkpoint's job, which rewrites once, not on every request.
// The tail, unresolved failures and parked blocks are left alone.
func staleResults(s *Session) map[int]string {
	uses := toolUses(s)
	failures := unresolvedToolFailures(s)
	openStart := len(s.Messages)
	if runs := sessionRuns(s); len(runs) > 0 {
		openStart = runs[len(runs)-1].start
	}
	stale := map[int]string{}
	newer := map[string]string{}   // repeatKey -> id of the newest result
	written := map[string]string{} // path -> id of the newest write after this point
	for i := len(s.Messages) - 1; i >= 0; i-- {
		m := s.Messages[i]
		if m.Role != "tool" {
			continue
		}
		u, ok := uses[m.ToolCallID]
		if !ok {
			continue
		}
		eligible := i < openStart && i < len(s.Messages)-tailKeep && m.ID != "" && !m.Parked && !failures[m.ID] && !m.ToolFailed
		if key := repeatKey(u); key != "" {
			if id, seen := newer[key]; seen {
				if eligible {
					stale[i] = fmt.Sprintf("[#%s superseded by #%s, the same %s run again; recall %s for this output]", m.ID, id, u.name, m.ID)
					continue
				}
			} else if !m.ToolFailed && m.ID != "" {
				newer[key] = m.ID
			}
		}
		switch u.name {
		case toolWrite:
			if !m.ToolFailed {
				for _, p := range writePaths(u) {
					written[p] = m.ID
				}
			}
		case toolRead:
			if !eligible || i >= openStart {
				continue
			}
			paths := readPaths(u)
			var by []string
			for _, p := range paths {
				if id, ok := written[p]; ok {
					by = append(by, id)
				}
			}
			if len(paths) > 0 && len(by) == len(paths) {
				stale[i] = fmt.Sprintf("[#%s read %s; edited later (#%s), so the disk has the current text. recall %s for this old copy]",
					m.ID, strings.Join(paths, ", "), strings.Join(by, ", #"), m.ID)
			}
		}
	}
	return stale
}

// stubWriteArgs drops the full file body from a write call in a closed run.
// The file on disk is the record; keeping its body in every later request is
// paying for the same bytes twice.
func stubWriteArgs(tc ToolCall) ToolCall {
	if tc.Function.Name != toolWrite || len(tc.Function.Arguments) < 600 {
		return tc
	}
	var args map[string]any
	if json.Unmarshal([]byte(tc.Function.Arguments), &args) != nil {
		return tc
	}
	if edits, ok := args["edits"].([]any); ok {
		for _, e := range edits {
			if m, ok := e.(map[string]any); ok {
				for _, key := range []string{"content", "old_str"} {
					if v := argString(m, key); len(v) > 200 {
						m[key] = fmt.Sprintf("[%d characters]", len([]rune(v)))
					}
				}
			}
		}
	}
	if _, ok := args["content"]; ok {
		content := argString(args, "content")
		args["content"] = fmt.Sprintf("[%d characters written to %s; the file on disk is the source of truth]", len([]rune(content)), argString(args, "path"))
	}
	if old := argString(args, "old_str"); len(old) > 200 {
		args["old_str"] = fmt.Sprintf("[%d characters replaced]", len([]rune(old)))
	}
	b, err := json.Marshal(args)
	if err != nil {
		return tc
	}
	tc.Function.Arguments = string(b)
	return tc
}

// ---------------------------------------------------------------------------
// Run cards
// ---------------------------------------------------------------------------

// Finished work is shown once, as runtime notes, never as a repeated shape.
//
// Two failures shaped this. Cards rendered as assistant turns were copied by
// the model instead of calling tools: "[finished run #m241..#m242, folded]
// read: ... ran: ...", every reply, nothing executed (seen 2026-10-26 with a
// free model). And any structure that repeats — one card per run, one stub
// per dropped result — is a template the model's attention is pulled toward.
// So a folded run keeps only what was said (the request and the final
// answer), all folded runs share one history note, the folded steps of a
// request share one note, and results out of context share one index.

// looksLikeCardEcho reports whether an assistant reply copies a runtime note
// instead of saying or doing anything.
func looksLikeCardEcho(text string) bool {
	t := strings.TrimSpace(text)
	return strings.HasPrefix(t, "[finished ") || strings.HasPrefix(t, "[runtime note") ||
		strings.Contains(t, "folded; recall any id for the original text]")
}

// work is what a stretch of the log did, read from the log itself, so it
// cannot invent anything.
type work struct {
	first, last                                string
	read, wrote, ran, failing, reverted, notes []string
	// answer is the last thing the agent said; a reply that only echoed a
	// runtime note is not one.
	answer string
}

// summarizeWork reads the messages in [from, to).
func summarizeWork(s *Session, from, to int, uses map[string]toolUse) work {
	var w work
	seen := map[string]bool{}
	add := func(list *[]string, key, v string) {
		if v != "" && !seen[key+v] {
			seen[key+v] = true
			*list = append(*list, v)
		}
	}
	failed := map[string]string{} // command -> block id of its latest failure
	for _, m := range s.Messages[from:to] {
		if t := s.Trims[m.ID]; t != nil && t.Note != "" {
			w.notes = append(w.notes, fmt.Sprintf("#%s %s", m.ID, t.Note))
		}
		if m.ID != "" {
			if w.first == "" {
				w.first = m.ID
			}
			w.last = m.ID
		}
		switch m.Role {
		case "assistant":
			if len(m.ToolCalls) == 0 && strings.TrimSpace(m.Content) != "" && !looksLikeCardEcho(m.Content) {
				w.answer = m.Content
			}
		case "system":
			if strings.Contains(m.Content, "reverted") {
				w.reverted = append(w.reverted, strings.Trim(m.Content, "[]"))
			}
		case "tool":
			u := uses[m.ToolCallID]
			switch u.name {
			case toolRead:
				for _, p := range readPaths(u) {
					add(&w.read, "r", p)
				}
			case toolWrite:
				if !m.ToolFailed {
					for _, p := range writePaths(u) {
						add(&w.wrote, "w", p)
					}
				}
			case toolExec:
				cmd := oneLine(argString(u.args, "command"), 80)
				if m.ToolFailed {
					failed[cmd] = m.ID
				} else {
					delete(failed, cmd)
				}
				add(&w.ran, "x", cmd)
			}
		}
	}
	for cmd, id := range failed {
		w.failing = append(w.failing, fmt.Sprintf("%s (#%s)", cmd, id))
	}
	sort.Strings(w.failing)
	return w
}

// line renders the work as one line of a note: what was written, run, still
// failing; reads only as a count of files.
func (w work) line() string {
	list := func(items []string, max int) string {
		if len(items) > max {
			return strings.Join(items[:max], ", ") + fmt.Sprintf(" (+%d more)", len(items)-max)
		}
		return strings.Join(items, ", ")
	}
	var parts []string
	if len(w.read) > 0 {
		parts = append(parts, fmt.Sprintf("read %d file(s)", len(w.read)))
	}
	if len(w.wrote) > 0 {
		parts = append(parts, "wrote "+list(w.wrote, 8))
	}
	if len(w.ran) > 0 {
		parts = append(parts, "ran "+list(w.ran, 4))
	}
	if len(w.failing) > 0 {
		parts = append(parts, "still failing: "+list(w.failing, 3))
	}
	if len(w.reverted) > 0 {
		parts = append(parts, "reverted: "+list(w.reverted, 3))
	}
	if len(w.notes) > 0 {
		parts = append(parts, "notes: "+list(w.notes, 4))
	}
	if len(parts) == 0 {
		return "no tool work"
	}
	return strings.Join(parts, "; ")
}

func (w work) span() string {
	if w.first == "" {
		return ""
	}
	return "#" + w.first + ".." + w.last
}

// historyNote is the one note for every folded request, in order. Each line
// names the request by its id; the request itself and the final answer stay
// in the conversation above it.
func historyNote(s *Session, runs []run, uses map[string]toolUse) string {
	var b strings.Builder
	b.WriteString("[runtime note: work done on the requests above, folded; recall an id for the original]")
	for _, r := range runs {
		w := summarizeWork(s, r.start+1, r.end, uses)
		fmt.Fprintf(&b, "\n#%s %s: %s", s.Messages[r.start].ID, w.span(), w.line())
		if n := s.runNote(r); n != nil && n.Summary != "" {
			b.WriteString("; summary: " + oneLine(n.Summary, 300))
		}
	}
	return b.String()
}

// stepsNote is the one note for the folded steps of a request.
func stepsNote(s *Session, steps []step, uses map[string]toolUse) string {
	var b strings.Builder
	b.WriteString("[runtime note: earlier steps of this request, folded; recall an id for the original]")
	for _, st := range steps {
		w := summarizeWork(s, st.start, st.end, uses)
		title := "step"
		if st.title != "" {
			title = "step \"" + oneLine(st.title, 80) + "\""
		}
		fmt.Fprintf(&b, "\n%s %s: %s", title, w.span(), w.line())
		if w.answer != "" {
			b.WriteString("; you said: " + oneLine(w.answer, 200))
		}
	}
	return b.String()
}

func oneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	return truncateRunes(s, max)
}

func truncateRunes(s string, max int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= max {
		return string(r)
	}
	return string(r[:max]) + "…"
}

// compactedIDs are blocks the view already shows in compact form: inside a
// folded run or step, or replaced by a stale stub. Reviewers have nothing to
// decide about them.
func (s *Session) compactedIDs() map[string]bool {
	ids := map[string]bool{}
	mark := func(from, to int) {
		for _, m := range s.Messages[from:to] {
			if m.ID != "" {
				ids[m.ID] = true
			}
		}
	}
	var uses map[string]toolUse
	for _, r := range sessionRuns(s) {
		if s.runFolded(r) {
			mark(r.start+1, r.end)
			continue
		}
		if len(s.Steps) == 0 {
			continue
		}
		if uses == nil {
			uses = toolUses(s)
		}
		for _, st := range runSteps(s, r, uses) {
			if s.stepFolded(st) {
				mark(st.start, st.end)
			}
		}
	}
	for i := range staleResults(s) {
		ids[s.Messages[i].ID] = true
	}
	return ids
}

// ---------------------------------------------------------------------------
// Steps: the goal tree below a run
// ---------------------------------------------------------------------------

// step is the stretch of a run between milestones: successful task advance or
// replan results, and compact checkpoints. closer is the ID of the result that
// closed it; the active step has none.
type step struct {
	start, end int
	closer     string
	title      string
}

// runSteps derives a run's steps from the log. The step titles come from the
// task register/replan arguments, so nothing new has to be stored.
func runSteps(s *Session, r run, uses map[string]toolUse) []step {
	var steps []step
	var plan []string
	k, segStart := 0, r.start+1
	for i := r.start + 1; i < r.end; i++ {
		m := s.Messages[i]
		if m.Role != "tool" || m.ToolFailed || m.ID == "" {
			continue
		}
		u := uses[m.ToolCallID]
		action := argString(u.args, "action")
		title := ""
		if k < len(plan) {
			title = plan[k]
		}
		closes := false
		switch {
		case u.name == toolTask && action == "register":
			plan, k = stringList(u.args["steps"]), 0
		case u.name == toolTask && action == "advance":
			closes = true
			k++
		case u.name == toolTask && action == "replan":
			closes = true
			plan, k = stringList(u.args["steps"]), 0
		case u.name == "compact":
			closes, title = true, "checkpoint: "+oneLine(argString(u.args, "state"), 80)
		}
		if !closes {
			continue
		}
		end := i + 1
		for end < r.end && s.Messages[end].Role == "tool" {
			end++ // the rest of the same tool round belongs to the closed step
		}
		steps = append(steps, step{start: segStart, end: end, closer: m.ID, title: title})
		segStart, i = end, end-1
	}
	title := ""
	if k < len(plan) {
		title = plan[k]
	}
	return append(steps, step{start: segStart, end: r.end, title: title})
}

func stringList(v any) []string {
	items, _ := v.([]any)
	var out []string
	for _, it := range items {
		if s, ok := it.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return out
}

func (s *Session) stepFolded(st step) bool {
	return st.closer != "" && s.Steps[st.closer]
}

// foldStep marks a finished step as folded; monotonic like run folding.
func (s *Session) foldStep(st step) bool {
	if st.closer == "" || s.Steps[st.closer] {
		return false
	}
	if s.Steps == nil {
		s.Steps = map[string]bool{}
	}
	s.Steps[st.closer] = true
	return true
}

// openRunSteps are the steps of the run the agent is working on now.
func openRunSteps(s *Session, uses map[string]toolUse) []step {
	runs := sessionRuns(s)
	if len(runs) == 0 {
		return nil
	}
	return runSteps(s, runs[len(runs)-1], uses)
}

// ---------------------------------------------------------------------------
// Trims: per-block line selections
// ---------------------------------------------------------------------------

// Trim is a checkpoint's line selection for one block. Spans are 1-based
// inclusive line ranges of the block's text. Note and Kept were written by
// the enricher removed in v2.7; sessions that carry them still render.
type Trim struct {
	Spans [][2]int `json:"spans,omitempty"`
	Note  string   `json:"note,omitempty"`
	Kept  bool     `json:"kept,omitempty"`
}

// blockText is a tool result without the "[#mN]" label the loop prepends.
func blockText(m Msg) string {
	return strings.TrimPrefix(m.Content, "[#"+m.ID+"]\n")
}

func blockLines(m Msg) []string {
	return strings.Split(blockText(m), "\n")
}

func renderTrim(m Msg, t *Trim) string {
	switch {
	case t == nil || t.Kept:
		return m.Content
	case t.Note != "":
		return fmt.Sprintf("[#%s note: %s | recall %s for the original]", m.ID, t.Note, m.ID)
	}
	lines := blockLines(m)
	var b strings.Builder
	kept := 0
	for _, sp := range t.Spans {
		if sp[0] < 1 || sp[1] > len(lines) || sp[0] > sp[1] {
			continue
		}
		if kept > 0 {
			b.WriteString("\n…\n")
		}
		b.WriteString(strings.Join(lines[sp[0]-1:sp[1]], "\n"))
		kept += sp[1] - sp[0] + 1
	}
	return fmt.Sprintf("[#%s trimmed: %d of %d lines kept; recall %s for the rest]\n%s", m.ID, kept, len(lines), m.ID, b.String())
}
