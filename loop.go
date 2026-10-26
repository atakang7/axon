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
	var gather gatherTracker
	var delegate delegateTracker
	a.nudge = ""
	a.turnNote = ""
	goalNotes, echoNotes := 0, 0

	for iteration := 1; ; iteration++ {
		// Checked before the model call, not after, so the cap is a bound on
		// requests actually issued rather than one-past.
		if a.maxIterations > 0 && iteration > a.maxIterations {
			a.emit(ctx, Event{Kind: KindTurnEnd})
			return StepResult{
				Assistant: assistantText,
				ToolCalls: calls,
				Turn:      a.session.Turn,
			}, fmt.Errorf("%w: %d", ErrMaxIterations, a.maxIterations)
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

		if msg.Content != "" {
			assistantText = msg.Content
			a.emit(ctx, Event{Kind: KindAssistantEnd, Text: msg.Content})
		}

		if len(msg.ToolCalls) == 0 {
			// The contract is checked before a turn may end: an unproven goal
			// gets one more request naming what is open, and a second one
			// only if a criterion is outright failed or stale. Two at most,
			// so an uncheckable criterion cannot trap the turn.
			open := a.session.openCriteria()
			if len(open) > 0 && !a.settings.Goals.DisableNudges && (goalNotes == 0 || (goalNotes == 1 && hasBrokenCriterion(open))) {
				goalNotes++
				a.turnNote = a.session.goalTurnNote()
				if goalNotes == 2 {
					a.turnNote = brokenGoalNote(open)
				}
				a.turnCancel.Store(nil)
				cancel()
				a.emit(ctx, Event{Kind: KindInfo, Text: "goal not proven yet: " + strings.Join(open, ", ")})
				continue
			}
			if len(open) > 0 {
				a.emit(ctx, Event{Kind: KindInfo, Text: "turn ended with the goal unproven: " + strings.Join(open, ", ")})
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

		if len(a.explorers) > 0 && delegate.observe(msg.ToolCalls) {
			a.nudge += delegateNote(delegate.rounds)
			a.emit(ctx, Event{Kind: KindInfo, Text: "the model has spent several replies locating things itself; suggesting explore"})
		}
		if rounds := gather.observe(msg.ToolCalls); rounds > 0 {
			a.nudge += gatherNudge(rounds)
			a.emit(ctx, Event{Kind: KindInfo, Text: "context gathered one item per round; asking the model to plan and batch its next lookups"})
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

// cardEchoNote answers a reply that copied a runtime card instead of acting.
const cardEchoNote = "[runtime note: your last reply copied a runtime summary instead of acting. Runtime notes describe work that already happened; writing one does nothing. To read, run or change anything, call the tools through the tool-calling API. If the request is already answered, answer in your own words.]"
