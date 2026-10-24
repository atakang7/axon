package axon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// Step submits one user message and drives the loop until the
// assistant emits text with no further tool calls.
func (a *Agent) Step(ctx context.Context, userInput string) (StepResult, error) {
	defer func() { a.categoryBoundary(ctx, false); a.pruner.stopCategoryJob() }()
	if userInput == "" {
		return StepResult{}, fmt.Errorf("agent: empty input")
	}

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
	a.nudge = ""

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

		a.categoryBoundary(ctx, true)
		if a.pruner.ShouldFire(a.session) && !a.pruner.categories {
			before := a.pruner.ContextTokens(a.session)
			a.emit(ctx, Event{Kind: KindPruneStart, Prune: &PruneInfo{Before: before}})

			pruneCtx, pruneCancel := context.WithCancel(ctx)
			pruneCF := context.CancelFunc(pruneCancel)
			a.turnCancel.Store(&pruneCF)
			after, err := a.pruner.Prune(pruneCtx, a.session)
			a.turnCancel.Store(nil)
			pruneCancel()
			if errors.Is(err, context.Canceled) {
				return StepResult{Turn: a.session.Turn, ToolCalls: calls}, ErrInterrupted
			}
			if err != nil {
				a.emit(ctx, Event{Kind: KindError, Err: err, Text: "prune skipped"})
			} else {
				a.emit(ctx, Event{Kind: KindPruneEnd, Prune: &PruneInfo{
					Before:   before,
					After:    after,
					Rejected: a.pruner.Rejected(),
				}})
			}
		}

		turnCtx, cancel := context.WithCancel(ctx)
		cf := context.CancelFunc(cancel)
		a.turnCancel.Store(&cf)

		msg, err := a.chat(turnCtx, a.tools)
		a.nudge = ""
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

		if msg.Content != "" {
			assistantText = msg.Content
			a.emit(ctx, Event{Kind: KindAssistantEnd, Text: msg.Content})
		}

		if len(msg.ToolCalls) == 0 {
			a.turnCancel.Store(nil)
			cancel()
			a.emit(ctx, Event{Kind: KindTurnEnd})
			return StepResult{
				Assistant: assistantText,
				ToolCalls: calls,
				Turn:      a.session.Turn,
			}, nil
		}

		if rounds := gather.observe(msg.ToolCalls); rounds > 0 {
			a.nudge = gatherNudge(rounds)
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
