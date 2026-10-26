package axon

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// The scout: every user message starts with the free explorer pool, before
// the deciding model's first request. See ROLES.md.

// scoutMinWords is the shortest message worth scouting. Shorter ones are
// acknowledgements and short commands ("push it", "thanks").
const scoutMinWords = 4

// scoutQuestion is what the scout is asked: the request, with the last few
// messages so a follow-up such as "do the same in the API" can be resolved.
func scoutQuestion(s *Session, request string) string {
	var earlier []string
	answer := ""
	// The new message is already in the log; look before it.
	for i := len(s.Messages) - 2; i >= 0 && len(earlier) < 3; i-- {
		m := s.Messages[i]
		switch {
		case m.Role == "user":
			earlier = append([]string{oneLine(m.Content, 300)}, earlier...)
		case m.Role == "assistant" && answer == "" && len(m.ToolCalls) == 0 && strings.TrimSpace(m.Content) != "" && !looksLikeCardEcho(m.Content):
			answer = oneLine(m.Content, 600)
		}
	}
	var b strings.Builder
	b.WriteString("An engineer is about to act on this request:\n«" + strings.TrimSpace(request) + "»\n")
	if len(earlier) > 0 {
		b.WriteString("\nEarlier messages in the conversation, oldest first:\n")
		for _, e := range earlier {
			b.WriteString("- " + e + "\n")
		}
	}
	if answer != "" {
		b.WriteString("\nThe engineer's last answer: " + answer + "\n")
	}
	b.WriteString("\nFind what they need to act: the files and functions the request is about, how that code behaves now, " +
		"and how the project builds or tests that part (the exact command). If the request is not about the code, report nothing.")
	return b.String()
}

// scout runs the explorer pool on a new request and records its brief as an
// explore result, so the deciding model's first request has it. It records
// nothing when the scout is off, the message is too short, or every
// explorer fails. It returns an error only when the turn was interrupted.
func (a *Agent) scout(ctx context.Context, request string) error {
	if len(a.explorers) == 0 || a.settings.Explorer.NoScout || !hasTool(a.tools, toolExplore) ||
		len(strings.Fields(request)) < scoutMinWords {
		return nil
	}
	question := scoutQuestion(a.session, request)
	raw, _ := json.Marshal(map[string]any{"question": question})
	tc := ToolCall{ID: fmt.Sprintf("scout-%d", time.Now().UnixNano()), Type: "function"}
	tc.Function.Name, tc.Function.Arguments = toolExplore, string(raw)

	scoutCtx, cancel := context.WithCancel(ctx)
	cf := context.CancelFunc(cancel)
	a.turnCancel.Store(&cf)
	defer func() { a.turnCancel.Store(nil); cancel() }()

	a.emit(ctx, Event{Kind: KindInfo, Text: "scout: the explorer is looking into the request before the main model starts"})
	a.emit(ctx, Event{Kind: KindToolCall, Tool: &ToolEvent{ID: tc.ID, Name: toolExplore, Args: raw}})
	out, err := a.explore(scoutCtx, question, nil)
	if scoutCtx.Err() != nil {
		a.emit(ctx, Event{Kind: KindToolError, Tool: &ToolEvent{ID: tc.ID, Name: toolExplore}, Err: scoutCtx.Err()})
		return ErrInterrupted
	}
	if err != nil {
		a.emit(ctx, Event{Kind: KindToolError, Tool: &ToolEvent{ID: tc.ID, Name: toolExplore}, Err: err})
		a.emit(ctx, Event{Kind: KindInfo, Text: "scout skipped: " + oneLine(err.Error(), 200)})
		return nil
	}

	// Recorded as an explore call the runtime made, so it is an ordinary
	// block: shown, checkpointed and recalled like any other.
	a.session.Append(Msg{Role: "assistant", ToolCalls: []ToolCall{tc}})
	a.session.Append(Msg{Role: "tool", ToolCallID: tc.ID, ToolName: toolExplore,
		Content: "scout, before your first reply: the explorer looked into this request.\n" + out})
	last := &a.session.Messages[len(a.session.Messages)-1]
	if last.ID != "" {
		last.Content = "[#" + last.ID + "]\n" + last.Content
	}
	if err := a.session.Save(); err != nil {
		a.emit(ctx, Event{Kind: KindError, Err: err, Text: "session not persisted"})
	}
	a.emit(ctx, Event{Kind: KindToolResult, Tool: &ToolEvent{ID: tc.ID, Name: toolExplore, Result: last.Content, BlockID: last.ID}})
	return nil
}
