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
	if userInput == "" {
		return StepResult{}, fmt.Errorf("agent: empty input")
	}
	a.session.Turn++
	a.session.Append(Msg{Role: "user", Content: userInput})
	if err := a.session.Save(); err != nil {
		return StepResult{}, fmt.Errorf("agent: save session: %w", err)
	}
	a.emit(ctx, Event{Kind: KindUserInput, Text: userInput})

	var calls []ToolCall
	var assistantText string

	for {
		if a.pruner.ShouldFire(a.session) {
			a.emit(ctx, Event{Kind: KindPruneStart, Prune: &PruneInfo{Before: a.pruner.ContextTokens(a.session)}})
			before, after, err := a.pruner.Prune(ctx, a.session)
			if err != nil {
				a.emit(ctx, Event{Kind: KindError, Err: err, Text: "prune skipped"})
			} else {
				a.emit(ctx, Event{Kind: KindPruneEnd, Prune: &PruneInfo{Before: before, After: after}})
			}
		}

		turnCtx, cancel := context.WithCancel(ctx)
		cf := context.CancelFunc(cancel)
		a.turnCancel.Store(&cf)

		msg, err := a.chat(turnCtx, a.tools)
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

		for _, tc := range msg.ToolCalls {
			// An interrupt mid-batch must stop the batch. Without this the
			// remaining calls all run against a cancelled context, fail
			// instantly, and get recorded as genuine tool failures the model
			// then has to reason about.
			if turnCtx.Err() != nil {
				break
			}
			calls = append(calls, tc)
			a.emit(ctx, Event{Kind: KindToolCall, Tool: &ToolEvent{
				ID: tc.ID, Name: tc.Function.Name, Args: json.RawMessage(tc.Function.Arguments),
			}})
			result := a.runTool(turnCtx, tc)
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
		a.turnCancel.Store(nil)
		// Interrupt fires turnCancel, which the tool loop above observes. It
		// must also end the step: otherwise the next iteration opens a fresh
		// context and calls the model again, and the interrupt is swallowed
		// entirely — the agent visibly ignores Ctrl-C and keeps working.
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
