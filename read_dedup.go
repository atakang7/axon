package axon

import (
	"fmt"
	"strings"
)

// A model that does not trust its context reads the same text again: a traced
// run on 2026-10-24 read one file eight times in a turn. Each copy is paid on
// every later request. A read whose result is exactly a block the next
// request already shows in full returns a pointer to it instead.

// minDedupBytes is the smallest result worth replacing with a pointer.
const minDedupBytes = 400

// dedupRead replaces a read result that repeats a block still in full view.
func (a *Agent) dedupRead(m Msg) Msg {
	if m.Role != "tool" || m.ToolName != toolRead || m.ToolFailed || len(m.Content) < minDedupBytes {
		return m
	}
	for _, v := range a.session.contextView(a.settings.Pruner.ContextWindow()) {
		if v.Role != "tool" || v.ToolName != toolRead || v.ID == "" {
			continue
		}
		if stripBlockID(v.Content) == m.Content {
			m.Content = fmt.Sprintf("[unchanged: the same text is block #%s, still in your context in full. Use it from there; nothing was added.]", v.ID)
			return m
		}
	}
	return m
}

// stripBlockID removes the "[#m12]\n" prefix the loop puts on results.
func stripBlockID(content string) string {
	if strings.HasPrefix(content, "[#") {
		if i := strings.Index(content, "]\n"); i > 0 && i < 16 {
			return content[i+2:]
		}
	}
	return content
}
