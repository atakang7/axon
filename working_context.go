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
	// Summary is the background note a pruner model wrote after the run
	// closed. Optional: the card is complete without it.
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
//   - A result repeated later by the identical call (any run).
//   - In closed runs, a read of a file that was written afterwards: the disk
//     holds the current text and the agent re-reads before editing.
//
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
		eligible := i < len(s.Messages)-tailKeep && m.ID != "" && !m.Parked && !failures[m.ID] && !m.ToolFailed
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
				written[argString(u.args, "path")] = m.ID
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
	content := argString(args, "content")
	args["content"] = fmt.Sprintf("[%d characters written to %s; the file on disk is the source of truth]", len([]rune(content)), argString(args, "path"))
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

// runCard is the deterministic stand-in for a folded run. It is built from
// the run's own messages, so it cannot invent anything; a background note
// adds judgement on top when one is ready.
func runCard(s *Session, r run, uses map[string]toolUse) string {
	var read, wrote, ran, reverted []string
	seen := map[string]bool{}
	add := func(list *[]string, key, v string) {
		if v != "" && !seen[key+v] {
			seen[key+v] = true
			*list = append(*list, v)
		}
	}
	failed := map[string]string{} // command -> block id of its latest failure
	first, last, answer := "", "", ""
	for _, m := range s.Messages[r.start+1 : r.end] {
		if m.ID != "" {
			if first == "" {
				first = m.ID
			}
			last = m.ID
		}
		switch m.Role {
		case "assistant":
			if len(m.ToolCalls) == 0 && strings.TrimSpace(m.Content) != "" {
				answer = m.Content
			}
		case "system":
			if strings.Contains(m.Content, "reverted") {
				reverted = append(reverted, strings.Trim(m.Content, "[]"))
			}
		case "tool":
			u := uses[m.ToolCallID]
			switch u.name {
			case toolRead:
				for _, p := range readPaths(u) {
					add(&read, "r", p)
				}
			case toolWrite:
				if !m.ToolFailed {
					add(&wrote, "w", argString(u.args, "path"))
				}
			case toolExec:
				cmd := oneLine(argString(u.args, "command"), 80)
				if m.ToolFailed {
					failed[cmd] = m.ID
				} else {
					delete(failed, cmd)
				}
				add(&ran, "x", cmd)
			}
		}
	}
	var b strings.Builder
	if first != "" {
		fmt.Fprintf(&b, "[finished run #%s..#%s, folded; recall any id for the original text]", first, last)
	} else {
		b.WriteString("[finished run, folded]")
	}
	line := func(label string, items []string, max int) {
		if len(items) == 0 {
			return
		}
		more := ""
		if len(items) > max {
			more = fmt.Sprintf(" (+%d more)", len(items)-max)
			items = items[:max]
		}
		fmt.Fprintf(&b, "\n%s: %s%s", label, strings.Join(items, ", "), more)
	}
	line("read", read, 12)
	line("wrote", wrote, 12)
	line("ran", ran, 6)
	var fails []string
	for cmd, id := range failed {
		fails = append(fails, fmt.Sprintf("%s (#%s)", cmd, id))
	}
	sort.Strings(fails)
	line("still failing at the end", fails, 4)
	line("reverted", reverted, 4)
	if n := s.runNote(r); n != nil && n.Summary != "" {
		b.WriteString("\nsummary: " + n.Summary)
	}
	if answer != "" {
		b.WriteString("\nfinal answer: " + truncateRunes(answer, 700))
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
// folded run or replaced by a stale stub. Reviewers have nothing to decide
// about them.
func (s *Session) compactedIDs() map[string]bool {
	ids := map[string]bool{}
	for _, r := range closedRuns(s) {
		if !s.runFolded(r) {
			continue
		}
		for _, m := range s.Messages[r.start+1 : r.end] {
			if m.ID != "" {
				ids[m.ID] = true
			}
		}
	}
	for i := range staleResults(s) {
		ids[s.Messages[i].ID] = true
	}
	return ids
}
