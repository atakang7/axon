package axon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Step submits one user message and drives the loop until the
// assistant emits text with no further tool calls.
func (a *Agent) Step(ctx context.Context, userInput string) (StepResult, error) {
	if userInput == "" {
		return StepResult{}, fmt.Errorf("agent: empty input")
	}
	// A new user message moves the focus: the working set a checkpoint kept
	// for the previous request no longer raises the trigger.
	a.keptTokens = 0
	a.exploreFailed.Store(false)

	a.session.Turn++
	a.emit(ctx, Event{Kind: KindTurnStart})
	if a.session.Turn == 1 && a.session.Title == "" {
		a.session.Title = a.deriveTitleWithModel(ctx, userInput)
	}
	a.session.Append(Msg{Role: "user", Content: userInput})
	if err := a.session.Save(); err != nil {
		return StepResult{}, fmt.Errorf("agent: save session: %w", err)
	}

	a.emit(ctx, Event{Kind: KindUserInput, Text: userInput})

	var calls []ToolCall
	var assistantText string
	a.nudge = ""
	a.turnNote = ""
	verifyNotes, echoNotes, textCallNotes := 0, 0, 0

	for iteration := 1; ; iteration++ {
		// Checked before the model call, not after, so the cap is a bound on
		// requests actually issued rather than one-past.
		if a.maxIterations > 0 && iteration > a.maxIterations {
			return a.reportAtLimit(ctx, calls)
		}
		// The model knows its budget before it runs out, so it can leave
		// the work in a state worth handing over.
		if left := a.maxIterations - iteration + 1; a.maxIterations > 0 && left <= budgetWarnCalls {
			note := fmt.Sprintf(budgetNote, left)
			if a.turnNote != "" {
				note = a.turnNote + "\n\n" + note
			}
			a.turnNote = note
		}

		turnCtx, cancel := context.WithCancel(ctx)
		cf := context.CancelFunc(cancel)
		a.turnCancel.Store(&cf)

		msg, err := a.chat(turnCtx, a.tools)
		a.nudge = ""
		a.turnNote = ""
		if err != nil {
			a.turnCancel.Store(nil)
			cancel()
			if errors.Is(err, context.Canceled) {
				return StepResult{Turn: a.session.Turn, ToolCalls: calls}, ErrInterrupted
			}
			return StepResult{Turn: a.session.Turn, ToolCalls: calls}, err
		}

		a.session.Append(*msg)
		if err := a.session.Save(); err != nil {
			a.emit(ctx, Event{Kind: KindError, Err: err, Text: "session not persisted"})
		}

		// A reply that copies a runtime card is neither an answer nor an
		// action: the model took the card for a way to work. Ask once more
		// with a note; a second copy ends the turn as it is.
		if len(msg.ToolCalls) == 0 && looksLikeCardEcho(msg.Content) && echoNotes == 0 {
			echoNotes++
			a.turnNote = cardEchoNote
			a.turnCancel.Store(nil)
			cancel()
			a.emit(ctx, Event{Kind: KindInfo, Text: "the model copied a runtime note instead of acting; asking it to use the tools"})
			continue
		}

		// A tool call written as text ran nothing; it is not an answer.
		if len(msg.ToolCalls) == 0 && textCallNotes < 2 && looksLikeTextToolCall(msg.Content) {
			textCallNotes++
			a.turnNote = textCallNote
			a.turnCancel.Store(nil)
			cancel()
			a.emit(ctx, Event{Kind: KindInfo, Text: "the model wrote a tool call as text instead of calling it; asking it to use the tools"})
			continue
		}

		if msg.Content != "" {
			assistantText = msg.Content
			a.emit(ctx, Event{Kind: KindAssistantEnd, Text: msg.Content})
		}

		if len(msg.ToolCalls) == 0 {
			// A turn that changed files ends after something checked them,
			// or after one note asking for that. Once per turn, so a change
			// that cannot be checked here cannot trap the turn.
			v := a.session.verification()
			if note := v.note(); note != "" && verifyNotes == 0 && !a.settings.Goals.DisableNudges {
				verifyNotes++
				a.turnNote = note
				a.turnCancel.Store(nil)
				cancel()
				a.emit(ctx, Event{Kind: KindInfo, Text: "change not checked yet (" + v.state + "); asking for a check"})
				continue
			}
			if v.wrote && v.state != "verified" {
				a.emit(ctx, Event{Kind: KindInfo, Text: "turn ended with the change " + v.state})
			}
			a.turnCancel.Store(nil)
			cancel()
			a.emit(ctx, Event{Kind: KindTurnEnd})
			return StepResult{
				Assistant: assistantText,
				ToolCalls: calls,
				Turn:      a.session.Turn,
			}, nil
		}

		// Consecutive read-only calls run concurrently; see toolGroups.
		for _, group := range toolGroups(msg.ToolCalls) {
			if turnCtx.Err() != nil {
				break
			}

			for _, tc := range group {
				calls = append(calls, tc)
				a.emit(ctx, Event{Kind: KindToolCall, Tool: &ToolEvent{
					ID: tc.ID, Name: tc.Function.Name, Args: json.RawMessage(tc.Function.Arguments),
				}})
			}

			for i, result := range a.runToolGroup(turnCtx, group) {
				tc := group[i]
				result = a.dedupRead(result)
				a.session.Append(result)

				blockID := a.session.Messages[len(a.session.Messages)-1].ID
				if blockID != "" {
					a.session.Messages[len(a.session.Messages)-1].Content = "[#" + blockID + "]\n" + result.Content
				}

				if err := a.session.Save(); err != nil {
					a.emit(ctx, Event{Kind: KindError, Err: err, Text: "session not persisted"})
				}

				a.emit(ctx, Event{Kind: KindToolResult, Tool: &ToolEvent{
					ID: tc.ID, Name: tc.Function.Name, Result: result.Content, BlockID: blockID,
				}})
			}
		}

		a.turnCancel.Store(nil)
		interrupted := turnCtx.Err() != nil
		cancel()

		if interrupted {
			a.emit(ctx, Event{Kind: KindTurnEnd})
			return StepResult{
				Assistant: assistantText,
				ToolCalls: calls,
				Turn:      a.session.Turn,
			}, ErrInterrupted
		}
	}
}

// Run drives the loop using input until it returns false or ctx is
// cancelled. Each line becomes one Step.
func (a *Agent) Run(ctx context.Context, input InputFunc) error {
	for {
		line, ok := input()
		if !ok {
			return nil
		}

		if _, err := a.Step(ctx, line); err != nil {
			if errors.Is(err, ErrInterrupted) {
				continue
			}
			return err
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
}

// budgetWarnCalls is how many model calls before the limit the model is told.
const budgetWarnCalls = 10

const budgetNote = "[runtime note: %d model calls left in this request. Finish the step you are on, run its check, and commit or save finished work. Leave the rest for the next request and say exactly where you stopped.]"

const limitNote = "[runtime note: this request has reached its step limit. Do not call tools. Report now, in the user's language: what is done and how it was checked, what is not done, and the exact next step.]"

// reportAtLimit ends a request that reached its step limit with a report,
// not an error: one more call, told not to use tools. Calls it makes anyway
// are not run and not kept, so the log stays valid; without text, the
// runtime's own plan and check state are the report.
func (a *Agent) reportAtLimit(ctx context.Context, calls []ToolCall) (StepResult, error) {
	a.emit(ctx, Event{Kind: KindInfo, Text: fmt.Sprintf("step limit reached (%d model calls); asking for a report", a.maxIterations)})
	a.turnNote = limitNote
	msg, err := a.chat(ctx, a.tools)
	a.turnNote = ""
	if errors.Is(err, context.Canceled) {
		return StepResult{Turn: a.session.Turn, ToolCalls: calls}, ErrInterrupted
	}
	text := ""
	if err == nil {
		text = msg.Content
	}
	if strings.TrimSpace(text) == "" {
		text = fmt.Sprintf("Stopped at the step limit (%d model calls) before the work was finished.", a.maxIterations)
		if st := a.session.stateBlock(); st != "" {
			text += "\n\n" + st
		}
	}
	a.session.Append(Msg{Role: "assistant", Content: text})
	if err := a.session.Save(); err != nil {
		a.emit(ctx, Event{Kind: KindError, Err: err, Text: "session not persisted"})
	}
	a.emit(ctx, Event{Kind: KindAssistantEnd, Text: text})
	a.emit(ctx, Event{Kind: KindTurnEnd})
	return StepResult{Assistant: text, ToolCalls: calls, Turn: a.session.Turn}, nil
}

// cardEchoNote answers a reply that copied a runtime card instead of acting.
const cardEchoNote = "[runtime note: your last reply copied a runtime summary instead of acting. Runtime notes describe work that already happened; writing one does nothing. To read, run or change anything, call the tools through the tool-calling API. If the request is already answered, answer in your own words.]"
